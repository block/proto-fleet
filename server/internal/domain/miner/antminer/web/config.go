package web

import (
	"net/url"
	"strings"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
)

type AntminerConnectionInfo struct {
	IPAddress string
	Port      string
	Username  string
	Password  secrets.Text
}

// NewAntminerConnectionInfo creates a new connection info struct with the given parameters
func NewAntminerConnectionInfo(ipAddress, port, username string, password secrets.Text) *AntminerConnectionInfo {
	return &AntminerConnectionInfo{
		IPAddress: ipAddress,
		Port:      port,
		Username:  username,
		Password:  password,
	}
}

// NewAntminerConnectionInfoFromURL creates a connection info struct from a URL string
func NewAntminerConnectionInfoFromURL(urlStr, username string, password secrets.Text) (*AntminerConnectionInfo, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("failed to parse URL %v", err)
	}

	// Extract host and port
	host := parsedURL.Hostname()
	port := parsedURL.Port()

	// Use default HTTP port if not specified
	if port == "" {
		if strings.ToLower(parsedURL.Scheme) == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	return &AntminerConnectionInfo{
		IPAddress: host,
		Port:      port,
		Username:  username,
		Password:  password,
	}, nil
}
