// Package whatsminer provides a mock WhatsMiner TCP JSON-RPC server
// that replays recorded API responses for contract testing.
package whatsminer

import (
	"crypto/aes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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

	"github.com/GehirnInc/crypt/md5_crypt"
	"github.com/block/proto-fleet/tests/plugin-contract/mockapi"
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

// NewServer starts a mock WhatsMiner server with RPC on 127.0.0.1:4028 and
// HTTP on 127.0.0.1:80 (returns 307 redirect to HTTPS, matching real firmware).
// Not safe for parallel tests.
func NewServer(t testing.TB, dataDir string) *Server {
	t.Helper()

	rpcAddr := "127.0.0.1:4028"
	listener := mockapi.ListenWithRetry(t, rpcAddr)

	// HTTP on port 80: real WhatsMiner returns 307 redirect to HTTPS.
	// asic-rs uses this for firmware identification (identify_web).
	httpAddr := "127.0.0.1:80"
	httpListener := mockapi.ListenWithRetry(t, httpAddr)
	httpServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Real WhatsMiner redirects "/" to HTTPS and returns 404 for other paths.
			if r.URL.Path == "/" {
				http.Redirect(w, r, "https://127.0.0.1/", http.StatusTemporaryRedirect)
			} else {
				http.NotFound(w, r)
			}
		}),
	}
	go httpServer.Serve(httpListener)

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
		httpServer.Close()
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

	s.t.Logf("mock WhatsMiner: RPC server listening on %s", s.listener.Addr())
	for {
		conn, err := s.listener.Accept()
		if err == nil {
			s.t.Logf("mock WhatsMiner: accepted RPC connection from %s", conn.RemoteAddr())
		}
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

	// Read until we have a complete JSON object or a null terminator.
	// asic-rs sends JSON without a null terminator, pyasic sends with one.
	// We use brace-matching to detect the end of the JSON payload.
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	raw, err := readJSONRequest(conn)
	if err != nil || raw == "" {
		return
	}
	s.t.Logf("mock WhatsMiner: received raw request (%d bytes): %q", len(raw), raw)
	if raw == "" {
		s.t.Logf("mock WhatsMiner: empty request, closing connection")
		return
	}

	var req map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		s.t.Logf("mock WhatsMiner: failed to parse request: %v (raw: %q)", err, raw)
		return
	}

	// Decrypt encrypted privileged commands (AES-256-ECB).
	if enc, ok := req["enc"].(float64); ok && enc == 1 {
		encData, _ := req["data"].(string)
		cmd, err := s.decryptCommand(encData)
		if err != nil {
			s.t.Logf("mock WhatsMiner: failed to decrypt command: %v", err)
			s.writeResponse(conn, "enc", []byte(`{"STATUS":"E","Code":-1,"Msg":"Decrypt failed"}`))
			return
		}
		s.t.Logf("mock WhatsMiner: decrypted privileged command: %s", cmd)

		s.mu.Lock()
		s.commands = append(s.commands, cmd)
		s.mu.Unlock()

		resp := s.loadResponse(cmd)
		encResp := s.encryptResponse(resp)
		s.writeResponse(conn, cmd, encResp)
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
	s.t.Logf("mock WhatsMiner: sending response for %q (%d bytes)", cmd, len(resp))
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

// readJSONRequest reads a complete JSON object from the connection.
// It handles both null-terminated (pyasic) and unterminated (asic-rs) requests
// by counting braces to detect the end of the JSON payload.
func readJSONRequest(conn net.Conn) (string, error) {
	var buf [4096]byte
	var raw []byte
	depth := 0
	started := false

	for {
		n, err := conn.Read(buf[:])
		if n > 0 {
			for _, b := range buf[:n] {
				if b == 0x00 {
					// Null terminator -- end of request
					return strings.TrimRight(string(raw), "\x00"), nil
				}
				raw = append(raw, b)
				if b == '{' {
					depth++
					started = true
				} else if b == '}' {
					depth--
					if started && depth == 0 {
						return string(raw), nil
					}
				}
			}
		}
		if err != nil {
			// Deadline or EOF -- return what we have
			return strings.TrimRight(string(raw), "\x00"), nil
		}
	}
}

// deriveEncryptionKey derives the AES encryption key from the mock's get_token response
// and the default password ("admin"). This mirrors asic-rs's token generation flow.
func (s *Server) deriveEncryptionKey() (string, error) {
	tokenResp := s.loadResponse("get_token")
	var tok struct {
		Msg struct {
			Salt    string `json:"salt"`
			NewSalt string `json:"newsalt"`
			Time    string `json:"time"`
		} `json:"Msg"`
	}
	if err := json.Unmarshal(tokenResp, &tok); err != nil {
		return "", fmt.Errorf("parse get_token: %w", err)
	}

	// md5crypt("admin", salt) → extract hash segment
	crypt := md5_crypt.New()
	hash, err := crypt.Generate([]byte("admin"), []byte("$1$"+tok.Msg.Salt+"$"))
	if err != nil {
		return "", fmt.Errorf("md5crypt: %w", err)
	}
	parts := strings.Split(hash, "$")
	if len(parts) < 4 {
		return "", fmt.Errorf("unexpected md5crypt output: %s", hash)
	}
	hostPasswordMD5 := parts[3]
	return hostPasswordMD5, nil
}

// decryptCommand decrypts a base64-encoded AES-256-ECB encrypted command
// and returns the command name from the decrypted JSON.
func (s *Server) decryptCommand(encData string) (string, error) {
	key, err := s.deriveEncryptionKey()
	if err != nil {
		return "", err
	}

	plaintext, err := aesECBDecrypt(key, encData)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	var cmd struct {
		Command string `json:"command"`
	}
	// Trim zero padding before parsing JSON
	plaintext = strings.TrimRight(plaintext, "\x00")
	if err := json.Unmarshal([]byte(plaintext), &cmd); err != nil {
		return "", fmt.Errorf("parse decrypted command: %w (plaintext: %q)", err, plaintext)
	}
	return strings.ToLower(cmd.Command), nil
}

// encryptResponse encrypts a JSON response using AES-256-ECB.
func (s *Server) encryptResponse(resp []byte) []byte {
	key, err := s.deriveEncryptionKey()
	if err != nil {
		s.t.Logf("mock WhatsMiner: failed to derive key for response encryption: %v", err)
		return resp // fall back to unencrypted
	}

	encrypted := aesECBEncrypt(key, string(resp))
	return []byte(fmt.Sprintf(`{"enc":"%s"}`, encrypted))
}

// aesECBDecrypt decrypts base64-encoded AES-256-ECB data.
// Key is SHA256-hashed and hex-decoded to get the 32-byte AES key.
func aesECBDecrypt(key, b64Data string) (string, error) {
	aesKey := deriveAESKey(key)

	ciphertext, err := base64.StdEncoding.DecodeString(b64Data)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", fmt.Errorf("aes cipher: %w", err)
	}

	blockSize := block.BlockSize()
	if len(ciphertext)%blockSize != 0 {
		return "", fmt.Errorf("ciphertext length %d not multiple of block size %d", len(ciphertext), blockSize)
	}

	plaintext := make([]byte, len(ciphertext))
	for i := 0; i < len(ciphertext); i += blockSize {
		block.Decrypt(plaintext[i:i+blockSize], ciphertext[i:i+blockSize])
	}

	return string(plaintext), nil
}

// aesECBEncrypt encrypts data with AES-256-ECB and returns base64.
func aesECBEncrypt(key, data string) string {
	aesKey := deriveAESKey(key)

	// Zero-pad to 16-byte boundary
	padded := []byte(data)
	if rem := len(padded) % aes.BlockSize; rem != 0 {
		padded = append(padded, make([]byte, aes.BlockSize-rem)...)
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return ""
	}

	ciphertext := make([]byte, len(padded))
	for i := 0; i < len(padded); i += aes.BlockSize {
		block.Encrypt(ciphertext[i:i+aes.BlockSize], padded[i:i+aes.BlockSize])
	}

	return base64.StdEncoding.EncodeToString(ciphertext)
}

// deriveAESKey converts a password/key string to a 32-byte AES key:
// SHA256(key) → hex → bytes
func deriveAESKey(key string) []byte {
	h := sha256.Sum256([]byte(key))
	hexStr := hex.EncodeToString(h[:])
	aesKey, _ := hex.DecodeString(hexStr)
	return aesKey
}
