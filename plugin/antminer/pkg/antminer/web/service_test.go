package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer/networking"
	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer/web"
	"github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestAntminerConnectionInfo creates an AntminerConnectionInfo from a URL for testing
func newTestAntminerConnectionInfo(t *testing.T, urlStr string, creds sdk.UsernamePassword) *web.AntminerConnectionInfo {
	t.Helper()
	parsedURL, err := url.Parse(urlStr)
	require.NoError(t, err)

	host := parsedURL.Hostname()
	port := parsedURL.Port()
	if port == "" {
		port = "80"
	}

	protocol, err := networking.ProtocolFromString(parsedURL.Scheme)
	require.NoError(t, err)

	connInfo, err := networking.NewConnectionInfo(host, port, protocol)
	require.NoError(t, err)

	return web.NewAntminerConnectionInfo(*connInfo, creds)
}

func TestGetSystemInfo(t *testing.T) {
	// Arrange
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/cgi-bin/get_system_info.cgi", r.URL.Path)

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="antminer", nonce="1234567890abcdef", algorithm=MD5, qop="auth"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{
			"minertype": "Antminer S21",
			"nettype": "DHCP",
			"netdevice": "eth0",
			"macaddr": "02:50:53:09:DA:D9",
			"hostname": "Antminer",
			"ipaddress": "127.0.0.1",
			"netmask": "255.255.255.0",
			"gateway": "",
			"dnsservers": "",
			"system_mode": "GNU/Linux",
			"system_kernel_version": "Linux 4.9.113 #1 SMP PREEMPT Thu Jul 11 17:01:13 CST 2024",
			"system_filesystem_version": "Thu Jul 11 16:38:25 CST 2024",
			"firmware_type": "Release",
			"serinum": "SMTTATUBDJAAI00A5"
		}`))
		if err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	service := web.NewService()
	connInfo := newTestAntminerConnectionInfo(t, server.URL, sdk.UsernamePassword{Username: "root", Password: "root"})

	// Act
	systemInfo, err := service.GetSystemInfo(t.Context(), connInfo)

	// Assert
	require.NoError(t, err)
	assert.NotZero(t, systemInfo)
	assert.Equal(t, "Antminer S21", systemInfo.MinerType)
	assert.Equal(t, "DHCP", systemInfo.NetType)
	assert.Equal(t, "SMTTATUBDJAAI00A5", systemInfo.SerialNumber)
}

func TestGetMinerSummary(t *testing.T) {
	// Arrange
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/cgi-bin/summary.cgi", r.URL.Path)

		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{
			"STATUS": [{"STATUS": "S", "When": 1750192565, "Msg": "summary", "Code": 0, "Description": ""}],
			"INFO": {"miner_version": "uart_trans.1.3", "CompileTime": "Thu Jul 11 16:38:25 CST 2024", "type": "Antminer S21"},
			"SUMMARY": [{
				"elapsed": 3817,
				"rate_5s": 206238.69,
				"rate_30m": 204185.62,
				"rate_avg": 203719.72,
				"rate_ideal": 200000.0,
				"rate_unit": "GH/s",
				"hw_all": 2,
				"bestshare": 727920402,
				"status": [
					{"type": "rate", "status": "s", "code": 0, "msg": ""},
					{"type": "network", "status": "s", "code": 0, "msg": ""},
					{"type": "fans", "status": "s", "code": 0, "msg": ""},
					{"type": "temp", "status": "s", "code": 0, "msg": ""}
				]
			}]
		}`))
		if err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	service := web.NewService()
	connInfo := newTestAntminerConnectionInfo(t, server.URL, sdk.UsernamePassword{Username: "root", Password: "root"})

	// Act
	summary, err := service.GetMinerSummary(t.Context(), connInfo)

	// Assert
	require.NoError(t, err)
	assert.NotZero(t, summary)
	require.NotEmpty(t, summary.Status)
	assert.Equal(t, "S", summary.Status[0].Status)
	assert.Equal(t, "summary", summary.Status[0].Msg)
	assert.Equal(t, "Antminer S21", summary.Info.Type)
	assert.InEpsilon(t, float64(206238.69), summary.Summary[0].Rate5s, 0.01)
	assert.Equal(t, "GH/s", summary.Summary[0].RateUnit)
}

func TestGetMinerConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/cgi-bin/get_miner_conf.cgi", r.URL.Path)

		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{
			"pools": [
				{
					"url": "stratum+tcp://stratum.example.com:3333",
					"user": "proto_mining_sw_test",
					"pass": "test-password"
				},
				{
					"url": "",
					"user": "",
					"pass": ""
				},
				{
					"url": "",
					"user": "",
					"pass": ""
				}
			],
			"api-listen": true,
			"api-network": true,
			"api-groups": "A:stats:pools:devs:summary:version",
			"api-allow": "A:0/0,W:*",
			"bitmain-fan-ctrl": false,
			"bitmain-fan-pwm": "100",
			"bitmain-use-vil": true,
			"bitmain-freq": "200",
			"bitmain-voltage": "1320",
			"bitmain-ccdelay": "0",
			"bitmain-pwth": "3",
			"bitmain-work-mode": "0",
			"bitmain-hashrate-percent": "100",
			"bitmain-freq-level": "100"
		}`))
		if err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	service := web.NewService()
	connInfo := newTestAntminerConnectionInfo(t, server.URL, sdk.UsernamePassword{Username: "root", Password: "root"})

	config, err := service.GetMinerConfig(t.Context(), connInfo)

	require.NoError(t, err)
	assert.NotZero(t, config)
	assert.Equal(t, "stratum+tcp://stratum.example.com:3333", config.Pools[0].URL)
	assert.Equal(t, "proto_mining_sw_test", config.Pools[0].Username)
	assert.Equal(t, "100", config.BitmainFanPWM)
	assert.Equal(t, "200", config.BitmainFreq)
}

func TestGetNetworkInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/cgi-bin/get_network_info.cgi", r.URL.Path)

		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{
			"nettype": "DHCP",
			"netdevice": "eth0",
			"macaddr": "02:50:53:09:DA:D9",
			"ipaddress": "127.0.0.1",
			"netmask": "255.255.255.0",
			"conf_nettype": "DHCP",
			"conf_hostname": "Antminer",
			"conf_ipaddress": "",
			"conf_netmask": "",
			"conf_gateway": "",
			"conf_dnsservers": ""
		}`))
		if err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	service := web.NewService()
	connInfo := newTestAntminerConnectionInfo(t, server.URL, sdk.UsernamePassword{Username: "root", Password: "root"})

	networkInfo, err := service.GetNetworkInfo(t.Context(), connInfo)

	require.NoError(t, err)
	assert.NotZero(t, networkInfo)
	assert.Equal(t, "DHCP", networkInfo.NetType)
	assert.Equal(t, "eth0", networkInfo.NetDevice)
	assert.Equal(t, "02:50:53:09:DA:D9", networkInfo.MacAddr)
	assert.Equal(t, "127.0.0.1", networkInfo.IPAddress)
}

func TestSetMinerConfig(t *testing.T) {
	authRequested := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/cgi-bin/set_miner_conf.cgi", r.URL.Path)

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" && !authRequested {
			authRequested = true
			w.Header().Set("WWW-Authenticate", `Digest realm="antminer", nonce="1234567890abcdef", algorithm=MD5, qop="auth"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		contentType := r.Header.Get("Content-Type")
		assert.Equal(t, "application/json", contentType)

		var config web.MinerConfig
		err := json.NewDecoder(r.Body).Decode(&config)
		assert.NoError(t, err)

		assert.Equal(t, "stratum+tcp://pool.example.com:3333", config.Pools[0].URL)
		assert.Equal(t, "username.worker", config.Pools[0].Username)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	service := web.NewService()
	connInfo := newTestAntminerConnectionInfo(t, server.URL, sdk.UsernamePassword{Username: "root", Password: "root"})

	config := &web.MinerConfig{
		Pools: []web.Pool{
			{
				URL:      "stratum+tcp://pool.example.com:3333",
				Username: "username.worker",
				Password: "x",
			},
		},
		BitmainFanPWM:    "100",
		BitmainFreqLevel: "100",
	}

	err := service.SetMinerConfig(t.Context(), connInfo, config)

	require.NoError(t, err)
}

func TestReboot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	service := web.NewService()
	connInfo := newTestAntminerConnectionInfo(t, server.URL, sdk.UsernamePassword{Username: "root", Password: "root"})

	err := service.Reboot(t.Context(), connInfo)

	require.NoError(t, err)
}

func TestBlink(t *testing.T) {
	testCases := []struct {
		name     string
		blinkOn  bool
		testFunc func(*web.Service, context.Context, *web.AntminerConnectionInfo) error
	}{
		{
			name:    "StartBlink",
			blinkOn: true,
			testFunc: func(s *web.Service, ctx context.Context, conn *web.AntminerConnectionInfo) error {
				return s.StartBlink(ctx, conn)
			},
		},
		{
			name:    "StopBlink",
			blinkOn: false,
			testFunc: func(s *web.Service, ctx context.Context, conn *web.AntminerConnectionInfo) error {
				return s.StopBlink(ctx, conn)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			authRequested := false

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/cgi-bin/blink.cgi", r.URL.Path)

				authHeader := r.Header.Get("Authorization")
				if authHeader == "" && !authRequested {
					authRequested = true
					w.Header().Set("WWW-Authenticate", `Digest realm="antminer", nonce="1234567890abcdef", algorithm=MD5, qop="auth"`)
					w.WriteHeader(http.StatusUnauthorized)
					return
				}

				contentType := r.Header.Get("Content-Type")
				assert.Equal(t, "application/json", contentType)

				var blinkData map[string]string
				err := json.NewDecoder(r.Body).Decode(&blinkData)
				assert.NoError(t, err)

				expectedValue := "true"
				if !tc.blinkOn {
					expectedValue = "false"
				}
				assert.Equal(t, expectedValue, blinkData["blink"])

				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			service := web.NewService()
			connInfo := newTestAntminerConnectionInfo(t, server.URL, sdk.UsernamePassword{Username: "root", Password: "root"})

			err := tc.testFunc(service, t.Context(), connInfo)

			require.NoError(t, err)
		})
	}
}

func TestGetStatsInfo(t *testing.T) {
	// Arrange
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/cgi-bin/stats.cgi", r.URL.Path)

		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{
			"STATUS": {"STATUS": "S", "when": 1766099123, "Msg": "stats", "api_version": "1.0.0"},
			"INFO": {"miner_version": "uart_trans.1.3", "CompileTime": "Thu Jul 11 16:38:25 CST 2024", "type": "Antminer S21"},
			"STATS": [{
				"elapsed": 8152,
				"rate_5s": 206901.8,
				"rate_30m": 204393.1,
				"rate_avg": 203856.36,
				"rate_ideal": 200000.0,
				"rate_unit": "GH/s",
				"chain_num": 3,
				"fan_num": 4,
				"fan": [7000, 7000, 7000, 7000],
				"psu": {"index": 0, "status": "ok"},
				"hwp_total": 0.0006,
				"chain": [
					{
						"index": 0,
						"freq_avg": 490,
						"rate_ideal": 67525.0,
						"rate_real": 67293.41,
						"asic_num": 108,
						"temp_pic": [44, 44, 58, 58],
						"temp_pcb": [54, 54, 68, 68],
						"temp_chip": [59, 59, 73, 73],
						"hw": 0,
						"sn": "SMTTYRHBDJAAI019D",
						"hwp": 0.0
					},
					{
						"index": 1,
						"freq_avg": 490,
						"rate_ideal": 67525.0,
						"rate_real": 68916.75,
						"asic_num": 108,
						"temp_pic": [44, 44, 57, 57],
						"temp_pcb": [54, 54, 67, 67],
						"temp_chip": [59, 59, 72, 72],
						"hw": 1,
						"sn": "SMTTYRHBDJAAI019N",
						"hwp": 0.001
					},
					{
						"index": 2,
						"freq_avg": 490,
						"rate_ideal": 67525.0,
						"rate_real": 70691.63,
						"asic_num": 108,
						"temp_pic": [44, 44, 58, 58],
						"temp_pcb": [54, 54, 68, 68],
						"temp_chip": [59, 59, 73, 73],
						"hw": 1,
						"sn": "SMTTYRHBDJAAI019S",
						"hwp": 0.001
					}
				]
			}]
		}`))
		if err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	service := web.NewService()
	connInfo := newTestAntminerConnectionInfo(t, server.URL, sdk.UsernamePassword{Username: "root", Password: "root"})

	// Act
	stats, err := service.GetStatsInfo(t.Context(), connInfo)

	// Assert
	require.NoError(t, err)
	assert.NotZero(t, stats)
	assert.Equal(t, "S", stats.STATUS.Status)
	assert.Equal(t, "Antminer S21", stats.INFO.Type)
	require.NotEmpty(t, stats.STATS)
	assert.Equal(t, 3, stats.STATS[0].ChainNum)
	assert.Equal(t, 4, stats.STATS[0].FanNum)
	assert.Len(t, stats.STATS[0].Fan, 4)
	assert.Equal(t, 7000, stats.STATS[0].Fan[0])
	require.NotNil(t, stats.STATS[0].PSU)
	assert.Equal(t, 0, stats.STATS[0].PSU.Index)
	assert.Equal(t, "ok", stats.STATS[0].PSU.Status)
	assert.Len(t, stats.STATS[0].Chain, 3)
	assert.Len(t, stats.STATS[0].Chain[0].TempChip, 4)
	assert.InEpsilon(t, 59.0, stats.STATS[0].Chain[0].TempChip[0], 0.01)
	assert.InEpsilon(t, 73.0, stats.STATS[0].Chain[0].TempChip[2], 0.01)
}

func TestGetKernelLog(t *testing.T) {
	expectedLog := `[    0.000000] Booting Linux on physical CPU 0x0
[    0.000000] Linux version 4.9.113 (root@builder) (gcc version 6.4.0)
[   12.345678] cgminer: Starting mining operations
[   12.456789] cgminer: Connected to pool stratum+tcp://pool.example.com:3333
[   45.678901] Temperature warning: Chain 0 reached 75C`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/cgi-bin/get_kernel_log.cgi", r.URL.Path)

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="antminer", nonce="1234567890abcdef", algorithm=MD5, qop="auth"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(expectedLog))
		if err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	service := web.NewService()
	connInfo := newTestAntminerConnectionInfo(t, server.URL, sdk.UsernamePassword{Username: "root", Password: "root"})

	log, err := service.GetKernelLog(t.Context(), connInfo)

	require.NoError(t, err)
	assert.Equal(t, expectedLog, log)
	assert.Contains(t, log, "cgminer")
	assert.Contains(t, log, "Temperature warning")
}

func TestErrorHandling(t *testing.T) {
	testCases := []struct {
		name       string
		statusCode int
		endpoint   string
	}{
		{
			name:       "Unauthorized",
			statusCode: http.StatusUnauthorized,
			endpoint:   "/cgi-bin/get_system_info.cgi",
		},
		{
			name:       "NotFound",
			statusCode: http.StatusNotFound,
			endpoint:   "/cgi-bin/get_system_info.cgi",
		},
		{
			name:       "ServerError",
			statusCode: http.StatusInternalServerError,
			endpoint:   "/cgi-bin/get_system_info.cgi",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.statusCode)
			}))
			defer server.Close()

			service := web.NewService()
			connInfo := newTestAntminerConnectionInfo(t, server.URL, sdk.UsernamePassword{})

			_, err := service.GetSystemInfo(t.Context(), connInfo)

			require.Error(t, err)
		})
	}
}

func TestChangePassword(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		// Arrange
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/cgi-bin/passwd.cgi", r.URL.Path)

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				w.Header().Set("WWW-Authenticate", `Digest realm="antminer", nonce="1234567890abcdef", algorithm=MD5, qop="auth"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			// Decode request body
			var req map[string]string
			err := json.NewDecoder(r.Body).Decode(&req)
			require.NoError(t, err)

			assert.Equal(t, "oldpassword", req["curPwd"])
			assert.Equal(t, "newpassword", req["newPwd"])
			assert.Equal(t, "newpassword", req["confirmPwd"])

			w.WriteHeader(http.StatusOK)
			_, err = w.Write([]byte(`{"stats":"success","code":"P000","msg":"OK!"}`))
			require.NoError(t, err)
		}))
		defer server.Close()

		service := web.NewService()
		connInfo := newTestAntminerConnectionInfo(t, server.URL, sdk.UsernamePassword{Username: "root", Password: "root"})

		// Act
		err := service.ChangePassword(t.Context(), connInfo, "oldpassword", "newpassword")

		// Assert
		require.NoError(t, err)
	})

	t.Run("wrong current password", func(t *testing.T) {
		// Arrange
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				w.Header().Set("WWW-Authenticate", `Digest realm="antminer", nonce="1234567890abcdef", algorithm=MD5, qop="auth"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			w.WriteHeader(http.StatusUnauthorized)
			_, err := w.Write([]byte(`{"stats":"error","code":"P002","msg":"Current password incorrect"}`))
			require.NoError(t, err)
		}))
		defer server.Close()

		service := web.NewService()
		connInfo := newTestAntminerConnectionInfo(t, server.URL, sdk.UsernamePassword{Username: "root", Password: "root"})

		// Act
		err := service.ChangePassword(t.Context(), connInfo, "wrongpassword", "newpassword")

		// Assert
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to change password")
	})

	t.Run("password mismatch", func(t *testing.T) {
		// Arrange
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				w.Header().Set("WWW-Authenticate", `Digest realm="antminer", nonce="1234567890abcdef", algorithm=MD5, qop="auth"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			w.WriteHeader(http.StatusBadRequest)
			_, err := w.Write([]byte(`{"stats":"error","code":"P003","msg":"New password and confirmation do not match"}`))
			require.NoError(t, err)
		}))
		defer server.Close()

		service := web.NewService()
		connInfo := newTestAntminerConnectionInfo(t, server.URL, sdk.UsernamePassword{Username: "root", Password: "root"})

		// Act
		err := service.ChangePassword(t.Context(), connInfo, "oldpassword", "newpassword")

		// Assert
		require.Error(t, err)
	})

	t.Run("api failure", func(t *testing.T) {
		// Arrange
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				w.Header().Set("WWW-Authenticate", `Digest realm="antminer", nonce="1234567890abcdef", algorithm=MD5, qop="auth"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(`{"stats":"error","code":"P999","msg":"Unknown error"}`))
			require.NoError(t, err)
		}))
		defer server.Close()

		service := web.NewService()
		connInfo := newTestAntminerConnectionInfo(t, server.URL, sdk.UsernamePassword{Username: "root", Password: "root"})

		// Act
		err := service.ChangePassword(t.Context(), connInfo, "oldpassword", "newpassword")

		// Assert
		require.Error(t, err)
		assert.Contains(t, err.Error(), "password change failed")
	})
}

func TestUploadFirmware(t *testing.T) {
	firmwareContent := []byte("fake-firmware-content-for-test")

	t.Run("successful upload with digest auth", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/cgi-bin/upgrade.cgi", r.URL.Path)

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				w.Header().Set("WWW-Authenticate", `Digest realm="antminer", nonce="1234567890abcdef", algorithm=MD5, qop="auth"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			assert.True(t, strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data"))

			file, header, err := r.FormFile("file")
			require.NoError(t, err, "should be able to read 'file' field")
			defer file.Close()

			assert.Equal(t, "firmware.tar.gz", header.Filename)
			body, err := io.ReadAll(file)
			require.NoError(t, err)
			assert.Equal(t, firmwareContent, body)

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("System Upgrade Successed"))
		}))
		defer server.Close()

		service := web.NewService()
		connInfo := newTestAntminerConnectionInfo(t, server.URL, sdk.UsernamePassword{Username: "root", Password: "root"})

		firmware := sdk.FirmwareFile{
			Reader:   bytes.NewReader(firmwareContent),
			Filename: "firmware.tar.gz",
			Size:     int64(len(firmwareContent)),
		}

		err := service.UploadFirmware(t.Context(), connInfo, firmware)
		require.NoError(t, err)
	})

	t.Run("auth failure (401)", func(t *testing.T) {
		challengeSent := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !challengeSent {
				challengeSent = true
				w.Header().Set("WWW-Authenticate", `Digest realm="antminer", nonce="1234567890abcdef", algorithm=MD5, qop="auth"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()

		service := web.NewService()
		connInfo := newTestAntminerConnectionInfo(t, server.URL, sdk.UsernamePassword{Username: "root", Password: "wrong"})

		firmware := sdk.FirmwareFile{
			Reader:   bytes.NewReader(firmwareContent),
			Filename: "firmware.tar.gz",
			Size:     int64(len(firmwareContent)),
		}

		err := service.UploadFirmware(t.Context(), connInfo, firmware)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "authentication")
	})

	t.Run("server error (500)", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				w.Header().Set("WWW-Authenticate", `Digest realm="antminer", nonce="1234567890abcdef", algorithm=MD5, qop="auth"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		service := web.NewService()
		connInfo := newTestAntminerConnectionInfo(t, server.URL, sdk.UsernamePassword{Username: "root", Password: "root"})

		firmware := sdk.FirmwareFile{
			Reader:   bytes.NewReader(firmwareContent),
			Filename: "firmware.tar.gz",
			Size:     int64(len(firmwareContent)),
		}

		err := service.UploadFirmware(t.Context(), connInfo, firmware)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "firmware upload failed with status 500")
	})

	t.Run("no credentials skips auth", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Empty(t, r.Header.Get("Authorization"), "no auth header when credentials are empty")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		service := web.NewService()
		connInfo := newTestAntminerConnectionInfo(t, server.URL, sdk.UsernamePassword{})

		firmware := sdk.FirmwareFile{
			Reader:   bytes.NewReader(firmwareContent),
			Filename: "firmware.tar.gz",
			Size:     int64(len(firmwareContent)),
		}

		err := service.UploadFirmware(t.Context(), connInfo, firmware)
		require.NoError(t, err)
	})

	t.Run("nil reader returns error", func(t *testing.T) {
		service := web.NewService()
		connInfo := newTestAntminerConnectionInfo(t, "http://localhost", sdk.UsernamePassword{Username: "root", Password: "root"})

		firmware := sdk.FirmwareFile{
			Filename: "firmware.tar.gz",
			Size:     100,
		}

		err := service.UploadFirmware(t.Context(), connInfo, firmware)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "firmware reader is required")
	})

	t.Run("context cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("WWW-Authenticate", `Digest realm="antminer", nonce="1234567890abcdef", algorithm=MD5, qop="auth"`)
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()

		service := web.NewService()
		connInfo := newTestAntminerConnectionInfo(t, server.URL, sdk.UsernamePassword{Username: "root", Password: "root"})

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		firmware := sdk.FirmwareFile{
			Reader:   bytes.NewReader(firmwareContent),
			Filename: "firmware.tar.gz",
			Size:     int64(len(firmwareContent)),
		}

		err := service.UploadFirmware(ctx, connInfo, firmware)
		require.Error(t, err)
	})
}
