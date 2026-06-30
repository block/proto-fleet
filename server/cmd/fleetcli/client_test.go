package main

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/url"
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
			name:     "uses http for insecure host without scheme",
			server:   "localhost:8080",
			insecure: true,
			want:     "http://localhost:8080/api-proxy",
		},
		{
			name:   "preserves explicit path",
			server: "https://fleet.example.com/custom/",
			want:   "https://fleet.example.com/custom",
		},
		{
			name:     "treats explicit trailing slash as RPC root",
			server:   "http://localhost:4000/",
			insecure: true,
			want:     "http://localhost:4000",
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

func TestLoopbackSecureJarReturnsSecureCookiesOverLoopbackHTTP(t *testing.T) {
	sessionCookie := []*http.Cookie{{Name: "fleet_session", Value: "abc", Path: "/", Secure: true}}

	for _, host := range []string{"localhost:4000", "127.0.0.1:4000"} {
		inner, err := cookiejar.New(nil)
		if err != nil {
			t.Fatalf("cookiejar.New() error = %v", err)
		}
		jar := &loopbackSecureJar{inner: inner}

		setURL, err := url.Parse("http://" + host + "/auth.v1.AuthService/Authenticate")
		if err != nil {
			t.Fatalf("url.Parse() error = %v", err)
		}
		jar.SetCookies(setURL, sessionCookie)

		probeURL, err := url.Parse("http://" + host + "/pairing.v1.PairingService/Pair")
		if err != nil {
			t.Fatalf("url.Parse() error = %v", err)
		}
		if len(jar.Cookies(probeURL)) == 0 {
			t.Fatalf("Cookies(%s) = empty, want secure session cookie", probeURL)
		}
	}
}

func TestLoopbackSecureJarKeepsSecureSemanticsForRemoteHTTP(t *testing.T) {
	inner, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	jar := &loopbackSecureJar{inner: inner}

	setURL, err := url.Parse("https://fleet.example.com/auth.v1.AuthService/Authenticate")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	jar.SetCookies(setURL, []*http.Cookie{{Name: "fleet_session", Value: "abc", Path: "/", Secure: true}})

	probeURL, err := url.Parse("http://fleet.example.com/pairing.v1.PairingService/Pair")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	if cookies := jar.Cookies(probeURL); len(cookies) != 0 {
		t.Fatalf("Cookies(%s) = %v, want none for secure cookie over remote http", probeURL, cookies)
	}
}
