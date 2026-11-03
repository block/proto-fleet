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
	"sync"
	"syscall"
	"time"
)

func main() {
	fleet, err := loadMinerFleet()
	if err != nil {
		log.Fatal(err)
	}

	runMinerFleet(fleet, ":4028", ":80")
}

func runMinerFleet(fleet *minerFleet, rpcAddress, httpAddress string) {
	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := fleet.startMetrics(context.Background()); err != nil {
		log.Fatal(err)
	}

	defer fleet.stopMetrics()

	// Create wait group for server goroutines
	var wg sync.WaitGroup

	// Start RPC server (handles cgminer API)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := startRPCServer(ctx, fleet, rpcAddress); err != nil && err != context.Canceled {
			log.Printf("RPC server error: %v", err)
		}
	}()

	// Start HTTP server (handles web API)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := startHTTPServer(ctx, fleet, httpAddress); err != nil && err != context.Canceled {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs

	log.Println("Shutting down fake Antminer server...")
	cancel()

	// Wait for servers to shutdown gracefully
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("Shutdown completed")
	case <-time.After(30 * time.Second):
		log.Println("Shutdown timeout exceeded")
	}
}

func startRPCServer(ctx context.Context, fleet *minerFleet, address string) error {
	// Listen on the standard cgminer API port
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to start RPC server: %w", err)
	}
	defer listener.Close()
	var workers sync.WaitGroup
	defer workers.Wait()

	// Handle graceful shutdown
	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	log.Printf("RPC server listening on %s", address)

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				log.Printf("Failed to accept connection: %v", err)
				continue
			}
		}

		workers.Add(1)
		go func() {
			defer workers.Done()
			// Accepted connections drain through shutdown; rpcConnectionTimeout bounds them.
			fleet.handleRPC(context.WithoutCancel(ctx), conn)
		}()
	}
}

func createHTTPHandler(state *MinerState) http.Handler {
	mux := http.NewServeMux()
	if state.LoadMode {
		mux.Handle("/fake-api/v1/load/led", digestAuthMiddleware(state)(loadLEDAudit(state)))
	}

	// Register API endpoints
	mux.HandleFunc("/cgi-bin/get_system_info.cgi", createSystemInfoHandler(state))
	mux.HandleFunc("/cgi-bin/summary.cgi", createMinerSummaryHandler(state))
	mux.HandleFunc("/cgi-bin/get_miner_conf.cgi", createMinerConfigHandler(state))
	mux.HandleFunc("/cgi-bin/get_network_info.cgi", createNetworkInfoHandler(state))
	mux.HandleFunc("/cgi-bin/set_miner_conf.cgi", createSetConfigHandler(state))
	mux.HandleFunc("/cgi-bin/reboot.cgi", createRebootHandler(state))
	mux.HandleFunc("/cgi-bin/blink.cgi", createBlinkHandler(state))
	mux.HandleFunc("/cgi-bin/stats.cgi", createStatsHandler(state))
	mux.HandleFunc("/cgi-bin/get_kernel_log.cgi", createKernelLogHandler(state))
	mux.HandleFunc("/cgi-bin/passwd.cgi", createPasswordChangeHandler(state))
	mux.HandleFunc("/cgi-bin/upgrade.cgi", createUpgradeHandler(state))

	// Add health check endpoint (no auth required)
	mux.HandleFunc("/health", createHealthHandler())

	// Wrap the mux with authentication middleware
	protectedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if this is a path that needs authentication
		if pathNeedsAuth(r.URL.Path) {
			// Apply auth middleware for protected endpoints
			digestAuthMiddleware(state)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mux.ServeHTTP(w, r)
			})).ServeHTTP(w, r)
		} else {
			// No auth needed, proceed normally
			mux.ServeHTTP(w, r)
		}
	})

	return protectedHandler
}

func startHTTPServer(ctx context.Context, fleet *minerFleet, address string) error {
	var connections sync.WaitGroup
	server := &http.Server{
		Addr:    address,
		Handler: fleet,
		// A forced Close cancels requests after the grace period, but it does not
		// join their goroutines. Keep their deferred metrics inside the lifetime.
		ConnState: func(_ net.Conn, state http.ConnState) {
			switch state {
			case http.StateNew:
				connections.Add(1)
			case http.StateClosed, http.StateHijacked:
				connections.Done()
			}
		},
	}

	// ListenAndServe returns when listeners close, before Shutdown drains handlers.
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
		}
	}()

	log.Printf("HTTP server listening on %s", address)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}
	<-shutdownDone
	connections.Wait()
	return nil
}

// Helper function to determine if a path needs authentication
func pathNeedsAuth(path string) bool {
	// Enforce auth for sensitive GET endpoints and POST endpoints that modify state
	return path == "/cgi-bin/get_system_info.cgi" ||
		path == "/cgi-bin/get_miner_conf.cgi" ||
		path == "/cgi-bin/stats.cgi" ||
		path == "/cgi-bin/get_kernel_log.cgi" ||
		path == "/cgi-bin/set_miner_conf.cgi" ||
		path == "/cgi-bin/reboot.cgi" ||
		path == "/cgi-bin/blink.cgi" ||
		path == "/cgi-bin/passwd.cgi" ||
		path == "/cgi-bin/upgrade.cgi"
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists && value != "" {
		return value
	}
	return fallback
}

func getEnvAllowEmpty(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

// validateConfig validates the configuration parameters
func validateConfig(minerType, serialNumber, macAddress string) error {
	if minerType == "" {
		return fmt.Errorf("miner type cannot be empty")
	}
	if serialNumber == "" {
		return fmt.Errorf("serial number cannot be empty")
	}
	if macAddress == "" {
		return fmt.Errorf("MAC address cannot be empty")
	}
	// Basic MAC address format validation
	if len(macAddress) != 17 {
		return fmt.Errorf("invalid MAC address format: %s", macAddress)
	}
	return nil
}

func getIPAddr() string {
	if ip := getEnv("IP_ADDRESS", ""); ip != "" {
		return ip
	}
	return getOutboundIP().String()
}

// getOutboundIP gets the preferred outbound IP of this machine.
// If the container has no outbound route, fall back to the first non-loopback
// IPv4 address so fake miners can run on internal-only Docker networks.
func getOutboundIP() net.IP {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		localAddr := conn.LocalAddr().(*net.UDPAddr)
		return localAddr.IP
	}

	addrs, addrErr := net.InterfaceAddrs()
	if addrErr != nil {
		log.Fatalf("failed to determine local IP after outbound probe failed: %v", addrErr)
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() {
			continue
		}
		if ip := ipNet.IP.To4(); ip != nil {
			return ip
		}
	}
	log.Fatalf("failed to determine local IP after outbound probe failed: %v", err)
	return nil
}

// Helper function to generate a standardized error response
func errorResponse(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	fmt.Fprintf(w, `{"error": "%s"}`, message)
}

// parseErrorConfig reads error configuration from environment variables
func parseErrorConfig() ErrorConfig {
	return ErrorConfig{
		BoardTemperature:   parseFloat("ERROR_BOARD_TEMP", 0),
		HWErrorPercent:     parseFloat("ERROR_HW_PERCENT", 0),
		HWErrorCount:       parseInt("ERROR_HW_COUNT", 0),
		RejectedPercent:    parseFloat("ERROR_REJECTED_PERCENT", 0),
		RejectedCount:      parseInt("ERROR_REJECTED_COUNT", 0),
		StaleCount:         parseInt("ERROR_STALE_COUNT", 0),
		BoardDisabled:      parseBool("ERROR_BOARD_DISABLED", false),
		BoardNotAlive:      parseBool("ERROR_BOARD_NOT_ALIVE", false),
		BoardNotHashing:    parseBool("ERROR_BOARD_NOT_HASHING", false),
		FanFailed:          parseBool("ERROR_FAN_FAILED", false),
		PSUFault:           parseBool("ERROR_PSU_FAULT", false),
		PoolNotAlive:       parseBool("ERROR_POOL_NOT_ALIVE", false),
		PoolGetFailures:    parseInt("ERROR_POOL_GET_FAILURES", 0),
		PoolRemoteFailures: parseInt("ERROR_POOL_REMOTE_FAILURES", 0),
	}
}

func parseFloat(key string, defaultValue float64) float64 {
	if val := os.Getenv(key); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	}
	return defaultValue
}

func parseInt(key string, defaultValue int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultValue
}

func parseBool(key string, defaultValue bool) bool {
	if val := os.Getenv(key); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			return b
		}
	}
	return defaultValue
}

func newMinerState(ip string) (*MinerState, error) {
	// Get configuration from environment variables with validation
	minerType := getEnv("MINER_TYPE", "Antminer S19j Pro")
	serialNumber := getEnv("SERIAL_NUMBER", "fake-antminer-1")
	macAddress := getEnv("MAC_ADDRESS", "00:11:22:33:44:55")
	firmwareVersion := getEnv("FIRMWARE_VERSION", "2.0.0")

	// Validate required environment variables
	if err := validateConfig(minerType, serialNumber, macAddress); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	// Parse error configuration from environment
	errorConfig := parseErrorConfig()

	// Create miner state
	return &MinerState{
		digestNonce:     generateNonce(),
		MinerType:       minerType,
		SerialNumber:    serialNumber,
		MacAddress:      macAddress,
		FirmwareVersion: firmwareVersion,
		IPAddress:       ip,
		Hostname:        "antminer-" + serialNumber,
		NetMask:         getEnv("NETMASK", "255.255.255.0"),
		Gateway:         getEnv("GATEWAY", "192.168.2.1"),
		DNSServers:      getEnvAllowEmpty("DNS_SERVERS", "8.8.8.8"),
		HashRate:        110.0,
		Temperature:     45.0,
		Pools: []Pool{
			{
				URL:  "stratum+tcp://btc.example.com:3333",
				User: "worker1",
				Pass: "x",
			},
		},
		BitmainWorkMode: WorkModeNormal,
		Username:        getEnv("USERNAME", "root"),
		Password:        getEnv("PASSWORD", "root"),
		ErrorConfig:     errorConfig,
	}, nil
}
