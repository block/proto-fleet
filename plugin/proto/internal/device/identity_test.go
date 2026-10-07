package device

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/block/proto-fleet/plugin/proto/pkg/proto"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

// identityTestRig models two physical rigs reusing one address and password.
// Each has its own bearer token, so a swap can also exercise login/retry paths.
type identityTestRig struct {
	mu                       sync.Mutex
	serial, mac              string
	token, password          string
	defaultPassword          bool
	identityStatus           int
	identityCredentialsCount int
	loginCount               int
	commandCount             int
	swapOnNextLogin          bool
	swapAfterRequest         string
}

func (r *identityTestRig) replace() {
	r.serial, r.mac, r.token = "replacement-serial", "AA:BB:CC:DD:EE:02", "replacement-token"
}

func (r *identityTestRig) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if req.URL.Path == "/api/v1/pairing/info" {
		if req.Header.Get("Authorization") != "" {
			r.identityCredentialsCount++
		}
		if r.identityStatus != 0 {
			w.WriteHeader(r.identityStatus)
			return
		}
		_, _ = fmt.Fprintf(w, `{"cb_sn":%q,"mac":%q}`, r.serial, r.mac)
		return
	}
	if req.URL.Path == "/api/v1/auth/login" {
		var supplied struct {
			Password string `json:"password"`
		}
		if json.NewDecoder(req.Body).Decode(&supplied) != nil || supplied.Password != r.password {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		r.loginCount++
		if r.swapOnNextLogin {
			r.replace()
			r.swapOnNextLogin = false
		}
		_, _ = fmt.Fprintf(w, `{"access_token":%q}`, r.token)
		return
	}
	if req.Header.Get("Authorization") != "Bearer "+r.token {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.defaultPassword && req.URL.Path != "/api/v1/auth/change-password" {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"DEFAULT_PASSWORD_ACTIVE","message":"default password must be changed"}}`))
		return
	}
	switch req.URL.Path {
	case "/api/v1/mining":
		_, _ = w.Write([]byte(`{"mining-status":{"status":"Mining"}}`))
	case "/api/v1/pools":
		_, _ = w.Write([]byte(`{"pools":[{"id":0,"url":"stratum+tcp://pool.example:3333","user":"worker"}]}`))
	case "/api/v1/telemetry":
		_, _ = w.Write([]byte(`{}`))
	case "/api/v1/system/status":
		_, _ = w.Write([]byte(`{"default_password_active":false}`))
	case "/api/v1/system":
		_, _ = w.Write([]byte(`{"system-info":{"os":{"name":"ProtoOS","version":"1.0.0"}}}`))
	case "/api/v1/auth/change-password":
		var supplied struct {
			NewPassword string `json:"new_password"`
		}
		if json.NewDecoder(req.Body).Decode(&supplied) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		r.password = supplied.NewPassword
		r.defaultPassword = false
		r.commandCount++
	case "/api/v1/system/reboot", "/api/v1/system/update":
		_, _ = io.Copy(io.Discard, req.Body)
		r.commandCount++
	default:
		w.WriteHeader(http.StatusNotFound)
	}
	if r.swapAfterRequest == req.URL.Path {
		r.replace()
		r.swapAfterRequest = ""
	}
}

func newIdentityTestRig(t *testing.T) (*identityTestRig, sdk.DeviceInfo) {
	t.Helper()
	rig := &identityTestRig{serial: "original-serial", mac: "AA:BB:CC:DD:EE:01", token: "original-token", password: "shared-fleet-password"}
	server := httptest.NewServer(rig)
	t.Cleanup(server.Close)
	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)
	host, portString, err := net.SplitHostPort(parsed.Host)
	require.NoError(t, err)
	port, err := strconv.ParseInt(portString, 10, 32)
	require.NoError(t, err)
	return rig, sdk.DeviceInfo{Host: host, Port: int32(port), URLScheme: "http", SerialNumber: rig.serial, MacAddress: rig.mac}
}

func requireIdentityUnavailable(t *testing.T, err error) {
	t.Helper()
	require.ErrorIs(t, err, proto.ErrDeviceIdentity)
	require.Equal(t, codes.Unavailable, grpcstatus.Code(err))
	require.False(t, isAuthenticationError(err), "identity failures must preserve paired credentials for address recovery")
	require.False(t, isDefaultPasswordError(err))
}

func TestDeviceRejectsDifferentMinerAtOldIP(t *testing.T) {
	for _, knownIdentity := range []bool{true, false} {
		for _, tokenStillWorks := range []bool{true, false} {
			t.Run(fmt.Sprintf("known_identity_%t/token_still_works_%t", knownIdentity, tokenStillWorks), func(t *testing.T) {
				rig, expected := newIdentityTestRig(t)
				if !knownIdentity {
					expected.SerialNumber, expected.MacAddress = "", ""
				}
				credentials := sdk.UsernamePassword{Username: "admin", Password: rig.password}
				dev, err := New(t.Context(), "original-fleet-id", expected, credentials, SetStatusTTL(0))
				require.NoError(t, err)
				t.Cleanup(func() { _ = dev.Close(t.Context()) })
				rig.mu.Lock()
				rig.replace()
				if tokenStillWorks {
					rig.token = "original-token"
				}
				rig.mu.Unlock()

				metrics, err := dev.Status(t.Context())
				requireIdentityUnavailable(t, err)
				require.Empty(t, metrics.DeviceID)
				_, _, err = dev.DescribeDevice(t.Context())
				requireIdentityUnavailable(t, err)
				requireIdentityUnavailable(t, dev.Reboot(t.Context()))
				requireIdentityUnavailable(t, dev.UpdateMinerPassword(t.Context(), credentials.Password, "new-password"))
				requireIdentityUnavailable(t, dev.FirmwareUpdate(t.Context(), sdk.FirmwareFile{Filename: "firmware.bin", Size: 1, Reader: strings.NewReader("x")}))

				// A fresh factory handle with the persisted original identity must also
				// reject the endpoint, rather than trust successful same-password login.
				expected.SerialNumber, expected.MacAddress = "original-serial", "AA:BB:CC:DD:EE:01"
				fresh, err := New(t.Context(), "original-fleet-id", expected, credentials)
				requireIdentityUnavailable(t, err)
				require.Nil(t, fresh)
				rig.mu.Lock()
				defer rig.mu.Unlock()
				require.Equal(t, 1, rig.loginCount, "replacement must not receive stored credentials")
				require.Zero(t, rig.commandCount, "replacement must not receive mutations")
				require.Zero(t, rig.identityCredentialsCount, "public identity probes must not send cached bearer credentials")
			})
		}
	}
}

func TestDeviceRechecksIdentityAfterLoginBeforeCommandReplay(t *testing.T) {
	for _, command := range []string{"status", "reboot", "password", "firmware"} {
		t.Run(command, func(t *testing.T) {
			rig, expected := newIdentityTestRig(t)
			dev, err := New(t.Context(), "original-fleet-id", expected, sdk.UsernamePassword{Username: "admin", Password: rig.password}, SetStatusTTL(0))
			require.NoError(t, err)
			t.Cleanup(func() { _ = dev.Close(t.Context()) })
			rig.mu.Lock()
			rig.token = "expired-token"
			rig.swapOnNextLogin = true
			rig.mu.Unlock()
			switch command {
			case "status":
				var metrics sdk.DeviceMetrics
				metrics, err = dev.Status(t.Context())
				require.Empty(t, metrics.DeviceID)
			case "reboot":
				err = dev.Reboot(t.Context())
			case "password":
				err = dev.UpdateMinerPassword(t.Context(), "shared-fleet-password", "new-password")
			case "firmware":
				err = dev.FirmwareUpdate(t.Context(), sdk.FirmwareFile{Filename: "firmware.bin", Size: 1, Reader: strings.NewReader("x")})
			}
			requireIdentityUnavailable(t, err)
			rig.mu.Lock()
			defer rig.mu.Unlock()
			require.Equal(t, 2, rig.loginCount, "the endpoint changes while reauthenticating")
			require.Zero(t, rig.commandCount)
		})
	}
}

func TestDeviceRefreshesTokenForSameIdentity(t *testing.T) {
	rig, expected := newIdentityTestRig(t)
	dev, err := New(t.Context(), "original-fleet-id", expected, sdk.UsernamePassword{Username: "admin", Password: rig.password}, SetStatusTTL(0))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dev.Close(t.Context()) })
	rig.mu.Lock()
	rig.token = "new-token"
	rig.mu.Unlock()
	metrics, err := dev.Status(t.Context())
	require.NoError(t, err)
	require.Equal(t, "original-fleet-id", metrics.DeviceID)
	require.NoError(t, dev.Reboot(t.Context()))
	rig.mu.Lock()
	defer rig.mu.Unlock()
	require.Equal(t, 2, rig.loginCount)
	require.Equal(t, 1, rig.commandCount)
}

func TestDeviceIdentityAllowsDefaultPasswordRemediation(t *testing.T) {
	rig, expected := newIdentityTestRig(t)
	rig.defaultPassword = true
	dev, err := New(t.Context(), "original-fleet-id", expected, sdk.UsernamePassword{Username: "admin", Password: rig.password}, SetStatusTTL(0))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dev.Close(t.Context()) })
	_, err = dev.Status(t.Context())
	require.True(t, isDefaultPasswordError(err))
	observed, _, err := dev.DescribeDevice(t.Context())
	require.NoError(t, err)
	require.Equal(t, expected.SerialNumber, observed.SerialNumber)
	require.NoError(t, dev.UpdateMinerPassword(t.Context(), rig.password, "new-password"))
	metrics, err := dev.Status(t.Context())
	require.NoError(t, err)
	require.Equal(t, sdk.HealthHealthyActive, metrics.Health)
}

func TestDeviceRejectsIdentityChangeDuringOptionalTelemetry(t *testing.T) {
	for _, path := range []string{"/api/v1/mining", "/api/v1/pools", "/api/v1/telemetry", "/api/v1/system"} {
		t.Run(path, func(t *testing.T) {
			rig, expected := newIdentityTestRig(t)
			dev, err := New(t.Context(), "original-fleet-id", expected, sdk.UsernamePassword{Username: "admin", Password: rig.password}, SetStatusTTL(0))
			require.NoError(t, err)
			t.Cleanup(func() { _ = dev.Close(t.Context()) })
			dev.lastFirmwareCheckAt = dev.lastFirmwareCheckAt.Add(-firmwareRefreshInterval)
			dev.lastDefaultPasswordCheckAt = dev.lastDefaultPasswordCheckAt.Add(-defaultPasswordInterval)
			rig.mu.Lock()
			rig.swapAfterRequest = path
			rig.mu.Unlock()
			metrics, err := dev.Status(t.Context())
			requireIdentityUnavailable(t, err)
			require.Empty(t, metrics.DeviceID)
		})
	}
}

func TestDeviceValidatesPartialLiveIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, expectedSerial, expectedMAC, observedSerial, observedMAC string
		accepted                                                       bool
	}{
		{"serial only expected", "original-serial", "", "original-serial", "AA:BB:CC:DD:EE:01", true},
		{"MAC only expected", "", "AA:BB:CC:DD:EE:01", "original-serial", "AA:BB:CC:DD:EE:01", true},
		{"serial only observed", "original-serial", "AA:BB:CC:DD:EE:01", "original-serial", "", true},
		{"MAC only observed", "original-serial", "AA:BB:CC:DD:EE:01", "", "AA:BB:CC:DD:EE:01", true},
		{"normalized fields", " original-serial ", " aabbccddee01 ", "original-serial", "aa-bb-cc-dd-ee-01", true},
		{"normalized dotted MAC", "", "aabb.ccdd.ee01", "", "AA:BB:CC:DD:EE:01", true},
		{"conflicting serial", "original-serial", "AA:BB:CC:DD:EE:01", "different", "AA:BB:CC:DD:EE:01", false},
		{"conflicting MAC", "original-serial", "AA:BB:CC:DD:EE:01", "original-serial", "AA:BB:CC:DD:EE:02", false},
		{"no overlapping field", "original-serial", "", "", "AA:BB:CC:DD:EE:01", false},
		{"no observed identity", "original-serial", "AA:BB:CC:DD:EE:01", "", "", false},
		{"invalid observed MAC", "", "AA:BB:CC:DD:EE:01", "", "invalid", false},
		{"learn unknown identity", "", "", "original-serial", "AA:BB:CC:DD:EE:01", true},
		{"cannot learn absent identity", "", "", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig, expected := newIdentityTestRig(t)
			expected.SerialNumber, expected.MacAddress = tc.expectedSerial, tc.expectedMAC
			rig.serial, rig.mac = tc.observedSerial, tc.observedMAC
			dev, err := New(t.Context(), "original-fleet-id", expected, sdk.UsernamePassword{Username: "admin", Password: rig.password})
			if !tc.accepted {
				requireIdentityUnavailable(t, err)
				require.Nil(t, dev)
				return
			}
			require.NoError(t, err)
			t.Cleanup(func() { _ = dev.Close(t.Context()) })
			observed, _, err := dev.DescribeDevice(t.Context())
			require.NoError(t, err)
			require.Equal(t, tc.observedSerial, observed.SerialNumber, "do not echo a stored expectation as a live observation")
			require.Equal(t, tc.observedMAC, observed.MacAddress)
		})
	}
}

func TestDeviceTreatsUnreadableIdentityAsUnavailable(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			rig, expected := newIdentityTestRig(t)
			rig.identityStatus = status
			dev, err := New(t.Context(), "original-fleet-id", expected, sdk.UsernamePassword{Username: "admin", Password: rig.password})
			requireIdentityUnavailable(t, err)
			require.Nil(t, dev)
		})
	}
}
