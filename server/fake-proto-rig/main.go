// Package main implements a fake Proto miner simulator for testing.
//
// This simulator implements the same REST API interfaces as real Proto miners,
// allowing the fleet management system to be tested without physical hardware.
//
// The simulator supports:
//   - All REST API endpoints matching the MDK-API OpenAPI spec
//   - Stateful simulation of mining state, pools, and configuration
//   - Error injection via environment variables for testing error handling
//   - Realistic telemetry data with random variation
package main

import (
	"context"
	"fmt"
	"hash/crc32"
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
)

const (
	defaultHTTPPort       = 8080
	serverShutdownTimeout = 10 * time.Second
)

func main() {
	// Identity derives from the container hostname so it survives restarts
	// (real rigs keep their serial across reboots).
	instanceID := stableInstanceID()
	serialNumber := getEnv("SERIAL_NUMBER", "PROTO-SIM-"+instanceID)
	macAddress := getEnv("MAC_ADDRESS", generateMACAddress(instanceID))

	log.Printf("Starting fake Proto miner: SN=%s MAC=%s", serialNumber, macAddress)

	// Create miner state
	state := NewMinerState(serialNumber, macAddress)
	configureStartupAuthState(state)

	// Apply error configuration from environment
	applyErrorConfig(state)

	// Set IP address based on outbound interface
	state.IPAddress = getOutboundIP().String()

	// Fake telemetry-service gRPC stream (consumed by the rig-otlp-bridge
	// sidecar). TELEMETRY_GRPC_PORT=0 disables it.
	telemetryPort := getEnvInt("TELEMETRY_GRPC_PORT", defaultTelemetryGRPCPort)
	if telemetryPort != 0 {
		publishSeconds := getEnvInt("TELEMETRY_PUBLISH_INTERVAL_S", defaultTelemetryPublishSeconds)
		if publishSeconds <= 0 {
			// time.NewTicker panics on non-positive intervals; only
			// TELEMETRY_GRPC_PORT=0 disables telemetry.
			log.Printf("TELEMETRY_PUBLISH_INTERVAL_S=%d is invalid; using default %ds", publishSeconds, defaultTelemetryPublishSeconds)
			publishSeconds = defaultTelemetryPublishSeconds
		}
		publishInterval := time.Duration(publishSeconds) * time.Second
		if err := startTelemetryGRPCServer(state, telemetryPort, publishInterval); err != nil {
			log.Fatalf("Failed to start telemetry gRPC server: %v", err)
		}
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start HTTP server
	port := getEnvInt("HTTP_PORT", defaultHTTPPort)
	// Support legacy GRPC_PORT env var for backwards compatibility during migration
	if legacyPort := os.Getenv("GRPC_PORT"); legacyPort != "" {
		if p, err := strconv.Atoi(legacyPort); err == nil {
			port = p
		}
	}
	if err := startHTTPServer(ctx, state, port); err != nil {
		log.Fatalf("Failed to start HTTP server: %v", err)
	}
}

func startHTTPServer(ctx context.Context, state *MinerState, port int) error {
	mux := http.NewServeMux()

	// Create REST API handler
	restHandler := NewRESTApiHandler(state)
	restHandler.RegisterRoutes(mux)

	// Add health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
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

	log.Printf("HTTP server listening on :%d", port)
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

func configureStartupAuthState(state *MinerState) {
	// FAKE_RIG_DEFAULT_PASSWORD leaves the default-password gate active;
	// FAKE_RIG_PASSWORD seeds an already-changed password with the gate off.
	if defaultPassword := os.Getenv("FAKE_RIG_DEFAULT_PASSWORD"); defaultPassword != "" {
		state.SeedDefaultPassword(defaultPassword)
	}
	if password := os.Getenv("FAKE_RIG_PASSWORD"); password != "" {
		state.SetPassword(password)
	}
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

func stableInstanceID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return uuid.New().String()[:8]
	}
	return fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(host)))
}

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
