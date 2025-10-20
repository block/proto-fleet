package web

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/btc-mining/proto-fleet/plugin/antminer/pkg/antminer/networking"
	"github.com/btc-mining/proto-fleet/server/sdk/v1"
)

type AntminerConnectionInfo struct {
	networking.ConnectionInfo
	Creds sdk.UsernamePassword
}

func NewAntminerConnectionInfo(connectionInfo networking.ConnectionInfo, credential sdk.UsernamePassword) *AntminerConnectionInfo {
	return &AntminerConnectionInfo{
		ConnectionInfo: connectionInfo,
		Creds:          credential,
	}
}

func NewAntminerConnectionInfoFromURL(urlStr string, creds sdk.UsernamePassword) (*AntminerConnectionInfo, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL %v", err)
	}

	// Extract host and port
	host := parsedURL.Hostname()
	port := parsedURL.Port()

	protocol, err := networking.ProtocolFromString(strings.ToLower(parsedURL.Scheme))
	if err != nil {
		return nil, fmt.Errorf("failed to parse protocol %v", err)
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
		return nil, fmt.Errorf("failed to create connection info: %v", err)
	}

	return &AntminerConnectionInfo{
		ConnectionInfo: *connInfo,
		Creds:          creds,
	}, nil
}
