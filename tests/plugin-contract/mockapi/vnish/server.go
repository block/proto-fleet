// Package vnish provides a mock VNish server (RPC + HTTP)
// that replays recorded API responses for contract testing.
package vnish

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/block/proto-fleet/tests/plugin-contract/mockapi"
)

const mockToken = "mock-vnish-token"

type Server struct {
	t       testing.TB
	dataDir string
	rpcAddr string
	webPort int

	rpcListener net.Listener

	httpListener net.Listener
	httpServer   *http.Server

	mu              sync.Mutex
	commands        []string
	wg              sync.WaitGroup
	closed          chan struct{}
	rpcResponses    map[string][]byte
	webResponses    map[string][]byte
	privResponses   map[string][]byte
	rpcOverrides    map[string][]byte
	webOverrides    map[string][]byte
	behaviors       map[string]mockapi.ConnBehavior
	defaultBehavior mockapi.ConnBehavior
}

// NewServer starts a mock VNish server with RPC on 127.0.0.1:4028 and HTTP on port 80.
// Port 80 is required because asic-rs hardcodes VNish HTTP connections to port 80.
// Not safe for parallel tests (shares ports 4028 and 80 with other mocks).
func NewServer(t testing.TB, dataDir string) *Server {
	t.Helper()

	rpcAddr := "127.0.0.1:4028"
	rpcListener := mockapi.ListenWithRetry(t, rpcAddr)

	httpAddr := "127.0.0.1:80"
	httpListener := mockapi.ListenWithRetry(t, httpAddr)

	_, portStr, err := net.SplitHostPort(httpListener.Addr().String())
	if err != nil {
		rpcListener.Close()
		httpListener.Close()
		t.Fatalf("failed to parse mock VNish HTTP address %q: %v", httpListener.Addr().String(), err)
	}

	webPort, err := strconv.Atoi(portStr)
	if err != nil {
		rpcListener.Close()
		httpListener.Close()
		t.Fatalf("failed to parse mock VNish HTTP port %q: %v", portStr, err)
	}

	s := &Server{
		t:             t,
		dataDir:       dataDir,
		rpcAddr:       rpcAddr,
		webPort:       webPort,
		rpcListener:   rpcListener,
		httpListener:  httpListener,
		closed:        make(chan struct{}),
		rpcResponses:  loadResponses(t, filepath.Join(dataDir, "rpc")),
		webResponses:  loadResponses(t, filepath.Join(dataDir, "web")),
		privResponses: loadResponses(t, filepath.Join(dataDir, "privileged")),
		rpcOverrides:  make(map[string][]byte),
		webOverrides:  make(map[string][]byte),
		behaviors:     make(map[string]mockapi.ConnBehavior),
	}

	s.wg.Add(1)
	go s.serveRPC()

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
				t.Logf("mock VNish HTTP: serve error: %v", err)
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

// SetWebResponse overrides an HTTP API response for a specific endpoint.
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
				s.t.Logf("mock VNish RPC: accept error: %v", err)
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

	var req map[string]interface{}
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&req); err != nil {
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
	s.writeRPCResponse(conn, cmd, resp)
}

func (s *Server) loadRPCResponse(cmd string) []byte {
	if strings.Contains(cmd, "+") {
		return s.loadRPCMulticommandResponse(cmd)
	}

	return s.loadRPCResponseSingle(cmd)
}

func (s *Server) loadRPCResponseSingle(cmd string) []byte {
	s.mu.Lock()
	override, hasOverride := s.rpcOverrides[cmd]
	s.mu.Unlock()

	if hasOverride {
		return override
	}
	if data, ok := s.rpcResponses[cmd]; ok {
		return data
	}

	s.t.Logf("mock VNish RPC: no recorded response for %q", cmd)
	return []byte(fmt.Sprintf(`{"STATUS":[{"STATUS":"E","Code":-1,"Msg":"Unknown command: %s","Description":"cgminer 4.11.1"}]}`, cmd))
}

func (s *Server) loadRPCMulticommandResponse(cmd string) []byte {
	combined := make(map[string][]json.RawMessage)
	for _, subcmd := range strings.Split(cmd, "+") {
		subcmd = strings.TrimSpace(subcmd)
		if subcmd == "" {
			continue
		}
		combined[subcmd] = []json.RawMessage{json.RawMessage(s.loadRPCResponseSingle(subcmd))}
	}

	data, err := json.Marshal(combined)
	if err != nil {
		s.t.Logf("mock VNish RPC: failed to marshal multicommand %q: %v", cmd, err)
		return []byte(`{}`)
	}
	return data
}

func (s *Server) writeRPCResponse(conn net.Conn, cmd string, resp []byte) {
	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	buf := append(resp[:len(resp):len(resp)], 0x00)
	if _, err := conn.Write(buf); err != nil {
		s.t.Logf("mock VNish RPC: failed to write response for %q: %v", cmd, err)
	}
}

func extractRPCCommand(req map[string]interface{}) string {
	cmd, _ := req["command"].(string)
	if cmd == "" {
		cmd, _ = req["cmd"].(string)
	}
	return strings.ToLower(strings.TrimSpace(cmd))
}

func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/":
		s.mu.Lock()
		s.commands = append(s.commands, "web:root")
		s.mu.Unlock()

		resp := s.loadWebResponse("root", false)
		if resp == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(resp)
		return

	case r.URL.Path == "/api/v1/unlock":
		s.handleUnlock(w, r)
		return

	case strings.HasPrefix(r.URL.Path, "/api/v1/"):
		s.handleAPI(w, r)
		return

	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleUnlock(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.commands = append(s.commands, "web:unlock")
	s.mu.Unlock()

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	defer r.Body.Close()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var req struct {
		Password string `json:"pw"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Password != "admin" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	resp := s.loadWebResponse("unlock", false)
	if resp == nil {
		resp = []byte(`{"token":"` + mockToken + `"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(resp)
}

func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	endpoint := strings.TrimPrefix(r.URL.Path, "/api/v1/")

	s.mu.Lock()
	s.commands = append(s.commands, "web:"+endpoint)
	s.mu.Unlock()

	// /api/v1/info is readable without auth (used by asic-rs for model detection).
	// All other endpoints require auth.
	if endpoint != "info" && !isAuthorized(r.Header.Get("Authorization")) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	resp := s.loadWebResponse(endpoint, r.Method == http.MethodPost)
	if resp == nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(resp)
}

func isAuthorized(header string) bool {
	return header == mockToken || header == "Bearer "+mockToken
}

func (s *Server) loadWebResponse(endpoint string, post bool) []byte {
	s.mu.Lock()
	override, hasOverride := s.webOverrides[endpoint]
	s.mu.Unlock()

	if hasOverride {
		return override
	}
	if post {
		if data, ok := s.privResponses[endpoint]; ok {
			return data
		}
	}
	if data, ok := s.webResponses[endpoint]; ok {
		return data
	}
	return nil
}

func loadResponses(t testing.TB, dirPath string) map[string][]byte {
	responses := make(map[string][]byte)
	if _, err := os.Stat(dirPath); err != nil {
		return responses
	}

	if err := filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Logf("mock VNish: failed to load %s: %v", path, readErr)
			return nil
		}

		rel, relErr := filepath.Rel(dirPath, path)
		if relErr != nil {
			return relErr
		}
		key := filepath.ToSlash(strings.TrimSuffix(rel, filepath.Ext(rel)))
		responses[key] = data
		return nil
	}); err != nil {
		t.Logf("mock VNish: failed to load responses from %s: %v", dirPath, err)
	}

	return responses
}
