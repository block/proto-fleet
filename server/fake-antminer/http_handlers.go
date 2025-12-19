package main

import (
	"crypto/md5" // #nosec G501 - Required for digest authentication with Antminer devices
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// HTTP handler functions

func createSystemInfoHandler(state *MinerState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Ensure this is a GET request
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		state.mu.RLock()
		defer state.mu.RUnlock()

		systemInfo := map[string]string{
			"minertype":                 state.MinerType,
			"nettype":                   "static",
			"netdevice":                 "eth0",
			"macaddr":                   state.MacAddress,
			"hostname":                  state.Hostname,
			"ipaddress":                 state.IPAddress,
			"netmask":                   state.NetMask,
			"gateway":                   state.Gateway,
			"dnsservers":                state.DNSServers,
			"system_mode":               "normal",
			"system_kernel_version":     "4.9.0",
			"system_filesystem_version": "1.0.0",
			"firmware_type":             "Antminer",
			"serinum":                   state.SerialNumber,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(systemInfo); err != nil {
			log.Printf("Failed to encode system info response: %v", err)
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}
	}
}

func createMinerSummaryHandler(state *MinerState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Ensure this is a GET request
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		state.mu.RLock()
		defer state.mu.RUnlock()

		now := time.Now().Unix()

		summary := map[string]interface{}{
			"STATUS": []map[string]interface{}{
				{
					"STATUS":      "S",
					"when":        now,
					"Msg":         "Summary",
					"api_version": "3.1",
				},
			},
			"INFO": map[string]string{
				"miner_version": "3.1",
				"CompileTime":   "2023-05-01",
				"type":          state.MinerType,
			},
			"SUMMARY": []map[string]interface{}{
				{
					"elapsed":    3600,
					"rate_5s":    state.HashRate,
					"rate_30m":   state.HashRate,
					"rate_avg":   state.HashRate,
					"rate_ideal": state.HashRate,
					"rate_unit":  "TH/s",
					"hw_all":     0,
					"bestshare":  12345678,
					"status": []map[string]interface{}{
						{
							"type":   "miner",
							"status": "running",
							"code":   0,
							"msg":    "running",
						},
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(summary); err != nil {
			log.Printf("Failed to encode miner summary response: %v", err)
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}
	}
}

func createStatsHandler(state *MinerState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		state.mu.RLock()
		defer state.mu.RUnlock()

		now := time.Now().Unix()

		const chainCount = 3
		chains := make([]map[string]interface{}, chainCount)

		// Add realistic variation to chain metrics
		// Real miners show slight differences in temperature and hashrate per chain
		chainTempVariations := []float64{0.0, 2.0, 4.0}       // Chains get progressively warmer
		chainHashrateVariations := []float64{1.0, 1.02, 0.98} // ±2% hashrate variation

		for i := 0; i < chainCount; i++ {
			baseTemp := state.Temperature + chainTempVariations[i]
			hashRatePerChain := (state.HashRate * 1000 / float64(chainCount)) * chainHashrateVariations[i]

			chains[i] = map[string]interface{}{
				"index":      i,
				"freq_avg":   490,
				"rate_ideal": state.HashRate * 1000 / float64(chainCount),
				"rate_real":  hashRatePerChain,
				"asic_num":   108,
				"temp_pic":   []float64{baseTemp - 15, baseTemp - 15, baseTemp, baseTemp},
				"temp_pcb":   []float64{baseTemp - 5, baseTemp - 5, baseTemp + 10, baseTemp + 10},
				"temp_chip":  []float64{baseTemp, baseTemp, baseTemp + 14, baseTemp + 14},
				"hw":         0,
				"sn":         fmt.Sprintf("SMTTYRHBDJAAI019%c", 'D'+i),
				"hwp":        0.0,
			}
		}

		// Add realistic variation to fan speeds
		// Real fans show slight RPM differences due to manufacturing tolerances and airflow
		fanSpeeds := []int{7000, 7050, 6980, 7020}

		stats := map[string]interface{}{
			"STATUS": map[string]interface{}{
				"STATUS":      "S",
				"when":        now,
				"Msg":         "stats",
				"api_version": "1.0.0",
			},
			"INFO": map[string]string{
				"miner_version": "uart_trans.1.3",
				"CompileTime":   "Thu Jul 11 16:38:25 CST 2024",
				"type":          state.MinerType,
			},
			"STATS": []map[string]interface{}{
				{
					"elapsed":    3600,
					"rate_5s":    state.HashRate * 1000,
					"rate_30m":   state.HashRate * 1000,
					"rate_avg":   state.HashRate * 1000,
					"rate_ideal": state.HashRate * 1000,
					"rate_unit":  "GH/s",
					"chain_num":  chainCount,
					"fan_num":    4,
					"fan":        fanSpeeds,
					"hwp_total":  0.0006,
					"chain":      chains,
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(stats); err != nil {
			log.Printf("Failed to encode stats response: %v", err)
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}
	}
}

func createMinerConfigHandler(state *MinerState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Ensure this is a GET request
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		state.mu.RLock()
		defer state.mu.RUnlock()

		pools := make([]map[string]string, len(state.Pools))
		for i, pool := range state.Pools {
			pools[i] = map[string]string{
				"url":  pool.URL,
				"user": pool.User,
				"pass": pool.Pass,
			}
		}

		config := map[string]interface{}{
			"pools":                    pools,
			"api-listen":               true,
			"api-network":              true,
			"api-groups":               "A:stats:pools:devs:summary:version",
			"api-allow":                "A:0/0,W:*",
			"bitmain-fan-ctrl":         true,
			"bitmain-fan-pwm":          "100",
			"bitmain-use-vil":          true,
			"bitmain-freq":             "550",
			"bitmain-voltage":          "1800",
			"bitmain-ccdelay":          "0",
			"bitmain-pwth":             "0",
			"bitmain-work-mode":        "0",
			"bitmain-hashrate-percent": "100",
			"bitmain-freq-level":       "100",
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(config); err != nil {
			log.Printf("Failed to encode miner config response: %v", err)
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}
	}
}

func createNetworkInfoHandler(state *MinerState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Ensure this is a GET request
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		state.mu.RLock()
		defer state.mu.RUnlock()

		networkInfo := map[string]string{
			"nettype":         "static",
			"netdevice":       "eth0",
			"macaddr":         state.MacAddress,
			"ipaddress":       state.IPAddress,
			"netmask":         state.NetMask,
			"conf_nettype":    "static",
			"conf_hostname":   state.Hostname,
			"conf_ipaddress":  state.IPAddress,
			"conf_netmask":    state.NetMask,
			"conf_gateway":    state.Gateway,
			"conf_dnsservers": state.DNSServers,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(networkInfo); err != nil {
			log.Printf("Failed to encode network info response: %v", err)
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}
	}
}

func createSetConfigHandler(state *MinerState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Ensure this is a POST request
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Limit request body size to prevent DoS attacks
		r.Body = http.MaxBytesReader(w, r.Body, 1024*1024) // 1MB limit

		var config struct {
			Pools []Pool `json:"pools"`
		}

		if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
			errorResponse(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		state.mu.Lock()
		if len(config.Pools) > 0 {
			state.Pools = config.Pools
		}
		state.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"success": true}`)
	}
}

func createRebootHandler(state *MinerState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Ensure this is a POST request
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		log.Println("Received reboot request")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"success": true, "message": "Reboot initiated"}`)
	}
}

func createBlinkHandler(state *MinerState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Ensure this is a POST request
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Limit request body size to prevent DoS attacks
		r.Body = http.MaxBytesReader(w, r.Body, 1024) // 1KB limit for blink requests

		var blinkRequest struct {
			Blink string `json:"blink"`
		}

		if err := json.NewDecoder(r.Body).Decode(&blinkRequest); err != nil {
			errorResponse(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		state.mu.Lock()
		state.IsBlinking = (blinkRequest.Blink == "true")
		state.mu.Unlock()

		log.Printf("Received blink request: %s", blinkRequest.Blink)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"success": true}`)
	}
}

// createHealthHandler provides a health check endpoint
func createHealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Ensure this is a GET request
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status": "healthy", "timestamp": %d}`, time.Now().Unix())
	}
}

// Authentication middleware

// DigestChallenge holds the challenge info
type DigestChallenge struct {
	Realm     string
	Nonce     string
	Opaque    string
	Algorithm string
	QOP       string
}

// Middleware for digest authentication
func digestAuthMiddleware(state *MinerState) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			state.mu.RLock()
			username := state.Username
			password := state.Password
			state.mu.RUnlock()

			auth := r.Header.Get("Authorization")
			if auth == "" {
				// No auth header, send challenge
				nonce := generateNonce()
				opaque := generateOpaque()

				w.Header().Set("WWW-Authenticate", fmt.Sprintf(
					`Digest realm="%s", nonce="%s", opaque="%s", algorithm=MD5, qop="auth"`,
					"Antminer", nonce, opaque,
				))
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			// Parse the auth header and validate
			if isValidDigestAuth(auth, username, password, r.Method) {
				next.ServeHTTP(w, r)
			} else {
				w.WriteHeader(http.StatusUnauthorized)
				fmt.Fprintf(w, `{"error": "Invalid credentials"}`)
			}
		})
	}
}

// isValidDigestAuth validates digest authentication
func isValidDigestAuth(authHeader, username, password, method string) bool {
	// Parse digest auth header
	if !strings.HasPrefix(authHeader, "Digest ") {
		return false
	}

	// Extract username from digest header using regex
	usernameRegex := regexp.MustCompile(`username="([^"]+)"`)
	matches := usernameRegex.FindStringSubmatch(authHeader)
	if len(matches) < 2 {
		return false
	}

	extractedUsername := matches[1]

	// For a fake implementation, we validate the username matches
	// In a real implementation, you would also validate the response hash
	return extractedUsername == username
}

// generateNonce creates a random nonce for digest auth
func generateNonce() string {
	return uuid.New().String()
}

// generateOpaque creates a random opaque string for digest auth
func generateOpaque() string {
	hash := sha256.Sum256([]byte(uuid.New().String()))
	return hex.EncodeToString(hash[:])
}

// md5Hash creates an MD5 hash of the input string
// #nosec G401 - MD5 is required for digest authentication with Antminer devices
func md5Hash(text string) string {
	hash := md5.Sum([]byte(text))
	return hex.EncodeToString(hash[:])
}
