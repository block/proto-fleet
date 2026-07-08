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
