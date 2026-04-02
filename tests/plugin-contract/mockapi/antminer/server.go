// Package antminer provides a mock Antminer server (RPC + HTTP)
// that replays recorded API responses for contract testing.
package antminer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/block/proto-fleet/tests/plugin-contract/mockapi"
)

type Server struct {
	t        testing.TB
	dataDir  string
	rpcAddr  string
	webPort  int

	// RPC server
	rpcListener net.Listener

	// HTTP server
	httpListener net.Listener
	httpServer   *http.Server

	mu              sync.Mutex
	commands        []string
	wg              sync.WaitGroup
	closed          chan struct{}
	rpcResponses    map[string][]byte
	webResponses    map[string][]byte
	rpcOverrides    map[string][]byte
	webOverrides    map[string][]byte
	behaviors       map[string]mockapi.ConnBehavior
	defaultBehavior mockapi.ConnBehavior
}

// NewServer starts a mock Antminer server with RPC on 127.0.0.1:4028 and HTTP on a random port.
// Not safe for parallel tests (shares port 4028 with WhatsMiner mock).
func NewServer(t testing.TB, dataDir string) *Server {
	t.Helper()

	rpcAddr := "127.0.0.1:4028"
	rpcListener := mockapi.ListenWithRetry(t, rpcAddr)

	httpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		rpcListener.Close()
		t.Fatalf("failed to start mock Antminer HTTP server: %v", err)
	}

	_, portStr, _ := net.SplitHostPort(httpListener.Addr().String())
	var webPort int
	fmt.Sscanf(portStr, "%d", &webPort)

	s := &Server{
		t:            t,
		dataDir:      dataDir,
		rpcAddr:      rpcAddr,
		webPort:      webPort,
		rpcListener:  rpcListener,
		httpListener: httpListener,
		closed:       make(chan struct{}),
		rpcResponses: loadResponses(t, filepath.Join(dataDir, "rpc"), ".json"),
		webResponses: loadResponses(t, filepath.Join(dataDir, "web"), ""),
		rpcOverrides: make(map[string][]byte),
		webOverrides: make(map[string][]byte),
		behaviors:    make(map[string]mockapi.ConnBehavior),
	}

	// Start RPC server
	s.wg.Add(1)
	go s.serveRPC()

	// Start HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleHTTP)
	s.httpServer = &http.Server{Handler: mux}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.httpServer.Serve(httpListener); err != nil && err != http.ErrServerClosed {
			select {
			case <-s.closed:
			default:
				t.Logf("mock Antminer HTTP: serve error: %v", err)
			}
		}
	}()

	t.Cleanup(func() {
		close(s.closed)
		rpcListener.Close()
		s.httpServer.Close()
		s.wg.Wait()
	})

	return s
}

func (s *Server) Host() string { return "127.0.0.1" }
func (s *Server) WebPort() int { return s.webPort }

func (s *Server) Commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.commands...)
}

// SetResponse overrides the RPC response for a specific command.
func (s *Server) SetResponse(cmd string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rpcOverrides[cmd] = data
}

// SetWebResponse overrides the HTTP response for a specific endpoint key.
func (s *Server) SetWebResponse(endpoint string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.webOverrides[endpoint] = data
}

// SetConnBehavior changes how the mock handles connections for a specific RPC command.
func (s *Server) SetConnBehavior(cmd string, behavior mockapi.ConnBehavior) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.behaviors[cmd] = behavior
}

// SetDefaultConnBehavior sets the fallback behavior for all RPC commands without a specific override.
func (s *Server) SetDefaultConnBehavior(behavior mockapi.ConnBehavior) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defaultBehavior = behavior
}

// ResetOverrides clears all per-test response overrides and connection behaviors.
func (s *Server) ResetOverrides() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rpcOverrides = make(map[string][]byte)
	s.webOverrides = make(map[string][]byte)
	s.behaviors = make(map[string]mockapi.ConnBehavior)
	s.defaultBehavior = mockapi.BehaviorNormal
}

// --- RPC server (CGMiner protocol: JSON over TCP, no null terminator) ---

func (s *Server) serveRPC() {
	defer s.wg.Done()

	for {
		conn, err := s.rpcListener.Accept()
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				s.t.Logf("mock Antminer RPC: accept error: %v", err)
				return
			}
		}
		s.wg.Add(1)
		go s.handleRPCConn(conn)
	}
}

func (s *Server) handleRPCConn(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	// CGMiner protocol: JSON encoded with json.NewEncoder (newline-terminated), not null-terminated
	reader := bufio.NewReader(conn)
	var req map[string]interface{}
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(&req); err != nil {
		return
	}

	cmd := extractRPCCommand(req)

	s.mu.Lock()
	s.commands = append(s.commands, cmd)
	behavior, ok := s.behaviors[cmd]
	if !ok {
		behavior = s.defaultBehavior
	}
	s.mu.Unlock()

	switch behavior {
	case mockapi.BehaviorTimeout:
		<-s.closed
		return
	case mockapi.BehaviorCloseConn:
		return
	}

	resp := s.loadRPCResponse(cmd)

	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := json.NewEncoder(conn).Encode(json.RawMessage(resp)); err != nil {
		s.t.Logf("mock Antminer RPC: failed to write response for %q: %v", cmd, err)
	}
}

func (s *Server) loadRPCResponse(cmd string) []byte {
	s.mu.Lock()
	override, hasOverride := s.rpcOverrides[cmd]
	s.mu.Unlock()

	if hasOverride {
		return override
	}
	if data, ok := s.rpcResponses[cmd]; ok {
		return data
	}
	s.t.Logf("mock Antminer RPC: no recorded response for %q", cmd)
	return []byte(fmt.Sprintf(`{"STATUS":[{"STATUS":"E","Code":-1,"Msg":"Unknown command: %s"}],"id":1}`, cmd))
}

func extractRPCCommand(req map[string]interface{}) string {
	cmd, _ := req["command"].(string)
	if cmd == "" {
		cmd, _ = req["cmd"].(string)
	}
	return strings.ToLower(strings.TrimSpace(cmd))
}

// --- HTTP server (digest auth + CGI endpoints) ---

func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request) {
	// Simplified digest auth: require Authorization header with valid username.
	// First request without auth gets a 401 with WWW-Authenticate challenge.
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Digest ") {
		w.Header().Set("WWW-Authenticate",
			`Digest realm="antMiner Configuration", nonce="abc123def456", qop="auth", algorithm=MD5`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Extract endpoint key from path: /cgi-bin/stats.cgi -> stats
	endpoint := endpointKey(r.URL.Path)

	s.mu.Lock()
	s.commands = append(s.commands, "web:"+endpoint)
	s.mu.Unlock()

	resp := s.loadWebResponse(endpoint)
	if resp == nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(resp)
}

func (s *Server) loadWebResponse(endpoint string) []byte {
	s.mu.Lock()
	override, hasOverride := s.webOverrides[endpoint]
	s.mu.Unlock()

	if hasOverride {
		return override
	}
	if data, ok := s.webResponses[endpoint]; ok {
		return data
	}
	s.t.Logf("mock Antminer HTTP: no recorded response for %q", endpoint)
	return nil
}

// endpointKey maps CGI paths to fixture file keys:
//
//	/cgi-bin/get_system_info.cgi -> get_system_info
//	/cgi-bin/stats.cgi           -> stats
//	/cgi-bin/log.cgi             -> log
//	/cgi-bin/set_miner_conf.cgi  -> set_miner_conf
//	/cgi-bin/reboot.cgi          -> reboot
//	/cgi-bin/blink.cgi           -> blink
func endpointKey(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, ".cgi")
}

// --- Loading fixtures ---

func loadResponses(t testing.TB, dirPath string, onlyExt string) map[string][]byte {
	responses := make(map[string][]byte)
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return responses
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if onlyExt != "" && ext != onlyExt {
			continue
		}
		key := strings.TrimSuffix(e.Name(), ext)
		data, err := os.ReadFile(filepath.Join(dirPath, e.Name()))
		if err != nil {
			t.Logf("mock Antminer: failed to load %s: %v", e.Name(), err)
			continue
		}
		responses[key] = data
	}
	return responses
}
