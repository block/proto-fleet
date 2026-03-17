// Package whatsminer provides a mock WhatsMiner TCP JSON-RPC server
// that replays recorded API responses for contract testing.
package whatsminer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/proto-at-block/proto-fleet/tests/plugin-contract/mockapi"
)


type Server struct {
	t        testing.TB
	listener net.Listener
	dataDir  string

	mu        sync.Mutex
	commands  []string
	wg        sync.WaitGroup
	closed    chan struct{}
	responses map[string][]byte

	overrides       map[string][]byte
	behaviors       map[string]mockapi.ConnBehavior
	defaultBehavior mockapi.ConnBehavior
}

// NewServer starts a mock WhatsMiner server on 127.0.0.1:4028 (pyasic's hardcoded port).
// Not safe for parallel tests.
func NewServer(t testing.TB, dataDir string) *Server {
	t.Helper()

	addr := "127.0.0.1:4028"

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("failed to start mock WhatsMiner server on %s: %v", addr, err)
	}

	s := &Server{
		t:         t,
		listener:  listener,
		dataDir:   dataDir,
		closed:    make(chan struct{}),
		responses: loadAllResponses(t, dataDir),
		overrides: make(map[string][]byte),
		behaviors: make(map[string]mockapi.ConnBehavior),
	}

	s.wg.Add(1)
	go s.serve()

	t.Cleanup(func() {
		close(s.closed)
		s.listener.Close()
		s.wg.Wait()
	})

	return s
}

func (s *Server) Addr() string { return s.listener.Addr().String() }

func (s *Server) Host() string {
	host, _, _ := net.SplitHostPort(s.Addr())
	return host
}

func (s *Server) Port() string {
	_, port, _ := net.SplitHostPort(s.Addr())
	return port
}

func (s *Server) Commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.commands...)
}

// SetResponse overrides the response for a specific command.
func (s *Server) SetResponse(cmd string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overrides[cmd] = data
}

// SetConnBehavior changes how the mock handles connections for a specific command.
func (s *Server) SetConnBehavior(cmd string, behavior mockapi.ConnBehavior) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.behaviors[cmd] = behavior
}

// SetDefaultConnBehavior sets the fallback behavior for all commands without a specific override.
func (s *Server) SetDefaultConnBehavior(behavior mockapi.ConnBehavior) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defaultBehavior = behavior
}

// ResetOverrides clears all per-test response overrides and connection behaviors.
func (s *Server) ResetOverrides() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overrides = make(map[string][]byte)
	s.behaviors = make(map[string]mockapi.ConnBehavior)
	s.defaultBehavior = mockapi.BehaviorNormal
}

func (s *Server) serve() {
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				s.t.Logf("mock WhatsMiner: accept error: %v", err)
				return
			}
		}
		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(conn)
	raw, err := reader.ReadString(0x00)
	if err != nil && len(raw) == 0 {
		return
	}
	raw = strings.TrimRight(raw, "\x00")
	if raw == "" {
		return
	}

	var req map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		s.t.Logf("mock WhatsMiner: failed to parse request: %v (raw: %q)", err, raw)
		return
	}

	// Encrypted privileged commands can't be decrypted in the mock.
	if enc, ok := req["enc"].(float64); ok && enc == 1 {
		s.mu.Lock()
		s.commands = append(s.commands, "enc")
		s.mu.Unlock()
		s.writeResponse(conn, "enc", []byte(`{"STATUS":"S","Code":131,"Msg":"API command OK","Description":"btminer"}`))
		return
	}

	cmd := extractCommand(req)

	s.mu.Lock()
	s.commands = append(s.commands, cmd)
	behavior, ok := s.behaviors[cmd]
	if !ok {
		behavior = s.defaultBehavior
	}
	s.mu.Unlock()

	switch behavior {
	case mockapi.BehaviorTimeout:
		// Block until the test's cleanup closes the server.
		<-s.closed
		return
	case mockapi.BehaviorCloseConn:
		return
	}

	resp := s.loadResponse(cmd)

	s.writeResponse(conn, cmd, resp)
}

func loadAllResponses(t testing.TB, dataDir string) map[string][]byte {
	responses := make(map[string][]byte)
	for _, dir := range []string{"rpc", "privileged"} {
		dirPath := filepath.Join(dataDir, dir)
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
				continue
			}
			cmd := strings.TrimSuffix(e.Name(), ".json")
			data, err := os.ReadFile(filepath.Join(dirPath, e.Name()))
			if err != nil {
				t.Logf("mock WhatsMiner: failed to load %s/%s: %v", dir, e.Name(), err)
				continue
			}
			responses[cmd] = data
		}
	}
	return responses
}

func (s *Server) loadResponse(cmd string) []byte {
	s.mu.Lock()
	override, hasOverride := s.overrides[cmd]
	s.mu.Unlock()

	if hasOverride {
		return override
	}

	if data, ok := s.responses[cmd]; ok {
		return data
	}

	s.t.Logf("mock WhatsMiner: no recorded response for %q", cmd)
	return []byte(fmt.Sprintf(`{"STATUS":"E","Code":-1,"Msg":"Unknown command: %s"}`, cmd))
}

func (s *Server) writeResponse(conn net.Conn, cmd string, resp []byte) {
	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	buf := append(resp[:len(resp):len(resp)], 0x00)
	if _, err := conn.Write(buf); err != nil {
		s.t.Logf("mock WhatsMiner: failed to write response for %q: %v", cmd, err)
	}
}

func extractCommand(req map[string]interface{}) string {
	cmd, _ := req["cmd"].(string)
	if cmd == "" {
		cmd, _ = req["command"].(string)
	}
	return strings.ToLower(strings.TrimSpace(cmd))
}
