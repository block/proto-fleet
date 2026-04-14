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
		hashRate := state.effectiveHashRateLocked()
		minerStatus := state.summaryStatusLocked()

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
					"rate_5s":    hashRate,
					"rate_30m":   hashRate,
					"rate_avg":   hashRate,
					"rate_ideal": hashRate,
					"rate_unit":  "TH/s",
					"hw_all":     0,
					"bestshare":  12345678,
					"status": []map[string]interface{}{
						{
							"type":   "miner",
							"status": minerStatus,
							"code":   0,
							"msg":    minerStatus,
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
		hashRate := state.effectiveHashRateLocked()

		const chainCount = 3
		chains := make([]map[string]interface{}, chainCount)

		// Add realistic variation to chain metrics
		// Real miners show slight differences in temperature and hashrate per chain
		chainTempVariations := []float64{0.0, 2.0, 4.0}       // Chains get progressively warmer
		chainHashrateVariations := []float64{1.0, 1.02, 0.98} // ±2% hashrate variation

		for i := 0; i < chainCount; i++ {
			baseTemp := state.Temperature + chainTempVariations[i]
			hashRatePerChain := (hashRate * 1000 / float64(chainCount)) * chainHashrateVariations[i]
			if state.ErrorConfig.BoardNotHashing && i == 0 {
				hashRatePerChain = 0
			}

			chains[i] = map[string]interface{}{
				"index":      i,
				"freq_avg":   490,
				"rate_ideal": hashRate * 1000 / float64(chainCount),
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
		if state.ErrorConfig.FanFailed {
			fanSpeeds[0] = 0
		}

		psuStatus := "ok"
		if state.ErrorConfig.PSUFault {
			psuStatus = "fault"
		}

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
					"rate_5s":    hashRate * 1000,
					"rate_30m":   hashRate * 1000,
					"rate_avg":   hashRate * 1000,
					"rate_ideal": hashRate * 1000,
					"rate_unit":  "GH/s",
					"chain_num":  chainCount,
					"fan_num":    4,
					"fan":        fanSpeeds,
					"hwp_total":  0.0006,
					"psu": map[string]interface{}{
						"index":  0,
						"status": psuStatus,
					},
					"chain": chains,
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
			"bitmain-hashrate-percent": "100",
			"bitmain-freq-level":       "100",
		}
		if state.MinerMode != "" {
			config["miner-mode"] = state.currentWorkModeLocked()
		} else {
			config["bitmain-work-mode"] = state.currentWorkModeLocked()
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
			Pools           []Pool `json:"pools"`
			MinerMode       string `json:"miner-mode"`
			BitmainWorkMode string `json:"bitmain-work-mode"`
		}

		if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
			errorResponse(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		state.mu.Lock()
		if len(config.Pools) > 0 {
			state.Pools = config.Pools
		}
		state.setWorkModeLocked(config.MinerMode, config.BitmainWorkMode)
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

func createPasswordChangeHandler(state *MinerState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Limit request body size
		r.Body = http.MaxBytesReader(w, r.Body, 1024) // 1KB limit

		var req struct {
			CurPwd     string `json:"curPwd"`
			NewPwd     string `json:"newPwd"`
			ConfirmPwd string `json:"confirmPwd"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"stats":"error","code":"P001","msg":"Invalid request"}`)
			return
		}

		// Validate current password
		state.mu.RLock()
		currentPassword := state.Password
		state.mu.RUnlock()

		if req.CurPwd != currentPassword {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, `{"stats":"error","code":"P002","msg":"Current password incorrect"}`)
			return
		}

		// Validate new password matches confirmation
		if req.NewPwd != req.ConfirmPwd {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"stats":"error","code":"P003","msg":"New password and confirmation do not match"}`)
			return
		}

		// Update password
		state.mu.Lock()
		state.Password = req.NewPwd
		state.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"stats":"success","code":"P000","msg":"OK!"}`)
	}
}

func createKernelLogHandler(state *MinerState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		state.mu.RLock()
		minerType := state.MinerType
		serialNumber := state.SerialNumber
		state.mu.RUnlock()

		// Use a single timestamp for consistency within the response
		currentTimestamp := time.Now().Unix() % 10000

		// Generate realistic kernel log output
		kernelLog := fmt.Sprintf(`[    0.000000] Booting Linux on physical CPU 0x0
[    0.000000] Linux version 4.9.113 (root@builder) (gcc version 6.4.0 (Buildroot 2018.02.1)) #1 SMP PREEMPT Thu Jul 11 17:01:13 CST 2024
[    0.000000] CPU: ARMv7 Processor [410fc075] revision 5 (ARMv7), cr=10c5387d
[    0.000000] Machine model: Bitmain %s
[    0.000000] Memory policy: Data cache writealloc
[    0.000000] cma: Reserved 64 MiB at 0x1c000000
[    0.000000] PERCPU: Embedded 14 pages/cpu @dbb36000 s26752 r8192 d22400 u57344
[    0.000000] Built 1 zonelists in Zone order, mobility grouping on.  Total pages: 129920
[    0.000000] Kernel command line: console=ttyS0,115200 rootfstype=squashfs root=/dev/ram0 coherent_pool=1M
[    0.000000] PID hash table entries: 2048 (order: 1, 8192 bytes)
[    0.000000] Dentry cache hash table entries: 65536 (order: 6, 262144 bytes)
[    0.000000] Inode-cache hash table entries: 32768 (order: 5, 131072 bytes)
[    0.000000] Memory: 420928K/524288K available (7168K kernel code, 393K rwdata, 2060K rodata, 1024K init, 257K bss, 37824K reserved, 65536K cma-reserved, 0K highmem)
[    0.040000] Console: colour dummy device 80x30
[    0.044000] Calibrating delay loop... 1996.80 BogoMIPS (lpj=9984000)
[    0.120028] pid_max: default: 32768 minimum: 301
[    0.124892] Mount-cache hash table entries: 1024 (order: 0, 4096 bytes)
[    0.131550] Mountpoint-cache hash table entries: 1024 (order: 0, 4096 bytes)
[    0.139401] CPU: Testing write buffer coherency: ok
[    0.144918] Setting up static identity map for 0x100000 - 0x100058
[    1.200000] cgminer[1234]: Starting cgminer 4.11.1 for %s
[    1.210000] cgminer[1234]: Serial Number: %s
[    1.220000] cgminer[1234]: Initializing mining hardware...
[    1.500000] cgminer[1234]: Detected 3 hashboards
[    1.600000] cgminer[1234]: Chain 0: 108 ASICs detected, SN=SMTTYRHBDJAAI019D
[    1.700000] cgminer[1234]: Chain 1: 108 ASICs detected, SN=SMTTYRHBDJAAI019N
[    1.800000] cgminer[1234]: Chain 2: 108 ASICs detected, SN=SMTTYRHBDJAAI019S
[    2.000000] cgminer[1234]: All hashboards initialized successfully
[    2.100000] cgminer[1234]: Connecting to pool stratum+tcp://btc.example.com:3333
[    2.500000] cgminer[1234]: Pool connection established
[    2.600000] cgminer[1234]: Mining started, target hashrate: 110 TH/s
[   %d.000000] cgminer[1234]: Current hashrate: 110.5 TH/s, Temperature: 45C
[   %d.100000] cgminer[1234]: Fan speeds: 7000 7050 6980 7020 RPM
`, minerType, minerType, serialNumber, currentTimestamp, currentTimestamp)

		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(kernelLog)); err != nil {
			log.Printf("Failed to write kernel log response: %v", err)
		}
	}
}

func createUpgradeHandler(_ *MinerState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "Missing or invalid 'file' field", http.StatusBadRequest)
			return
		}
		defer file.Close()

		log.Printf("Firmware upgrade received: filename=%s, size=%d", header.Filename, header.Size)

		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "<html><body>System Upgrade Successed</body></html>")
	}
}
