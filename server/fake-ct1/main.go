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
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
)

const (
	defaultHTTPPort       = 8080
	serverShutdownTimeout = 10 * time.Second
)

func main() {
	webRoot := flag.String("web-root", "", "Serve a built ProtoOS directory on the API origin (disabled by default)")
	flag.Parse()

	cfg, err := containerConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	address, err := parseListenAddress(os.Getenv("LISTEN_ADDRESS"))
	if err != nil {
		log.Fatal(err)
	}
	state := NewContainerState(cfg)
	state.IPAddress = getOutboundIP().String()
	if address != "" {
		state.IPAddress = address
	}

	log.Printf(
		"Starting fake CT1 container: serverID=%s tanks=%d modules=%d boards=%d",
		cfg.ServerID, cfg.Tanks, state.ModuleCount(), state.BoardCount(),
	)

	grpcPort, httpPort := getEnvPort("GRPC_PORT", 9095), getEnvPort("HTTP_PORT", defaultHTTPPort)
	pair, enabled, err := haPairSettingsFromEnv(cfg, grpcPort, httpPort)
	if err != nil {
		log.Fatal(err)
	}
	if enabled {
		peer := NewContainerState(pair.peerConfig(cfg))
		peer.IPAddress = state.IPAddress
		sim := newHAPair(state, peer, pair)
		log.Printf("CT1_HA_PAIR: controllers %s and %s, cluster %s, primary %s", state.ServerID, peer.ServerID, pair.ClusterID, pair.Update.Primary)
		if err := runHAPair(state, peer, sim, address, grpcPort, httpPort, pair.PeerGRPCPort, pair.PeerHTTPPort, *webRoot); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := runContainer(state, address, grpcPort, httpPort, *webRoot); err != nil {
		log.Fatal(err)
	}
}

func containerConfigFromEnv() (ContainerConfig, error) {
	unlocatedBoards, err := parseUnlocatedBoards(os.Getenv("CT1_UNLOCATED_BOARDS"))
	if err != nil {
		return ContainerConfig{}, err
	}
	instanceID := simulatorInstanceID()
	cfg := ContainerConfig{
		InstanceID:        instanceID,
		IdentityNamespace: getEnv("CT1_IDENTITY_NAMESPACE", getEnv("CT1_INSTANCE_ID", os.Getenv("SERVER_ID"))),
		Seed:              getEnvInt("CT1_SEED", 1),
		ServerID:          getEnv("SERVER_ID", "ct1-sim-"+instanceID),
		Hostname:          getEnv("HOSTNAME", "fake-ct1-"+instanceID),
		Version:           getEnv("CT1_VERSION", defaultVersion),
		Tanks:             getEnvInt("CT1_TANKS", defaultTanks),
		ModulesPerTank:    getEnvInt("CT1_MODULES_PER_TANK", defaultModulesPerTank),
		SlotsPerModule:    getEnvInt("CT1_SLOTS_PER_MODULE", defaultSlotsPerModule),
		AsicsPerBoard:     getEnvInt("CT1_ASICS_PER_BOARD", defaultAsicsPerBoard),
		DemoModuleStates:  getEnv("CT1_DEMO_MODULE_STATES", "true") == "true",
		UnlocatedBoards:   unlocatedBoards,

		DemoAllBoardFaults: getEnv("CT1_DEMO_ALL_BOARD_FAULTS", "false") == "true",
		DemoPlantAlarms:    getEnv("CT1_DEMO_PLANT_ALARMS", "false") == "true",
		// Real CT1 hardware cannot reset alarms; set false to mimic it.
		AlarmResetUnsupported: getEnv("CT1_ALARM_RESET_SUPPORTED", "true") == "false",
	}

	cfg.OccupiedBoards, err = parseOccupiedBoards(os.Getenv("CT1_OCCUPIED_BOARDS"), cfg.Tanks*cfg.ModulesPerTank*cfg.SlotsPerModule)
	if err != nil {
		return ContainerConfig{}, err
	}
	cfg.ResponseDelay, err = parseResponseDelay(os.Getenv("FAKE_MINER_RESPONSE_DELAY"))
	return cfg, err
}

func runContainer(state *ContainerState, address string, rpcPort, httpPort int, webRoot string, options ...grpc.ServerOption) error {
	// Keep repeated termination signals intercepted until both transports have
	// drained and the deferred final metrics snapshot has been written.
	ctx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	metricsFile := os.Getenv("FAKE_MINER_METRICS_FILE")
	if state.ha != nil && !state.ha.primaryMetrics {
		// One metrics file cannot be shared by both pair members.
		metricsFile = ""
	}
	stopMetrics, err := state.startMetrics(context.Background(), metricsFile)
	if err != nil {
		return err
	}
	defer func() {
		log.Println("Flushing final fake CT1 metrics...")
		stopMetrics()
	}()

	rpcListener, err := net.Listen("tcp", net.JoinHostPort(address, strconv.Itoa(rpcPort)))
	if err != nil {
		cancel()
		return fmt.Errorf("listen for gRPC: %w", err)
	}
	rpcServer := newGRPCServer(state, options...)
	rpcShutdownDone := make(chan struct{})
	go func() {
		defer close(rpcShutdownDone)
		<-ctx.Done()
		stopGRPCServer(rpcServer)
	}()
	defer func() {
		cancel()
		<-rpcShutdownDone
	}()
	go func() {
		if err := rpcServer.Serve(rpcListener); err != nil {
			log.Printf("gRPC server failed: %v", err)
			cancel()
		}
	}()
	log.Printf("gRPC server listening on %s", rpcListener.Addr())
	err = startHTTPServer(ctx, state, address, httpPort, webRoot)
	cancel()
	return err
}

func startHTTPServer(ctx context.Context, state *ContainerState, address string, port int, webRoot string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	handler, closeHandler, err := newHTTPHandler(state, webRoot)
	if err != nil {
		return err
	}
	defer closeHandler()

	var connections sync.WaitGroup
	server := &http.Server{
		Addr:              net.JoinHostPort(address, strconv.Itoa(port)),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// Bound total request and response time. Every handler holds the state
		// mutex while reading the body / streaming the (up to 288-board)
		// response, so an unbounded stall would pin the lock indefinitely.
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		ConnState: func(_ net.Conn, state http.ConnState) {
			switch state {
			case http.StateNew:
				connections.Add(1)
			case http.StateClosed, http.StateHijacked:
				connections.Done()
			}
		},
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

		<-ctx.Done()
		log.Println("Shutting down fake CT1 container...")

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("Server shutdown error: %v", err)
			_ = server.Close()
		}
	}()

	log.Printf("HTTP server listening on %s", listener.Addr())
	serveErr := server.Serve(listener)
	cancel()
	<-shutdownDone
	connections.Wait()
	if serveErr != nil && serveErr != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", serveErr)
	}
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

func parseOccupiedBoards(value string, capacity int) (int, error) {
	if value == "" {
		return 0, nil
	}
	count, err := strconv.Atoi(value)
	if err != nil || count < 1 || count > capacity {
		return 0, fmt.Errorf("invalid CT1_OCCUPIED_BOARDS=%q: expected 1..%d", value, capacity)
	}
	return count, nil
}

func parseResponseDelay(value string) (time.Duration, error) {
	if value == "" {
		return 0, nil
	}
	delay, err := time.ParseDuration(value)
	if err != nil || delay < 0 {
		return 0, fmt.Errorf("invalid FAKE_MINER_RESPONSE_DELAY=%q: expected nonnegative duration", value)
	}
	return delay, nil
}

func parseListenAddress(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	address := net.ParseIP(value)
	if address == nil || address.IsUnspecified() {
		return "", fmt.Errorf("invalid LISTEN_ADDRESS=%q: expected a specific IP address", value)
	}
	return address.String(), nil
}
