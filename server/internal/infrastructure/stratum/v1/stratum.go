package stratum

import (
	"context"
	"fmt"
	"net"
	netUrl "net/url"
	"strings"

	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
	"github.com/sourcegraph/jsonrpc2"
)

const (
	stratumPrefix = "stratum+"
	stratumPort   = "3333"
)

// Authenticate returns true if the authentication is successful, otherwise false.
// It also returns an error if there is any issue during the connection or authentication process.
// The context can be used to set a timeout for the authentication process.
// The url is expected to be in the format "stratum+<protocol>://address".
// The Password is optional and can be nil if not required by the Stratum server.
// TODO(briano): Add custom error types for better error handling with connectGRPc
func Authenticate(ctx context.Context, url string, username string, password *secrets.Text) (bool, error) {
	protocol, address, err := parseStratumURL(url)
	if err != nil {
		return false, err
	}

	netConn, err := net.Dial(protocol, address)
	if err != nil {
		return false, fmt.Errorf("unable to dial: %v", err)
	}

	conn := jsonrpc2.NewConn(ctx, jsonrpc2.NewPlainObjectStream(netConn), nil)
	defer conn.Close()

	request := &AuthRequest{
		Username: username,
		Password: password,
	}

	var response bool
	err = conn.Call(ctx, request.Method(), request.MarshalParams(), &response)
	if err != nil {
		return false, fmt.Errorf("unable to call: %v", err)
	}

	return response, nil
}

// Returns protocol, address and error
// protocol is minus "stratum+" for the net package.
func parseStratumURL(url string) (string, string, error) {
	parsedURL, err := netUrl.Parse(url)
	if err != nil {
		return "", "", fmt.Errorf("unable to parse provided url: %v", err)
	}

	protocol, _ := strings.CutPrefix(parsedURL.Scheme, stratumPrefix)
	address := []string{parsedURL.Hostname(), ":"}

	if parsedURL.Port() == "" {
		address = append(address, stratumPort)
	} else {
		address = append(address, parsedURL.Port())
	}

	return protocol, strings.Join(address, ""), nil
}
