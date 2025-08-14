package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"

	"connectrpc.com/connect"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_common_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_system_api/miner_system_apiconnect"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto/client"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: go run debug_client.go <URL>")
	}

	targetURL := os.Args[1]

	// Enable HTTP debugging
	os.Setenv("GODEBUG", "http2debug=1")

	// Parse the URL to determine protocol and connection info
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		log.Fatalf("Failed to parse URL: %v", err)
	}

	// Determine protocol from URL scheme
	var protocol networking.Protocol
	switch strings.ToLower(parsedURL.Scheme) {
	case "http":
		protocol = networking.ProtocolHTTP
	case "https":
		protocol = networking.ProtocolHTTPS
	default:
		log.Fatalf("Unsupported protocol: %s", parsedURL.Scheme)
	}

	// Extract host and port
	host := parsedURL.Hostname()
	portStr := parsedURL.Port()
	if portStr == "" {
		if protocol == networking.ProtocolHTTPS {
			portStr = "443"
		} else {
			portStr = "80"
		}
	}

	// Convert port string to uint16
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		log.Fatalf("Invalid port number: %v", err)
	}

	// Create connection info
	connectionInfo := networking.ConnectionInfo{
		IPAddress: networking.IPAddress(host),
		Port:      networking.Port(port),
		Protocol:  protocol,
	}

	fmt.Printf("Testing connection to: %s (Protocol: %s)\n", targetURL, protocol.String())

	// Create a debug client using the create_client code
	debugClient, err := client.CreateClient(
		miner_system_apiconnect.NewMinerPairingApiClient,
		connectionInfo,
	)
	if err != nil {
		log.Fatalf("Failed to create debug client: %v", err)
	}

	pairingInfo, err := debugClient.GetPairingInfo(context.Background(), connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		log.Fatalf("Failed to get pairing info: %v", err)
	}

	fmt.Printf("Pairing info: %v\n", pairingInfo)
}
