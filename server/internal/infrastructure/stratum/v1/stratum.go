package stratum

import (
	"context"
	"fmt"
	"net"
	netUrl "net/url"
	"strings"

	"github.com/block/proto-fleet/server/internal/infrastructure/secrets"
	"github.com/sourcegraph/jsonrpc2"
)

const (
	stratumPrefix = "stratum+"
	stratumPort   = "3333"
)

// rpcRequest and rpcResponse are used with PlainObjectStream directly instead of
// jsonrpc2.NewConn, which panics on id:null notifications from stratum servers.
type rpcRequest struct {
	ID     int    `json:"id"`
	Method string `json:"method"`
	Params []any  `json:"params"`
}

type rpcResponse struct {
	ID     any `json:"id"`
	Result any `json:"result"`
	Error  any `json:"error"`
}

// Authenticate returns true if the authentication is successful, otherwise false.
// It also returns an error if there is any issue during the connection or authentication process.
// The context can be used to set a timeout for the authentication process.
// The url is expected to be in the format "stratum+<protocol>://address".
// The Password is optional and can be nil if not required by the Stratum server.
// TODO: Add custom error types for better error handling with connectGRPC
func Authenticate(ctx context.Context, url string, username string, password *secrets.Text) (bool, error) {
	protocol, address, err := parseStratumURL(url)
	if err != nil {
		return false, err
	}

	netConn, err := net.Dial(protocol, address)
	if err != nil {
		return false, fmt.Errorf("unable to dial: %v", err)
	}
	defer netConn.Close()

	// Apply context deadline to the connection
	if deadline, ok := ctx.Deadline(); ok {
		if err := netConn.SetDeadline(deadline); err != nil {
			return false, fmt.Errorf("unable to set deadline: %v", err)
		}
	}

	// Use PlainObjectStream directly instead of jsonrpc2.NewConn because NewConn
	// spawns a background goroutine that panics when stratum servers send
	// notifications with id:null (which the library doesn't support).
	stream := jsonrpc2.NewPlainObjectStream(netConn)
	defer stream.Close()

	request := &AuthRequest{
		Username: username,
		Password: password,
	}

	req := rpcRequest{
		ID:     1,
		Method: request.Method(),
		Params: request.MarshalParams(),
	}

	if err := stream.WriteObject(req); err != nil {
		return false, fmt.Errorf("failed to send authentication request: %v", err)
	}

	var resp rpcResponse
	if err := stream.ReadObject(&resp); err != nil {
		return false, fmt.Errorf("failed to receive authentication response: %v", err)
	}

	if resp.Error != nil {
		return false, fmt.Errorf("authentication failed: %v", resp.Error)
	}

	if resp.Result == nil {
		return false, fmt.Errorf("server returned null result")
	}

	response, ok := resp.Result.(bool)
	if !ok {
		return false, fmt.Errorf("unexpected result type: %T", resp.Result)
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
