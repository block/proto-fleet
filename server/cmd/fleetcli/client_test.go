package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		server   string
		insecure bool
		want     string
	}{
		{
			name:   "adds https scheme and api proxy path",
			server: "fleet.example.com",
			want:   "https://fleet.example.com/api-proxy",
		},
		{
			name:     "uses http for insecure loopback host without scheme",
			server:   "localhost:8080",
			insecure: true,
			want:     "http://localhost:8080/api-proxy",
		},
		{
			name:     "keeps https for insecure remote host without scheme",
			server:   "fleet.example.com",
			insecure: true,
			want:     "https://fleet.example.com/api-proxy",
		},
		{
			name:   "preserves explicit path",
			server: "https://fleet.example.com/custom/",
			want:   "https://fleet.example.com/custom",
		},
		{
			name:     "treats explicit trailing slash as RPC root",
			server:   "http://localhost:4000/",
			insecure: false,
			want:     "http://localhost:4000",
		},
		{
			name:     "allows remote http with insecure override",
			server:   "http://fleet.example.com",
			insecure: true,
			want:     "http://fleet.example.com/api-proxy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeBaseURL(tt.server, tt.insecure)
			if err != nil {
				t.Fatalf("normalizeBaseURL() error = %v", err)
			}
			if got.String() != tt.want {
				t.Fatalf("normalizeBaseURL() = %q, want %q", got.String(), tt.want)
			}
		})
	}
}

func TestNormalizeBaseURLRejectsMissingHost(t *testing.T) {
	if _, err := normalizeBaseURL("https:///api-proxy", false); err == nil {
		t.Fatal("normalizeBaseURL() error = nil, want missing host error")
	}
}

func TestNormalizeBaseURLRejectsUnsafeServerURL(t *testing.T) {
	tests := []struct {
		name   string
		server string
		want   string
	}{
		{name: "remote http without insecure", server: "http://fleet.example.com", want: "https"},
		{name: "userinfo", server: "https://user:pass@fleet.example.com", want: "userinfo"},
		{name: "query string", server: "https://fleet.example.com?token=abc", want: "query"},
		{name: "fragment", server: "https://fleet.example.com#token", want: "fragment"},
		{name: "invalid scheme", server: "ftp://fleet.example.com", want: "scheme"},
		{name: "missing host", server: "https:///api-proxy", want: "host"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizeBaseURL(tt.server, false)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("normalizeBaseURL() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestNewClientTransportPreservesDefaultProxyHook(t *testing.T) {
	client, err := New(context.Background(), Options{Server: "https://fleet.example.com", APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *http.Transport", client.httpClient.Transport)
	}
	if transport.Proxy == nil {
		t.Fatal("Transport.Proxy = nil, want default proxy hook")
	}
	if transport.TLSClientConfig == nil {
		t.Fatal("Transport.TLSClientConfig = nil, want CLI TLS config")
	}
}

func TestSessionCredentialsReadsPasswordFromStdin(t *testing.T) {
	oldRead := readFleetPasswordFromStdin
	readFleetPasswordFromStdin = func() (string, error) {
		return "stdin-pass", nil
	}
	t.Cleanup(func() { readFleetPasswordFromStdin = oldRead })

	client := &Client{username: "admin", passwordStdin: true}
	username, password, err := client.sessionCredentials()
	if err != nil {
		t.Fatalf("sessionCredentials() error = %v", err)
	}
	if username != "admin" || password != "stdin-pass" {
		t.Fatalf("sessionCredentials() = (%q, %q), want (admin, stdin-pass)", username, password)
	}
	if client.passwordStdin {
		t.Fatal("passwordStdin = true, want false after reading stdin once")
	}
}

func TestSessionCredentialsPromptsForPassword(t *testing.T) {
	oldPrompt := promptFleetPassword
	promptFleetPassword = func() (string, error) {
		return "prompt-pass", nil
	}
	t.Cleanup(func() { promptFleetPassword = oldPrompt })

	client := &Client{username: "admin"}
	username, password, err := client.sessionCredentials()
	if err != nil {
		t.Fatalf("sessionCredentials() error = %v", err)
	}
	if username != "admin" || password != "prompt-pass" {
		t.Fatalf("sessionCredentials() = (%q, %q), want (admin, prompt-pass)", username, password)
	}
}

func TestClientRejectsRedirects(t *testing.T) {
	redirectTargetHit := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirectTargetHit = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	t.Cleanup(srv.Close)

	client, err := New(context.Background(), Options{Server: srv.URL + "/"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = client.Authenticate(context.Background(), "admin", "proto")
	if err == nil || !strings.Contains(err.Error(), "redirects are not allowed") {
		t.Fatalf("Authenticate() error = %v, want redirect rejection", err)
	}
	if redirectTargetHit {
		t.Fatal("redirect target was hit, want redirect blocked")
	}
}

func TestTransferClientRejectsRedirects(t *testing.T) {
	redirectTargetHit := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirectTargetHit = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	t.Cleanup(srv.Close)

	client, err := New(context.Background(), Options{Server: srv.URL + "/"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	resp, err := client.transferClient().Post(srv.URL, "text/plain", strings.NewReader("body"))
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "redirects are not allowed") {
		t.Fatalf("transferClient().Post() error = %v, want redirect rejection", err)
	}
	if redirectTargetHit {
		t.Fatal("redirect target was hit, want redirect blocked")
	}
}
