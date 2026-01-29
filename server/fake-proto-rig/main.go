// Package main implements a fake Proto miner simulator for testing.
//
// This simulator implements the same gRPC/Connect-RPC interfaces as real Proto miners,
// allowing the fleet management system to be tested without physical hardware.
//
// The simulator supports:
//   - All MinerDataApi, MinerCommandApi, MinerSystemApi, and MinerPairingApi endpoints
//   - Stateful simulation of mining state, pools, and configuration
//   - Error injection via environment variables for testing error handling
//   - Realistic telemetry data with random variation
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"connectrpc.com/connect"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_command_api/miner_command_apiconnect"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_data_api/miner_data_apiconnect"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_system_api/miner_system_apiconnect"
)

const (
	defaultGRPCPort       = 2121
	serverShutdownTimeout = 10 * time.Second
)

func main() {
	// Generate unique identifiers for this instance
	instanceID := uuid.New().String()[:8]
	serialNumber := getEnv("SERIAL_NUMBER", "PROTO-SIM-"+instanceID)
	macAddress := getEnv("MAC_ADDRESS", generateMACAddress(instanceID))

	log.Printf("Starting fake Proto miner: SN=%s MAC=%s", serialNumber, macAddress)

	// Create miner state
	state := NewMinerState(serialNumber, macAddress)

	// Apply error configuration from environment
	applyErrorConfig(state)

	// Set IP address based on outbound interface
	state.IPAddress = getOutboundIP().String()

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start gRPC server
	port := getEnvInt("GRPC_PORT", defaultGRPCPort)
	if err := startGRPCServer(ctx, state, port); err != nil {
		log.Fatalf("Failed to start gRPC server: %v", err)
	}
}

func startGRPCServer(ctx context.Context, state *MinerState, port int) error {
	mux := http.NewServeMux()

	// Create service handlers
	dataHandler := NewDataApiHandler(state)
	commandHandler := NewCommandApiHandler(state)
	systemHandler := NewSystemApiHandler(state)
	pairingHandler := NewPairingApiHandler(state)

	// Create REST API handler for ProtoOS compatibility
	restHandler := NewRESTApiHandler(state)
	restHandler.RegisterRoutes(mux)

	// Create interceptors for auth handling
	interceptors := connect.WithInterceptors(
		newAuthInterceptor(state),
	)

	// Register service handlers with interceptors (auth for protected endpoints)
	mux.Handle(miner_data_apiconnect.NewMinerDataApiHandler(dataHandler, interceptors))
	mux.Handle(miner_command_apiconnect.NewMinerCommandApiHandler(commandHandler, interceptors))
	mux.Handle(miner_system_apiconnect.NewMinerSystemApiHandler(systemHandler, interceptors))
	// Pairing API does NOT require auth
	mux.Handle(miner_system_apiconnect.NewMinerPairingApiHandler(pairingHandler))

	// Add health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Create HTTP/2 cleartext server (h2c)
	h2s := &http2.Server{}
	handler := h2c.NewHandler(mux, h2s)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: handler,
	}

	// Start listening
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", port, err)
	}

	// Handle graceful shutdown
	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

		select {
		case <-sigs:
			log.Println("Shutting down fake Proto miner...")
		case <-ctx.Done():
		}

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
	}()

	log.Printf("gRPC server listening on :%d", port)
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

// authInterceptor validates Bearer tokens for protected endpoints.
type authInterceptor struct {
	state *MinerState
}

func newAuthInterceptor(state *MinerState) connect.Interceptor {
	return &authInterceptor{state: state}
}

func (i *authInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		// Get auth header
		authHeader := req.Header().Get("Authorization")

		// Check if we require auth (auth key is set)
		if i.state.GetAuthKey() != "" {
			if authHeader == "" {
				return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("missing API key"))
			}

			// Extract bearer token
			if !strings.HasPrefix(authHeader, "Bearer ") {
				return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("invalid authorization header format"))
			}

			// For simulation purposes, we accept any non-empty token
			// In a real implementation, this would validate the JWT signature
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token == "" {
				return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("empty bearer token"))
			}
		}

		return next(ctx, req)
	}
}

func (i *authInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *authInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

// applyErrorConfig reads error configuration from environment variables.
func applyErrorConfig(state *MinerState) {
	// Temperature override
	if temp := getEnvFloat("ERROR_TEMPERATURE", 0); temp > 0 {
		state.ErrorConfig.OverrideTemperature = temp
		log.Printf("Error injection: temperature override = %.1f°C", temp)
	}

	// Hashboard missing
	if missing := getEnvIntSlice("ERROR_HASHBOARD_MISSING"); len(missing) > 0 {
		state.ErrorConfig.HashboardMissing = missing
		log.Printf("Error injection: missing hashboards = %v", missing)
	}

	// Hashboard error state
	if errors := getEnvIntSlice("ERROR_HASHBOARD_ERROR"); len(errors) > 0 {
		state.ErrorConfig.HashboardErrorState = errors
		log.Printf("Error injection: hashboards in error state = %v", errors)
	}

	// PSU missing
	if missing := getEnvIntSlice("ERROR_PSU_MISSING"); len(missing) > 0 {
		state.ErrorConfig.PSUMissing = missing
		log.Printf("Error injection: missing PSUs = %v", missing)
	}

	// PSU error state
	if errors := getEnvIntSlice("ERROR_PSU_ERROR"); len(errors) > 0 {
		state.ErrorConfig.PSUErrorState = errors
		log.Printf("Error injection: PSUs in error state = %v", errors)
	}

	// Pools offline
	if getEnvBool("ERROR_POOLS_OFFLINE", false) {
		state.ErrorConfig.PoolsOffline = true
		log.Printf("Error injection: all pools offline")
	}
}

// Helper functions for environment variables

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if value := os.Getenv(key); value != "" {
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if value := os.Getenv(key); value != "" {
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
	}
	return fallback
}

func getEnvIntSlice(key string) []int {
	value := os.Getenv(key)
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	result := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if i, err := strconv.Atoi(p); err == nil {
			result = append(result, i)
		}
	}
	return result
}

// generateMACAddress generates a MAC address based on instance ID.
func generateMACAddress(instanceID string) string {
	// Use a fixed OUI prefix and derive the rest from instance ID
	hash := 0
	for _, c := range instanceID {
		hash = hash*31 + int(c)
	}

	return fmt.Sprintf("02:00:00:%02X:%02X:%02X",
		(hash>>16)&0xFF,
		(hash>>8)&0xFF,
		hash&0xFF,
	)
}

// getOutboundIP gets the preferred outbound IP of this machine.
func getOutboundIP() net.IP {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return net.ParseIP("127.0.0.1")
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP
}
