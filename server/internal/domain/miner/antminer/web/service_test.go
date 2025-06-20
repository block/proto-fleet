package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/web"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
)

func TestGetSystemInfo(t *testing.T) {
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
	connInfo, err := web.NewAntminerConnectionInfoFromURL(server.URL, "root", *secrets.NewText("root"))
	assert.NoError(t, err)

	systemInfo, err := service.GetSystemInfo(t.Context(), connInfo)

	assert.NoError(t, err)
	assert.NotZero(t, systemInfo)
	assert.Equal(t, "Antminer S21", systemInfo.MinerType)
	assert.Equal(t, "DHCP", systemInfo.NetType)
	assert.Equal(t, "SMTTATUBDJAAI00A5", systemInfo.SerialNumber)
}

func TestGetMinerSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/cgi-bin/summary.cgi", r.URL.Path)

		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{
			"STATUS": {"STATUS": "S", "when": 1750192565, "Msg": "summary", "api_version": "1.0.0"}, 
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
	connInfo, err := web.NewAntminerConnectionInfoFromURL(server.URL, "root", *secrets.NewText("root"))
	assert.NoError(t, err)

	summary, err := service.GetMinerSummary(t.Context(), connInfo)

	assert.NoError(t, err)
	assert.NotZero(t, summary)
	assert.Equal(t, "S", summary.Status.Status)
	assert.Equal(t, "summary", summary.Status.Msg)
	assert.Equal(t, "Antminer S21", summary.Info.Type)
	assert.Equal(t, float64(206238.69), summary.Summary[0].Rate5s)
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
	connInfo, err := web.NewAntminerConnectionInfoFromURL(server.URL, "root", *secrets.NewText("root"))
	assert.NoError(t, err)

	config, err := service.GetMinerConfig(t.Context(), connInfo)

	assert.NoError(t, err)
	assert.NotZero(t, config)
	assert.Equal(t, "stratum+tcp://stratum.example.com:3333", config.Pools[0].URL)
	assert.Equal(t, "proto_mining_sw_test", config.Pools[0].User)
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
	connInfo, err := web.NewAntminerConnectionInfoFromURL(server.URL, "root", *secrets.NewText("root"))
	assert.NoError(t, err)

	networkInfo, err := service.GetNetworkInfo(t.Context(), connInfo)

	assert.NoError(t, err)
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
		assert.Equal(t, "username.worker", config.Pools[0].User)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	service := web.NewService()
	connInfo, err := web.NewAntminerConnectionInfoFromURL(server.URL, "root", *secrets.NewText("root"))
	assert.NoError(t, err)

	config := &web.MinerConfig{
		Pools: []struct {
			URL  string `json:"url"`
			User string `json:"user"`
			Pass string `json:"pass"`
		}{
			{
				URL:  "stratum+tcp://pool.example.com:3333",
				User: "username.worker",
				Pass: "x",
			},
		},
		BitmainFanPWM:    "100",
		BitmainFreqLevel: "100",
	}

	err = service.SetMinerConfig(t.Context(), connInfo, config)

	assert.NoError(t, err)
}

func TestReboot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	service := web.NewService()
	connInfo, err := web.NewAntminerConnectionInfoFromURL(server.URL, "root", *secrets.NewText("root"))
	assert.NoError(t, err)

	err = service.Reboot(t.Context(), connInfo)

	assert.NoError(t, err)
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
			connInfo, err := web.NewAntminerConnectionInfoFromURL(server.URL, "root", *secrets.NewText("root"))
			assert.NoError(t, err)

			err = tc.testFunc(service, t.Context(), connInfo)

			assert.NoError(t, err)
		})
	}
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
			connInfo, err := web.NewAntminerConnectionInfoFromURL(server.URL, "", *secrets.NewText(""))
			assert.NoError(t, err)

			_, err = service.GetSystemInfo(t.Context(), connInfo)

			assert.Error(t, err)
		})
	}
}
