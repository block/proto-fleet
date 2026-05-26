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
	// Get configuration from environment variables with validation
	minerType := getEnv("MINER_TYPE", "Antminer S19j Pro")
	serialNumber := getEnv("SERIAL_NUMBER", "fake-antminer-1")
	macAddress := getEnv("MAC_ADDRESS", "00:11:22:33:44:55")
	firmwareVersion := getEnv("FIRMWARE_VERSION", "Antminer S19j Pro 110Th 28/11/2022 16:51:53")

	// Validate required environment variables
	if err := validateConfig(minerType, serialNumber, macAddress); err != nil {
		log.Fatalf("Configuration validation failed: %v", err)
	}

	log.Printf("Starting fake Antminer server: %s (SN: %s)", minerType, serialNumber)

	// Parse error configuration from environment
	errorConfig := parseErrorConfig()

	// Create miner state
	state := &MinerState{
		MinerType:       minerType,
		SerialNumber:    serialNumber,
		MacAddress:      macAddress,
		FirmwareVersion: firmwareVersion,
		IPAddress:       getOutboundIP().String(),
		Hostname:        "antminer-" + serialNumber,
		NetMask:         "255.255.255.0",
		Gateway:         "192.168.2.1",
		DNSServers:      "8.8.8.8",
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
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create wait group for server goroutines
	var wg sync.WaitGroup

	// Start RPC server (handles cgminer API)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := startRPCServer(ctx, state); err != nil && err != context.Canceled {
			log.Printf("RPC server error: %v", err)
		}
	}()

	// Start HTTP server (handles web API)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := startHTTPServer(ctx, state); err != nil && err != context.Canceled {
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

func startRPCServer(ctx context.Context, state *MinerState) error {
	// Listen on the standard cgminer API port
	listener, err := net.Listen("tcp", ":4028")
	if err != nil {
		return fmt.Errorf("failed to start RPC server: %w", err)
	}
	defer listener.Close()

	// Handle graceful shutdown
	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	log.Println("RPC server listening on :4028")

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

		go handleRPCConnection(conn, state)
	}
}

func startHTTPServer(ctx context.Context, state *MinerState) error {
	mux := http.NewServeMux()

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

	server := &http.Server{
		Addr:    ":80",
		Handler: protectedHandler,
	}

	// Handle graceful shutdown
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	log.Println("HTTP server listening on :80")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}
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

// getOutboundIP gets the preferred outbound IP of this machine
func getOutboundIP() net.IP {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP
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
