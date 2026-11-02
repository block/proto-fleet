// Package main implements a fake CT1 container simulator for testing.
//
// This simulator implements the MDK v2 REST and native gRPC surfaces a fleet client hits
// against a real CT1 container, allowing the fleet management system to be
// developed and tested without real hardware and without running Frigate's
// heavy full-stack simulator (TimescaleDB + hbsim + containersim + ...).
//
// It mirrors server/fake-proto-rig (which fakes R2 rigs on the MDK v1 surface)
// but models a full container: a Tank -> Module -> Slot -> Hashboard tree of
// 96 modules x 3 boards = 288 boards, plant/PUE telemetry, and the scoped
// mining write operations.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/google/uuid"
)

const (
	defaultHTTPPort       = 8080
	serverShutdownTimeout = 10 * time.Second
)

func main() {
	webRoot := flag.String("web-root", "", "Serve a built ProtoOS directory on the API origin (disabled by default)")
	flag.Parse()

	unlocatedBoards, err := parseUnlocatedBoards(os.Getenv("CT1_UNLOCATED_BOARDS"))
	if err != nil {
		log.Fatal(err)
	}
	instanceID := simulatorInstanceID()

	cfg := ContainerConfig{
		InstanceID:       instanceID,
		Seed:             getEnvInt("CT1_SEED", 1),
		ServerID:         getEnv("SERVER_ID", "ct1-sim-"+instanceID),
		Hostname:         getEnv("HOSTNAME", "fake-ct1-"+instanceID),
		Version:          getEnv("CT1_VERSION", defaultVersion),
		Tanks:            getEnvInt("CT1_TANKS", defaultTanks),
		ModulesPerTank:   getEnvInt("CT1_MODULES_PER_TANK", defaultModulesPerTank),
		SlotsPerModule:   getEnvInt("CT1_SLOTS_PER_MODULE", defaultSlotsPerModule),
		AsicsPerBoard:    getEnvInt("CT1_ASICS_PER_BOARD", defaultAsicsPerBoard),
		DemoModuleStates: getEnv("CT1_DEMO_MODULE_STATES", "true") == "true",
		UnlocatedBoards:  unlocatedBoards,

		DemoAllBoardFaults: getEnv("CT1_DEMO_ALL_BOARD_FAULTS", "false") == "true",
	}

	state := NewContainerState(cfg)
	state.IPAddress = getOutboundIP().String()

	log.Printf(
		"Starting fake CT1 container: serverID=%s tanks=%d modules=%d boards=%d",
		cfg.ServerID, cfg.Tanks, state.ModuleCount(), state.BoardCount(),
	)

	ctx, cancel := context.WithCancel(context.Background())

	rpcListener, err := net.Listen("tcp", fmt.Sprintf(":%d", getEnvPort("GRPC_PORT", 9095)))
	if err != nil {
		cancel()
		log.Fatalf("Failed to listen for gRPC: %v", err)
	}
	rpcServer := newGRPCServer(state)
	defer rpcServer.Stop()
	go func() {
		if err := rpcServer.Serve(rpcListener); err != nil {
			log.Printf("gRPC server failed: %v", err)
			cancel()
		}
	}()
	log.Printf("gRPC server listening on %s", rpcListener.Addr())
	port := getEnvPort("HTTP_PORT", defaultHTTPPort)
	err = startHTTPServer(ctx, state, port, *webRoot)
	cancel()
	if err != nil {
		log.Fatalf("Failed to start HTTP server: %v", err)
	}
}

func startHTTPServer(ctx context.Context, state *ContainerState, port int, webRoot string) error {
	handler, closeHandler, err := newHTTPHandler(state, webRoot)
	if err != nil {
		return err
	}
	defer closeHandler()

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// Bound total request and response time. Every handler holds the state
		// mutex while reading the body / streaming the (up to 288-board)
		// response, so an unbounded stall would pin the lock indefinitely.
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", port, err)
	}

	// shutdownDone lets main() wait for a graceful drain: Shutdown closes the
	// listener so Serve returns immediately, and without this the process could
	// exit mid-drain.
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

		select {
		case <-sigs:
			log.Println("Shutting down fake CT1 container...")
		case <-ctx.Done():
		}

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
	}()

	log.Printf("HTTP server listening on %s", listener.Addr())
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}
	<-shutdownDone

	return nil
}

// Helper functions for environment variables.

func parseUnlocatedBoards(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	if value == "all" {
		return -1, nil
	}
	count, err := strconv.Atoi(value)
	if err != nil || count < 0 {
		return 0, fmt.Errorf("invalid CT1_UNLOCATED_BOARDS=%q: expected a nonnegative integer or all", value)
	}
	return count, nil
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// getEnvPort allows an ephemeral listener (0) for isolated process tests.
func getEnvPort(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 0 || port > 65535 {
		log.Printf("invalid %s=%q; using %d", key, value, fallback)
		return fallback
	}
	return port
}

// getEnvInt reads a positive integer env var. Topology knobs (CT1_TANKS, etc.)
// must be >= 1, so a zero/negative/garbage value logs and falls back rather
// than silently building an empty container.
func getEnvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	i, err := strconv.Atoi(value)
	if err != nil || i <= 0 {
		log.Printf("invalid %s=%q; using %d", key, value, fallback)
		return fallback
	}
	return i
}

// getOutboundIP gets the preferred outbound IP of this machine.
func getOutboundIP() net.IP {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return net.ParseIP("127.0.0.1")
	}
	defer conn.Close()

	localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return net.ParseIP("127.0.0.1")
	}
	return localAddr.IP
}

// A configured controller must retain its board serials across process restarts.
// CT1_INSTANCE_ID preserves the random prefix of an already-paired legacy fixture.
func simulatorInstanceID() string {
	if id := os.Getenv("CT1_INSTANCE_ID"); id != "" {
		return id
	}
	if id := os.Getenv("SERVER_ID"); id != "" {
		return id
	}
	return uuid.New().String()[:8]
}
