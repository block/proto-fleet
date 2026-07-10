package transportguard

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
)

// ErrRedirectNotAllowed prevents clients from replaying sensitive requests to
// another location after a 3xx response.
var ErrRedirectNotAllowed = errors.New("redirects are not allowed")

// RejectRedirect is suitable for http.Client.CheckRedirect.
func RejectRedirect(_ *http.Request, _ []*http.Request) error {
	return ErrRedirectNotAllowed
}

// ValidateServerURL requires https unless the host is loopback (localhost,
// 127/8, ::1) or allowInsecure is set.
func ValidateServerURL(raw string, allowInsecure bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse server-url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("server-url scheme must be http or https; got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("server-url has no host")
	}
	if u.User != nil {
		return fmt.Errorf("server-url must not contain userinfo")
	}
	if u.RawQuery != "" {
		return fmt.Errorf("server-url must not contain a query string")
	}
	if u.Fragment != "" {
		return fmt.Errorf("server-url must not contain a fragment")
	}
	if u.Scheme == "https" {
		return nil
	}
	if isLoopbackHost(u.Hostname()) {
		return nil
	}
	if allowInsecure {
		return nil
	}
	return fmt.Errorf("server-url must use https for non-loopback hosts; set allow-insecure transport to override (testing only)")
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// LoopbackSecureJar wraps an http.CookieJar so plain-HTTP loopback origins are
// treated as secure contexts the way browsers do. Without it a Secure-flagged
// cookie (such as a fleet session cookie) would never be replayed to an
// http://localhost server.
type LoopbackSecureJar struct {
	inner http.CookieJar
}

// NewLoopbackSecureJar wraps inner so loopback http origins are treated as https
// for cookie storage and retrieval.
func NewLoopbackSecureJar(inner http.CookieJar) *LoopbackSecureJar {
	return &LoopbackSecureJar{inner: inner}
}

func (j *LoopbackSecureJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.inner.SetCookies(loopbackAsHTTPS(u), cookies)
}

func (j *LoopbackSecureJar) Cookies(u *url.URL) []*http.Cookie {
	return j.inner.Cookies(loopbackAsHTTPS(u))
}

// loopbackAsHTTPS returns u with its scheme rewritten to https when it is a
// plain-http loopback origin, so Secure cookies are stored and sent for local
// servers; other URLs are returned unchanged.
func loopbackAsHTTPS(u *url.URL) *url.URL {
	if u.Scheme != "http" || !isLoopbackHost(u.Hostname()) {
		return u
	}
	clone := *u
	clone.Scheme = "https"
	return &clone
}
