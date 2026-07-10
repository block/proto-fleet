package transportguard

import (
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
)

func TestValidateServerURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		url           string
		allowInsecure bool
		wantErr       string
	}{
		{name: "https accepted", url: "https://fleet.example.com", wantErr: ""},
		{name: "loopback http localhost", url: "http://localhost:4000", wantErr: ""},
		{name: "loopback http 127.0.0.1", url: "http://127.0.0.1:4000", wantErr: ""},
		{name: "loopback http 127.x.x.x", url: "http://127.5.6.7:4000", wantErr: ""},
		{name: "loopback http ipv6", url: "http://[::1]:4000", wantErr: ""},
		{name: "remote http rejected", url: "http://fleet.example.com", wantErr: "https"},
		{name: "remote http allowed via flag", url: "http://fleet.example.com", allowInsecure: true, wantErr: ""},
		{name: "unknown scheme rejected", url: "ftp://fleet.example.com", wantErr: "scheme"},
		{name: "missing host rejected", url: "https://", wantErr: "host"},
		{name: "userinfo rejected", url: "https://fleet.example.com@attacker.example", wantErr: "userinfo"},
		{name: "userinfo with password rejected", url: "https://user:pass@attacker.example", wantErr: "userinfo"},
		{name: "query string rejected", url: "https://fleet.example.com?foo=bar", wantErr: "query"},
		{name: "fragment rejected", url: "https://fleet.example.com#frag", wantErr: "fragment"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateServerURL(tt.url, tt.allowInsecure)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateServerURL() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateServerURL() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRejectRedirect(t *testing.T) {
	t.Parallel()

	err := RejectRedirect(&http.Request{}, nil)
	if !errors.Is(err, ErrRedirectNotAllowed) {
		t.Fatalf("RejectRedirect() error = %v, want ErrRedirectNotAllowed", err)
	}
}

func TestLoopbackSecureJarReturnsSecureCookiesOverLoopbackHTTP(t *testing.T) {
	sessionCookie := []*http.Cookie{{Name: "fleet_session", Value: "abc", Path: "/", Secure: true}}

	for _, host := range []string{"localhost:4000", "127.0.0.1:4000"} {
		inner, err := cookiejar.New(nil)
		if err != nil {
			t.Fatalf("cookiejar.New() error = %v", err)
		}
		jar := NewLoopbackSecureJar(inner)

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
	jar := NewLoopbackSecureJar(inner)

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
