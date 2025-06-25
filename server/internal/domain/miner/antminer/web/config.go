package web

import (
	"net/url"
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

func NewAntminerConnectionInfo(connectionInfo networking.ConnectionInfo, username string, password secrets.Text) *AntminerConnectionInfo {
	return &AntminerConnectionInfo{
		ConnectionInfo: connectionInfo,
		Username:       username,
		Password:       password,
	}
}

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

	connInfo, err := networking.NewConnectionInfo(host, port, protocol)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create connection info: %v", err)
	}

	return &AntminerConnectionInfo{
		ConnectionInfo: *connInfo,
		Username:       username,
		Password:       password,
	}, nil
}
