package web

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
)

type AntminerConnectionInfo struct {
	networking.ConnectionInfo
	Username string
	Password secrets.Text
}

// NewAntminerConnectionInfo creates a new connection info struct with the given parameters
func NewAntminerConnectionInfo(connectionInfo networking.ConnectionInfo, username string, password secrets.Text) *AntminerConnectionInfo {
	return &AntminerConnectionInfo{
		ConnectionInfo: connectionInfo,
		Username:       username,
		Password:       password,
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

	protocol, err := networking.ProtocolFromString(strings.ToLower(parsedURL.Scheme))
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("failed to parse protocol %v", err)
	}

	// Use default HTTP port if not specified
	if port == "" {
		if protocol == networking.ProtocolHTTPS {
			port = "443"
		} else {
			port = "80"
		}
	}

	portInt, err := strconv.Atoi(port)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("failed to parse port %v", err)
	}
	if portInt < 0 || portInt > 65535 {
		return nil, fleeterror.NewInvalidArgumentErrorf("port out of range: %d", portInt)
	}

	return &AntminerConnectionInfo{
		ConnectionInfo: networking.ConnectionInfo{
			IPAddress: networking.IPAddress(host),
			Port:      networking.Port(portInt),
			Protocol:  protocol,
		},
		Username: username,
		Password: password,
	}, nil
}
