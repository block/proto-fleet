package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIPTrustBoundary(t *testing.T) {
	for _, tt := range []struct{ name, trusted, peer, forwarded, want string }{
		{"bare ignores headers", "", "192.0.2.1:123", "198.51.100.1", "192.0.2.1"},
		{"nginx strips spoof", "127.0.0.1/32", "127.0.0.1:123", "203.0.113.9, 192.0.2.1", "192.0.2.1"},
		{"ALB and nginx", "127.0.0.1/32,10.0.0.0/24", "127.0.0.1:123", "203.0.113.9, 192.0.2.1, 10.0.0.4", "192.0.2.1"},
		{"IPv6", "::1/128", "[::1]:123", "2001:db8::1", "2001:db8::1"},
		{"mapped IPv4", "127.0.0.1/32", "[::ffff:127.0.0.1]:123", "192.0.2.1", "192.0.2.1"},
		{"malformed trusted hop", "127.0.0.1/32", "127.0.0.1:123", "192.0.2.1, bad", "127.0.0.1"},
		{"malformed prefix", "127.0.0.1/32", "127.0.0.1:123", "bad, 192.0.2.1", "127.0.0.1"},
		{"no forwarding", "127.0.0.1/32", "127.0.0.1:123", "", "127.0.0.1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewClientIPMiddleware(tt.trusted)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tt.peer
			r.Header.Set("X-Forwarded-For", tt.forwarded)
			r.Header.Set("X-Real-IP", "203.0.113.7")
			m.Wrap(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				if got := ClientIP(r.Context()); got != tt.want {
					t.Fatalf("got %q, want %q", got, tt.want)
				}
			})).ServeHTTP(httptest.NewRecorder(), r)
		})
	}
	for _, invalid := range []string{"10.0.0.1", "bad", "127.0.0.1/32,"} {
		if _, err := NewClientIPMiddleware(invalid); err == nil {
			t.Fatalf("accepted invalid CIDRs %q", invalid)
		}
	}
}

func TestClientIPRepeatedForwardingHeaders(t *testing.T) {
	m, err := NewClientIPMiddleware("127.0.0.1/32,10.0.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:123"
	r.Header.Add("X-Forwarded-For", "203.0.113.9")
	r.Header.Add("X-Forwarded-For", "192.0.2.1, 10.0.0.4")
	m.Wrap(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if got := ClientIP(r.Context()); got != "192.0.2.1" {
			t.Fatal(got)
		}
	})).ServeHTTP(httptest.NewRecorder(), r)
}
