package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	telemetryv1 "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/fatih/color"
	"github.com/urfave/cli/v3"
)

// pinFleetAuthEnv pins the string-valued FLEET_* connection and auth env vars
// so values from the developer's shell cannot leak into root flag resolution;
// vars then sets the per-case values.
func pinFleetAuthEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	for _, key := range []string{envFleetServer, envFleetAPIKey, envFleetUsername, envFleetPassword} {
		t.Setenv(key, "")
	}
	for key, value := range vars {
		t.Setenv(key, value)
	}
}

func findSubcommand(t *testing.T, parent *cli.Command, name string) *cli.Command {
	t.Helper()
	for _, sub := range parent.Commands {
		if sub.Name == name {
			return sub
		}
	}
	t.Fatalf("subcommand %q not found under %q", name, parent.Name)
	return nil
}

func TestWriteAPIErrorWritesBodyToProvidedWriter(t *testing.T) {
	oldNoColor := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = oldNoColor })

	var stderr bytes.Buffer
	writeAPIError(&stderr, &APIError{
		Method: "POST /example.Service/Call",
		Status: "401 Unauthorized",
		Body:   []byte(`{"error":"denied"}`),
	})

	output := stderr.String()
	if !strings.Contains(output, "POST /example.Service/Call returned 401 Unauthorized:") {
		t.Fatalf("output = %q, want status line", output)
	}
	if !strings.Contains(output, `"error": "denied"`) {
		t.Fatalf("output = %q, want formatted API error body", output)
	}
}

// probeAuthInputs runs the full root command with argv and captures what
// resolvedAuthInputs returns inside the leaf command's action, exercising the
// real flag parsing including subcommand-local flags and env sources.
func probeAuthInputs(t *testing.T, path []string, argv ...string) (string, string, string) {
	t.Helper()

	root := newRootCommand()
	leaf := root
	for _, name := range path {
		leaf = findSubcommand(t, leaf, name)
	}

	var apiKey, username, password string
	captured := false
	leaf.Action = func(_ context.Context, cmd *cli.Command) error {
		apiKey, username, password = resolvedAuthInputs(cmd)
		captured = true
		return nil
	}
	if err := root.Run(context.Background(), append([]string{"fleetcli"}, argv...)); err != nil {
		t.Fatalf("run fleetcli %s: %v", strings.Join(argv, " "), err)
	}
	if !captured {
		t.Fatalf("probe action never ran for: fleetcli %s", strings.Join(argv, " "))
	}
	return apiKey, username, password
}

func TestNormalizeEnum(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "set-power-target", want: "set_power_target"},
		{in: "SET-POWER-TARGET", want: "set_power_target"},
		{in: "set_power_target", want: "set_power_target"},
		{in: " one-time ", want: "one_time"},
		{in: "reboot", want: "reboot"},
	}
	for _, tt := range tests {
		if got := normalizeEnum(tt.in); got != tt.want {
			t.Errorf("normalizeEnum(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func assertMeasurementTypes(t *testing.T, got, want []telemetryv1.MeasurementType) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("measurement types = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("measurement types = %v, want %v", got, want)
		}
	}
}

func buildCombinedMetricsRequestFromArgs(t *testing.T, args ...string) (*telemetryv1.GetCombinedMetricsRequest, error) {
	t.Helper()

	var req *telemetryv1.GetCombinedMetricsRequest
	var buildErr error
	cmd := performanceCommand().Commands[0]
	cmd.Action = func(_ context.Context, cmd *cli.Command) error {
		req, buildErr = buildCombinedMetricsRequest(cmd)
		return nil
	}
	if err := cmd.Run(context.Background(), append([]string{"get"}, args...)); err != nil {
		t.Fatalf("run performance get flag harness: %v", err)
	}
	return req, buildErr
}

func TestParseMeasurementTypes(t *testing.T) {
	t.Run("valid and normalized metrics", func(t *testing.T) {
		got, err := parseMeasurementTypes([]string{"hashrate", "FAN-SPEED", " error-rate "})
		if err != nil {
			t.Fatalf("parseMeasurementTypes() error = %v", err)
		}
		assertMeasurementTypes(t, got, []telemetryv1.MeasurementType{
			telemetryv1.MeasurementType_MEASUREMENT_TYPE_HASHRATE,
			telemetryv1.MeasurementType_MEASUREMENT_TYPE_FAN_SPEED,
			telemetryv1.MeasurementType_MEASUREMENT_TYPE_ERROR_RATE,
		})
	})

	t.Run("single unknown metric rejected", func(t *testing.T) {
		_, err := parseMeasurementTypes([]string{"hashrat"})
		if err == nil || !strings.Contains(err.Error(), "invalid value for metric: hashrat") {
			t.Fatalf("parseMeasurementTypes() error = %v, want invalid metric error", err)
		}
		if !strings.Contains(err.Error(), "fan-speed") || !strings.Contains(err.Error(), "hashrate") {
			t.Errorf("error should list supported metrics, got: %v", err)
		}
	})

	t.Run("mixed valid and unknown metric rejected", func(t *testing.T) {
		_, err := parseMeasurementTypes([]string{"hashrate", "bogus"})
		if err == nil || !strings.Contains(err.Error(), "invalid value for metric: bogus") {
			t.Fatalf("parseMeasurementTypes() error = %v, want invalid metric error", err)
		}
	})
}

func TestBuildCombinedMetricsRequestMetrics(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		req, err := buildCombinedMetricsRequestFromArgs(t)
		if err != nil {
			t.Fatalf("buildCombinedMetricsRequest() error = %v", err)
		}
		want, err := parseMeasurementTypes(defaultPerformanceMetrics)
		if err != nil {
			t.Fatalf("parse default metrics: %v", err)
		}
		assertMeasurementTypes(t, req.GetMeasurementTypes(), want)
	})

	t.Run("explicit normalized metrics", func(t *testing.T) {
		req, err := buildCombinedMetricsRequestFromArgs(t, "--metric", "FAN-SPEED", "--metric", "error-rate")
		if err != nil {
			t.Fatalf("buildCombinedMetricsRequest() error = %v", err)
		}
		assertMeasurementTypes(t, req.GetMeasurementTypes(), []telemetryv1.MeasurementType{
			telemetryv1.MeasurementType_MEASUREMENT_TYPE_FAN_SPEED,
			telemetryv1.MeasurementType_MEASUREMENT_TYPE_ERROR_RATE,
		})
	})

	t.Run("unknown metric rejected", func(t *testing.T) {
		_, err := buildCombinedMetricsRequestFromArgs(t, "--metric", "hashrate", "--metric", "bogus")
		if err == nil || !strings.Contains(err.Error(), "invalid value for metric: bogus") {
			t.Fatalf("buildCombinedMetricsRequest() error = %v, want invalid metric error", err)
		}
	})

	t.Run("page token", func(t *testing.T) {
		req, err := buildCombinedMetricsRequestFromArgs(t, "--page-token", "next-page-1")
		if err != nil {
			t.Fatalf("buildCombinedMetricsRequest() error = %v", err)
		}
		if req.GetPageToken() != "next-page-1" {
			t.Fatalf("page token = %q, want next-page-1", req.GetPageToken())
		}
	})
}

func TestPerformanceGetRejectsUnknownMetricBeforeRequest(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	requestCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected request", http.StatusTeapot)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	err := newRootCommand().Run(context.Background(), []string{
		"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
		"performance", "get", "--metric", "hashrat",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid value for metric: hashrat") {
		t.Fatalf("performance get error = %v, want invalid metric error", err)
	}
	if requestCount != 0 {
		t.Fatalf("request count = %d, want 0", requestCount)
	}
}

func TestDeviceSetDeleteVerifiesType(t *testing.T) {
	t.Run("groups delete rejects rack id before delete", func(t *testing.T) {
		pinFleetAuthEnv(t, nil)

		var getAuth string
		deleteCount := 0
		mux := http.NewServeMux()
		mux.HandleFunc("POST /device_set.v1.DeviceSetService/GetDeviceSet", func(w http.ResponseWriter, r *http.Request) {
			getAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", contentTypeJSON)
			_, _ = w.Write([]byte(`{"device_set":{"id":"42","type":"DEVICE_SET_TYPE_RACK","label":"rack-42"}}`))
		})
		mux.HandleFunc("POST /device_set.v1.DeviceSetService/DeleteDeviceSet", func(w http.ResponseWriter, r *http.Request) {
			deleteCount++
			http.Error(w, "delete should not be called", http.StatusTeapot)
		})
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)

		err := newRootCommand().Run(context.Background(), []string{
			"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
			"groups", "delete", "--device-set-id", "42",
		})
		if err == nil || !strings.Contains(err.Error(), "device set 42 is a rack, not a group") {
			t.Fatalf("groups delete error = %v, want device set type mismatch", err)
		}
		if getAuth != "Bearer test-key" {
			t.Errorf("GetDeviceSet Authorization = %q, want %q", getAuth, "Bearer test-key")
		}
		if deleteCount != 0 {
			t.Fatalf("delete count = %d, want 0", deleteCount)
		}
	})

	t.Run("racks delete proceeds for rack id", func(t *testing.T) {
		pinFleetAuthEnv(t, nil)

		var calls []string
		mux := http.NewServeMux()
		mux.HandleFunc("POST /device_set.v1.DeviceSetService/GetDeviceSet", func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, "get")
			w.Header().Set("Content-Type", contentTypeJSON)
			_, _ = w.Write([]byte(`{"device_set":{"id":"42","type":"DEVICE_SET_TYPE_RACK","label":"rack-42"}}`))
		})
		mux.HandleFunc("POST /device_set.v1.DeviceSetService/DeleteDeviceSet", func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, "delete")
			w.Header().Set("Content-Type", contentTypeJSON)
			_, _ = w.Write([]byte("{}"))
		})
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)

		err := newRootCommand().Run(context.Background(), []string{
			"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
			"racks", "delete", "--device-set-id", "42",
		})
		if err != nil {
			t.Fatalf("racks delete error = %v, want success", err)
		}
		want := []string{"get", "delete"}
		if strings.Join(calls, ",") != strings.Join(want, ",") {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	})
}

func TestDeviceSetGetVerifiesType(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		actualType string
		wantError  string
	}{
		{
			name:       "groups get rejects rack id",
			args:       []string{"groups", "get", "--device-set-id", "42"},
			actualType: "DEVICE_SET_TYPE_RACK",
			wantError:  "device set 42 is a rack, not a group",
		},
		{
			name:       "racks get rejects group id",
			args:       []string{"racks", "get", "--device-set-id", "42"},
			actualType: "DEVICE_SET_TYPE_GROUP",
			wantError:  "device set 42 is a group, not a rack",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pinFleetAuthEnv(t, nil)

			getCount := 0
			mux := http.NewServeMux()
			mux.HandleFunc("POST /device_set.v1.DeviceSetService/GetDeviceSet", func(w http.ResponseWriter, _ *http.Request) {
				getCount++
				w.Header().Set("Content-Type", contentTypeJSON)
				_, _ = w.Write([]byte(`{"device_set":{"id":"42","type":"` + tt.actualType + `","label":"wrong-type"}}`))
			})
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			err := newRootCommand().Run(context.Background(), append([]string{
				"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
			}, tt.args...))
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("fleetcli %s error = %v, want %q", strings.Join(tt.args, " "), err, tt.wantError)
			}
			if getCount != 1 {
				t.Fatalf("get count = %d, want 1", getCount)
			}
		})
	}

	t.Run("matching group id proceeds to command get", func(t *testing.T) {
		pinFleetAuthEnv(t, nil)

		getCount := 0
		mux := http.NewServeMux()
		mux.HandleFunc("POST /device_set.v1.DeviceSetService/GetDeviceSet", func(w http.ResponseWriter, _ *http.Request) {
			getCount++
			w.Header().Set("Content-Type", contentTypeJSON)
			_, _ = w.Write([]byte(`{"device_set":{"id":"42","type":"DEVICE_SET_TYPE_GROUP","label":"group-42"}}`))
		})
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)

		err := newRootCommand().Run(context.Background(), []string{
			"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
			"groups", "get", "--device-set-id", "42",
		})
		if err != nil {
			t.Fatalf("groups get error = %v, want success", err)
		}
		if getCount != 2 {
			t.Fatalf("get count = %d, want 2", getCount)
		}
	})
}

func TestDeviceSetMutationsVerifyType(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		actualType    string
		mutationRoute string
		wantError     string
	}{
		{
			name:          "groups add-devices rejects rack id",
			args:          []string{"groups", "add-devices", "--target-group-id", "42", "--all-devices"},
			actualType:    "DEVICE_SET_TYPE_RACK",
			mutationRoute: "POST /device_set.v1.DeviceSetService/AddDevicesToGroup",
			wantError:     "device set 42 is a rack, not a group",
		},
		{
			name:          "groups remove-devices rejects rack id",
			args:          []string{"groups", "remove-devices", "--target-group-id", "42", "--all-devices"},
			actualType:    "DEVICE_SET_TYPE_RACK",
			mutationRoute: "POST /device_set.v1.DeviceSetService/RemoveDevicesFromGroup",
			wantError:     "device set 42 is a rack, not a group",
		},
		{
			name:          "groups update rejects rack id",
			args:          []string{"groups", "update", "--device-set-id", "42", "--label", "group-label"},
			actualType:    "DEVICE_SET_TYPE_RACK",
			mutationRoute: "POST /device_set.v1.DeviceSetService/UpdateDeviceSet",
			wantError:     "device set 42 is a rack, not a group",
		},
		{
			name:          "racks add-devices rejects group id",
			args:          []string{"racks", "add-devices", "--target-rack-id", "42", "--device", "miner-1"},
			actualType:    "DEVICE_SET_TYPE_GROUP",
			mutationRoute: "POST /device_set.v1.DeviceSetService/AssignDevicesToRack",
			wantError:     "device set 42 is a group, not a rack",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pinFleetAuthEnv(t, nil)

			mutationCount := 0
			mux := http.NewServeMux()
			mux.HandleFunc("POST /device_set.v1.DeviceSetService/GetDeviceSet", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", contentTypeJSON)
				_, _ = w.Write([]byte(`{"device_set":{"id":"42","type":"` + tt.actualType + `","label":"wrong-type"}}`))
			})
			mux.HandleFunc(tt.mutationRoute, func(w http.ResponseWriter, r *http.Request) {
				mutationCount++
				t.Errorf("unexpected mutation request: %s %s", r.Method, r.URL.Path)
				http.Error(w, "mutation should not be called", http.StatusTeapot)
			})
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			err := newRootCommand().Run(context.Background(), append([]string{
				"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
			}, tt.args...))
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("fleetcli %s error = %v, want %q", strings.Join(tt.args, " "), err, tt.wantError)
			}
			if mutationCount != 0 {
				t.Fatalf("mutation count = %d, want 0", mutationCount)
			}
		})
	}
}

func TestRackSaveRequiresJSON(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	requestCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "request should not be called", http.StatusTeapot)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	err := newRootCommand().Run(context.Background(), []string{
		"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
		"racks", "save",
	})
	if err == nil || !strings.Contains(err.Error(), "json") {
		t.Fatalf("racks save error = %v, want json requirement", err)
	}
	if requestCount != 0 {
		t.Fatalf("request count = %d, want 0", requestCount)
	}
}

func TestRackSaveJSONWithoutDeviceSetIDSkipsTypeCheck(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	jsonPath := filepath.Join(t.TempDir(), "rack.json")
	if err := os.WriteFile(jsonPath, []byte(`{
		"label": "new-rack",
		"rackInfo": {
			"rows": 1,
			"columns": 1,
			"orderIndex": "RACK_ORDER_INDEX_BOTTOM_LEFT",
			"coolingType": "RACK_COOLING_TYPE_AIR"
		}
	}`), 0o600); err != nil {
		t.Fatalf("write rack json: %v", err)
	}

	getCount := 0
	saveCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /device_set.v1.DeviceSetService/GetDeviceSet", func(w http.ResponseWriter, r *http.Request) {
		getCount++
		t.Errorf("unexpected preflight request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "preflight should not be called", http.StatusTeapot)
	})
	mux.HandleFunc("POST /device_set.v1.DeviceSetService/SaveRack", func(w http.ResponseWriter, _ *http.Request) {
		saveCount++
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte("{}"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	err := newRootCommand().Run(context.Background(), []string{
		"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
		"racks", "save", "--json", jsonPath,
	})
	if err != nil {
		t.Fatalf("racks save json without device set id error = %v, want success", err)
	}
	if getCount != 0 {
		t.Fatalf("get count = %d, want 0", getCount)
	}
	if saveCount != 1 {
		t.Fatalf("save count = %d, want 1", saveCount)
	}
}

func TestRackSaveJSONRejectsGroupID(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	jsonPath := filepath.Join(t.TempDir(), "rack.json")
	if err := os.WriteFile(jsonPath, []byte(`{
		"deviceSetId": "42",
		"label": "rack-label",
		"rackInfo": {
			"rows": 1,
			"columns": 1,
			"orderIndex": "RACK_ORDER_INDEX_BOTTOM_LEFT",
			"coolingType": "RACK_COOLING_TYPE_AIR"
		}
	}`), 0o600); err != nil {
		t.Fatalf("write rack json: %v", err)
	}

	saveCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /device_set.v1.DeviceSetService/GetDeviceSet", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte(`{"device_set":{"id":"42","type":"DEVICE_SET_TYPE_GROUP","label":"wrong-type"}}`))
	})
	mux.HandleFunc("POST /device_set.v1.DeviceSetService/SaveRack", func(w http.ResponseWriter, r *http.Request) {
		saveCount++
		t.Errorf("unexpected save request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "save should not be called", http.StatusTeapot)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	err := newRootCommand().Run(context.Background(), []string{
		"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
		"racks", "save", "--json", jsonPath,
	})
	if err == nil || !strings.Contains(err.Error(), "device set 42 is a group, not a rack") {
		t.Fatalf("racks save json error = %v, want group/rack mismatch", err)
	}
	if saveCount != 0 {
		t.Fatalf("save count = %d, want 0", saveCount)
	}
}

func TestRackAddDevicesRequiresTargetRackID(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	requestCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "request should not be called", http.StatusTeapot)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	err := newRootCommand().Run(context.Background(), []string{
		"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
		"racks", "add-devices", "--device", "miner-1",
	})
	if err == nil || !strings.Contains(err.Error(), "target-rack-id") {
		t.Fatalf("racks add-devices error = %v, want target-rack-id requirement", err)
	}
	if requestCount != 0 {
		t.Fatalf("request count = %d, want 0", requestCount)
	}
}

func TestRackAddDevicesRejectsAllDevicesBeforeRequest(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	requestCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "request should not be called", http.StatusTeapot)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	err := newRootCommand().Run(context.Background(), []string{
		"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
		"racks", "add-devices", "--target-rack-id", "42", "--all-devices",
	})
	if err == nil || !strings.Contains(err.Error(), "all-devices") {
		t.Fatalf("racks add-devices error = %v, want all-devices rejection", err)
	}
	if requestCount != 0 {
		t.Fatalf("request count = %d, want 0", requestCount)
	}
}

func TestRackSaveRejectsAllDevicesBeforeRequest(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	jsonPath := filepath.Join(t.TempDir(), "rack.json")
	if err := os.WriteFile(jsonPath, []byte(`{
		"label": "rack-from-json",
		"rackInfo": {
			"rows": 1,
			"columns": 1,
			"orderIndex": "RACK_ORDER_INDEX_BOTTOM_LEFT",
			"coolingType": "RACK_COOLING_TYPE_AIR"
		},
		"deviceSelector": {
			"deviceList": {
				"deviceIdentifiers": ["miner-1"]
			}
		}
	}`), 0o600); err != nil {
		t.Fatalf("write rack json: %v", err)
	}

	requestCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "request should not be called", http.StatusTeapot)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	err := newRootCommand().Run(context.Background(), []string{
		"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
		"racks", "save", "--json", jsonPath, "--all-devices",
	})
	if err == nil || !strings.Contains(err.Error(), "all-devices") {
		t.Fatalf("racks save error = %v, want all-devices rejection", err)
	}
	if requestCount != 0 {
		t.Fatalf("request count = %d, want 0", requestCount)
	}
}

func boundedSelectorDeviceIDsFromArgs(t *testing.T, srv *httptest.Server, args ...string) ([]string, error) {
	t.Helper()
	client, err := New(context.Background(), Options{Server: srv.URL + "/", APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	var deviceIDs []string
	cmd := &cli.Command{
		Name:  "selector-test",
		Flags: generatedBoundedMinerSelectorFlags(),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			selector, err := generatedBuildBoundedMinerSelector(ctx, cmd, client)
			if err != nil {
				return err
			}
			deviceIDs = selector.GetIncludeDevices().GetDeviceIdentifiers()
			return nil
		},
	}
	if err := cmd.Run(context.Background(), append([]string{"selector-test"}, args...)); err != nil {
		return nil, fmt.Errorf("run selector harness: %w", err)
	}
	return deviceIDs, nil
}

func deviceSetIDFromRequest(t *testing.T, r *http.Request) string {
	t.Helper()
	var body struct {
		DeviceSetID string `json:"device_set_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("decode device set request: %v", err)
	}
	return body.DeviceSetID
}

func TestBoundedMinerSelectorVerifiesDeviceSetIDs(t *testing.T) {
	t.Run("group id rejects rack", func(t *testing.T) {
		listMembersCount := 0
		mux := http.NewServeMux()
		mux.HandleFunc("POST /device_set.v1.DeviceSetService/GetDeviceSet", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", contentTypeJSON)
			_, _ = w.Write([]byte(`{"device_set":{"id":"42","type":"DEVICE_SET_TYPE_RACK","label":"rack-42"}}`))
		})
		mux.HandleFunc("POST /device_set.v1.DeviceSetService/ListDeviceSetMembers", func(w http.ResponseWriter, r *http.Request) {
			listMembersCount++
			t.Errorf("unexpected member list request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "members should not be listed", http.StatusTeapot)
		})
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)

		_, err := boundedSelectorDeviceIDsFromArgs(t, srv, "--group-id", "42")
		if err == nil || !strings.Contains(err.Error(), "verify group ids: device set 42 is a rack, not a group") {
			t.Fatalf("selector error = %v, want group/rack mismatch", err)
		}
		if listMembersCount != 0 {
			t.Fatalf("list members count = %d, want 0", listMembersCount)
		}
	})

	t.Run("rack id rejects group", func(t *testing.T) {
		listMembersCount := 0
		mux := http.NewServeMux()
		mux.HandleFunc("POST /device_set.v1.DeviceSetService/GetDeviceSet", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", contentTypeJSON)
			_, _ = w.Write([]byte(`{"device_set":{"id":"42","type":"DEVICE_SET_TYPE_GROUP","label":"group-42"}}`))
		})
		mux.HandleFunc("POST /device_set.v1.DeviceSetService/ListDeviceSetMembers", func(w http.ResponseWriter, r *http.Request) {
			listMembersCount++
			t.Errorf("unexpected member list request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "members should not be listed", http.StatusTeapot)
		})
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)

		_, err := boundedSelectorDeviceIDsFromArgs(t, srv, "--rack-id", "42")
		if err == nil || !strings.Contains(err.Error(), "verify rack ids: device set 42 is a group, not a rack") {
			t.Fatalf("selector error = %v, want rack/group mismatch", err)
		}
		if listMembersCount != 0 {
			t.Fatalf("list members count = %d, want 0", listMembersCount)
		}
	})

	t.Run("matching group and rack ids expand members", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("POST /device_set.v1.DeviceSetService/GetDeviceSet", func(w http.ResponseWriter, r *http.Request) {
			deviceSetID := deviceSetIDFromRequest(t, r)
			deviceSetType := "DEVICE_SET_TYPE_GROUP"
			if deviceSetID == "9" {
				deviceSetType = "DEVICE_SET_TYPE_RACK"
			}
			w.Header().Set("Content-Type", contentTypeJSON)
			_, _ = w.Write([]byte(`{"device_set":{"id":"` + deviceSetID + `","type":"` + deviceSetType + `","label":"device-set-` + deviceSetID + `"}}`))
		})
		mux.HandleFunc("POST /device_set.v1.DeviceSetService/ListDeviceSetMembers", func(w http.ResponseWriter, r *http.Request) {
			deviceSetID := deviceSetIDFromRequest(t, r)
			deviceID := "group-device"
			if deviceSetID == "9" {
				deviceID = "rack-device"
			}
			w.Header().Set("Content-Type", contentTypeJSON)
			_, _ = w.Write([]byte(`{"members":[{"device_identifier":"` + deviceID + `"}]}`))
		})
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)

		got, err := boundedSelectorDeviceIDsFromArgs(t, srv, "--group-id", "7", "--rack-id", "9")
		if err != nil {
			t.Fatalf("selector error = %v, want success", err)
		}
		want := []string{"group-device", "rack-device"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("device ids = %v, want %v", got, want)
		}
	})
}

func TestDeviceSetStatsVerifyType(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		actualType string
		wantError  string
	}{
		{
			name:       "groups stats rejects rack id",
			args:       []string{"groups", "stats", "--device-set-ids", "42"},
			actualType: "DEVICE_SET_TYPE_RACK",
			wantError:  "device set 42 is a rack, not a group",
		},
		{
			name:       "racks stats rejects group id",
			args:       []string{"racks", "stats", "--device-set-ids", "42"},
			actualType: "DEVICE_SET_TYPE_GROUP",
			wantError:  "device set 42 is a group, not a rack",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pinFleetAuthEnv(t, nil)

			statsCount := 0
			mux := http.NewServeMux()
			mux.HandleFunc("POST /device_set.v1.DeviceSetService/GetDeviceSet", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", contentTypeJSON)
				_, _ = w.Write([]byte(`{"device_set":{"id":"42","type":"` + tt.actualType + `","label":"wrong-type"}}`))
			})
			mux.HandleFunc("POST /device_set.v1.DeviceSetService/GetDeviceSetStats", func(w http.ResponseWriter, r *http.Request) {
				statsCount++
				t.Errorf("unexpected stats request: %s %s", r.Method, r.URL.Path)
				http.Error(w, "stats should not be called", http.StatusTeapot)
			})
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			err := newRootCommand().Run(context.Background(), append([]string{
				"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
			}, tt.args...))
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("fleetcli %s error = %v, want %q", strings.Join(tt.args, " "), err, tt.wantError)
			}
			if statsCount != 0 {
				t.Fatalf("stats count = %d, want 0", statsCount)
			}
		})
	}

	t.Run("matching group id proceeds", func(t *testing.T) {
		pinFleetAuthEnv(t, nil)

		var calls []string
		mux := http.NewServeMux()
		mux.HandleFunc("POST /device_set.v1.DeviceSetService/GetDeviceSet", func(w http.ResponseWriter, _ *http.Request) {
			calls = append(calls, "get")
			w.Header().Set("Content-Type", contentTypeJSON)
			_, _ = w.Write([]byte(`{"device_set":{"id":"42","type":"DEVICE_SET_TYPE_GROUP","label":"group-42"}}`))
		})
		mux.HandleFunc("POST /device_set.v1.DeviceSetService/GetDeviceSetStats", func(w http.ResponseWriter, _ *http.Request) {
			calls = append(calls, "stats")
			w.Header().Set("Content-Type", contentTypeJSON)
			_, _ = w.Write([]byte(`{"stats":[]}`))
		})
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)

		err := newRootCommand().Run(context.Background(), []string{
			"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
			"groups", "stats", "--device-set-ids", "42",
		})
		if err != nil {
			t.Fatalf("groups stats error = %v, want success", err)
		}
		want := []string{"get", "stats"}
		if strings.Join(calls, ",") != strings.Join(want, ",") {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	})
}

func TestDeviceSetMembersVerifyType(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		actualType string
		wantError  string
	}{
		{
			name:       "groups members rejects rack id",
			args:       []string{"groups", "members", "--device-set-id", "42"},
			actualType: "DEVICE_SET_TYPE_RACK",
			wantError:  "device set 42 is a rack, not a group",
		},
		{
			name:       "racks members rejects group id",
			args:       []string{"racks", "members", "--device-set-id", "42"},
			actualType: "DEVICE_SET_TYPE_GROUP",
			wantError:  "device set 42 is a group, not a rack",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pinFleetAuthEnv(t, nil)

			membersCount := 0
			mux := http.NewServeMux()
			mux.HandleFunc("POST /device_set.v1.DeviceSetService/GetDeviceSet", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", contentTypeJSON)
				_, _ = w.Write([]byte(`{"device_set":{"id":"42","type":"` + tt.actualType + `","label":"wrong-type"}}`))
			})
			mux.HandleFunc("POST /device_set.v1.DeviceSetService/ListDeviceSetMembers", func(w http.ResponseWriter, r *http.Request) {
				membersCount++
				t.Errorf("unexpected members request: %s %s", r.Method, r.URL.Path)
				http.Error(w, "members should not be called", http.StatusTeapot)
			})
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			err := newRootCommand().Run(context.Background(), append([]string{
				"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
			}, tt.args...))
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("fleetcli %s error = %v, want %q", strings.Join(tt.args, " "), err, tt.wantError)
			}
			if membersCount != 0 {
				t.Fatalf("members count = %d, want 0", membersCount)
			}
		})
	}

	t.Run("matching group id proceeds", func(t *testing.T) {
		pinFleetAuthEnv(t, nil)

		var calls []string
		mux := http.NewServeMux()
		mux.HandleFunc("POST /device_set.v1.DeviceSetService/GetDeviceSet", func(w http.ResponseWriter, _ *http.Request) {
			calls = append(calls, "get")
			w.Header().Set("Content-Type", contentTypeJSON)
			_, _ = w.Write([]byte(`{"device_set":{"id":"42","type":"DEVICE_SET_TYPE_GROUP","label":"group-42"}}`))
		})
		mux.HandleFunc("POST /device_set.v1.DeviceSetService/ListDeviceSetMembers", func(w http.ResponseWriter, _ *http.Request) {
			calls = append(calls, "members")
			w.Header().Set("Content-Type", contentTypeJSON)
			_, _ = w.Write([]byte(`{"members":[]}`))
		})
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)

		err := newRootCommand().Run(context.Background(), []string{
			"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
			"groups", "members", "--device-set-id", "42",
		})
		if err != nil {
			t.Fatalf("groups members error = %v, want success", err)
		}
		want := []string{"get", "members"}
		if strings.Join(calls, ",") != strings.Join(want, ",") {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	})
}

func TestResolvedAuthInputs(t *testing.T) {
	authLogin := []string{"auth", "login"}
	tests := []struct {
		name     string
		env      map[string]string
		path     []string
		argv     []string
		wantKey  string
		wantUser string
		wantPass string
	}{
		{
			name:     "env api key and creds all pass through",
			env:      map[string]string{envFleetAPIKey: "k", envFleetUsername: "u", envFleetPassword: "p"},
			path:     authLogin,
			argv:     []string{"auth", "login"},
			wantKey:  "k",
			wantUser: "u",
			wantPass: "p",
		},
		{
			name:    "cli api key only",
			path:    authLogin,
			argv:    []string{"--api-key", "clik", "auth", "login"},
			wantKey: "clik",
		},
		{
			name:     "cli username before subcommand with env password",
			env:      map[string]string{envFleetPassword: "p"},
			path:     authLogin,
			argv:     []string{"--username", "u", "auth", "login"},
			wantUser: "u",
			wantPass: "p",
		},
		{
			name:     "auth username flag after subcommand binds to root",
			env:      map[string]string{envFleetPassword: "p"},
			path:     authLogin,
			argv:     []string{"auth", "login", "--username", "u"},
			wantUser: "u",
			wantPass: "p",
		},
		{
			name:     "cli username overrides env username",
			env:      map[string]string{envFleetUsername: "envu"},
			path:     authLogin,
			argv:     []string{"--username", "cliu", "auth", "login"},
			wantUser: "cliu",
		},
		{
			name:     "env api key kept alongside cli username and env password",
			env:      map[string]string{envFleetAPIKey: "k", envFleetPassword: "p"},
			path:     authLogin,
			argv:     []string{"--username", "u", "auth", "login"},
			wantKey:  "k",
			wantUser: "u",
			wantPass: "p",
		},
		{
			name:    "pools update local username does not leak into auth",
			env:     map[string]string{envFleetAPIKey: "k"},
			path:    []string{"pools", "update"},
			argv:    []string{"pools", "update", "--username", "pooluser"},
			wantKey: "k",
		},
		{
			name:    "pools validate local username does not leak into auth",
			env:     map[string]string{envFleetAPIKey: "k"},
			path:    []string{"pools", "validate"},
			argv:    []string{"pools", "validate", "--username", "pooluser"},
			wantKey: "k",
		},
		{
			name: "onboarding create-admin credentials do not leak into auth",
			path: []string{"onboarding", "create-admin"},
			argv: []string{"onboarding", "create-admin", "--username", "au", "--password-stdin"},
		},
		{
			name: "nothing set resolves empty",
			path: authLogin,
			argv: []string{"auth", "login"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pinFleetAuthEnv(t, tt.env)
			apiKey, username, password := probeAuthInputs(t, tt.path, tt.argv...)
			if apiKey != tt.wantKey || username != tt.wantUser || password != tt.wantPass {
				t.Errorf("resolvedAuthInputs = (%q, %q, %q), want (%q, %q, %q)",
					apiKey, username, password, tt.wantKey, tt.wantUser, tt.wantPass)
			}
		})
	}
}

// TestApiKeyListAuthenticatesWithEnvCreds covers the bug where an env
// FLEET_API_KEY blanked env FLEET_USERNAME/FLEET_PASSWORD, breaking
// session-only commands even though credentials were available.
func TestApiKeyListAuthenticatesWithEnvCreds(t *testing.T) {
	pinFleetAuthEnv(t, map[string]string{
		envFleetAPIKey:   "env-key",
		envFleetUsername: "admin",
		envFleetPassword: "proto",
	})

	var authBody map[string]any
	var listAuthHeader string
	var listCookie string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth.v1.AuthService/Authenticate", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&authBody); err != nil {
			t.Errorf("decode authenticate request: %v", err)
		}
		http.SetCookie(w, &http.Cookie{Name: "fleet_session", Value: "sess", Path: "/", Secure: true})
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte("{}"))
	})
	mux.HandleFunc("POST /apikey.v1.ApiKeyService/ListApiKeys", func(w http.ResponseWriter, r *http.Request) {
		listAuthHeader = r.Header.Get("Authorization")
		if cookie, err := r.Cookie("fleet_session"); err == nil {
			listCookie = cookie.Value
		}
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte(`{"api_keys":[]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	err := newRootCommand().Run(context.Background(), []string{
		"fleetcli", "--server", srv.URL + "/", "apikey", "list",
	})
	if err != nil {
		t.Fatalf("apikey list with env key and creds should succeed, got: %v", err)
	}
	if authBody["username"] != "admin" || authBody["password"] != "proto" {
		t.Errorf("authenticate body = %v, want env username/password", authBody)
	}
	if listAuthHeader != "" {
		t.Errorf("ListApiKeys Authorization = %q, want empty for session-only command", listAuthHeader)
	}
	if listCookie != "sess" {
		t.Errorf("ListApiKeys session cookie = %q, want %q", listCookie, "sess")
	}
}

func TestAuthLoginReadsPasswordStdin(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	var authBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth.v1.AuthService/Authenticate", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&authBody); err != nil {
			t.Errorf("decode authenticate request: %v", err)
		}
		http.SetCookie(w, &http.Cookie{Name: "fleet_session", Value: "sess", Path: "/", Secure: true})
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte("{}"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	withStdin(t, "stdin-secret\n", func() {
		err := newRootCommand().Run(context.Background(), []string{
			"fleetcli", "--server", srv.URL + "/", "--username", "admin", "--password-stdin", "auth", "login",
		})
		if err != nil {
			t.Fatalf("auth login with --password-stdin error = %v", err)
		}
	})

	if authBody["username"] != "admin" || authBody["password"] != "stdin-secret" {
		t.Errorf("authenticate body = %v, want stdin password", authBody)
	}
}

func TestOnboardingCreateAdminReadsPasswordStdin(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	var createBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /onboarding.v1.OnboardingService/CreateAdminLogin", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
			t.Errorf("decode create-admin request: %v", err)
		}
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte(`{"user_id":"admin-id"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	withStdin(t, "admin-secret\n", func() {
		err := newRootCommand().Run(context.Background(), []string{
			"fleetcli", "--server", srv.URL + "/", "onboarding", "create-admin",
			"--username", "admin", "--password-stdin",
		})
		if err != nil {
			t.Fatalf("onboarding create-admin with --password-stdin error = %v", err)
		}
	})

	if createBody["username"] != "admin" || createBody["password"] != "admin-secret" {
		t.Errorf("create-admin body = %v, want stdin password", createBody)
	}
}

func TestOnboardingCreateAdminRejectsPasswordFlag(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	err := newRootCommand().Run(context.Background(), []string{
		"fleetcli", "onboarding", "create-admin", "--username", "admin", "--password", "admin-secret",
	})
	if err == nil || !strings.Contains(err.Error(), "password") {
		t.Fatalf("onboarding create-admin --password error = %v, want unknown password flag", err)
	}
}

func TestApiKeyCommandsRejectAPIKeyOnly(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "create", args: []string{"apikey", "create", "--name", "test-key"}},
		{name: "list", args: []string{"apikey", "list"}},
		{name: "revoke", args: []string{"apikey", "revoke", "--key-id", "key-1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pinFleetAuthEnv(t, nil)

			requestCount := 0
			mux := http.NewServeMux()
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				requestCount++
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				http.Error(w, "unexpected request", http.StatusTeapot)
			})
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			err := newRootCommand().Run(context.Background(), append([]string{
				"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
			}, tt.args...))
			if err == nil || !strings.Contains(err.Error(), "requires username and password") {
				t.Fatalf("fleetcli %s error = %v, want username/password requirement", strings.Join(tt.args, " "), err)
			}
			if !strings.Contains(err.Error(), "session-only") {
				t.Errorf("error should explain session-only API key lifecycle commands, got: %v", err)
			}
			if strings.Contains(err.Error(), "either an API key") {
				t.Errorf("error should not claim API keys are accepted, got: %v", err)
			}
			if requestCount != 0 {
				t.Fatalf("request count = %d, want 0", requestCount)
			}
		})
	}
}

// TestPoolsValidateBearerWithLocalUsername covers the bug where the
// subcommand-local --username flag hijacked Fleet auth and discarded the API
// key: the pool username must reach the request body while auth stays authenticated.
func TestPoolsValidateAuthenticatedWithLocalUsername(t *testing.T) {
	pinFleetAuthEnv(t, map[string]string{envFleetAPIKey: "k"})

	var gotAuth string
	var gotBody map[string]any
	mux := http.NewServeMux()
	forbidFirmwareEndpoint(t, mux, "POST /auth.v1.AuthService/Authenticate")
	mux.HandleFunc("POST /pools.v1.PoolsService/ValidatePool", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode validate request: %v", err)
		}
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte("{}"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	err := newRootCommand().Run(context.Background(), []string{
		"fleetcli", "--server", srv.URL + "/",
		"pools", "validate", "--url", "stratum+tcp://pool:3333", "--username", "pooluser",
	})
	if err != nil {
		t.Fatalf("pools validate with env api key should succeed, got: %v", err)
	}
	if gotAuth != "Bearer k" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer k")
	}
	if gotBody["username"] != "pooluser" {
		t.Errorf("request username = %v, want pooluser", gotBody["username"])
	}
	if gotBody["url"] != "stratum+tcp://pool:3333" {
		t.Errorf("request url = %v, want the pool url", gotBody["url"])
	}
}

func TestPoolsCreateJSONPoolConfigFlagOverridePreservesFields(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	jsonPath := filepath.Join(t.TempDir(), "pool.json")
	if err := os.WriteFile(jsonPath, []byte(`{
		"pool_config": {
			"pool_name": "old-name",
			"url": "stratum+tcp://pool:3333",
			"username": "pool-user",
			"password": "pool-pass"
		}
	}`), 0o600); err != nil {
		t.Fatalf("write pool json: %v", err)
	}

	var gotAuth string
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /pools.v1.PoolsService/CreatePool", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode create pool request: %v", err)
		}
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte("{}"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	err := newRootCommand().Run(context.Background(), []string{
		"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
		"pools", "create", "--json", jsonPath, "--pool-name", "new-name",
	})
	if err != nil {
		t.Fatalf("pools create with json override should succeed, got: %v", err)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-key")
	}
	poolConfig, ok := gotBody["pool_config"].(map[string]any)
	if !ok {
		t.Fatalf("pool_config = %#v, want object", gotBody["pool_config"])
	}
	want := map[string]string{
		"pool_name": "new-name",
		"url":       "stratum+tcp://pool:3333",
		"username":  "pool-user",
		"password":  "pool-pass",
	}
	for field, wantValue := range want {
		if got := poolConfig[field]; got != wantValue {
			t.Errorf("pool_config.%s = %v, want %q", field, got, wantValue)
		}
	}
}

func TestPoolsReadPasswordFromStdin(t *testing.T) {
	tests := []struct {
		name     string
		route    string
		args     []string
		password func(map[string]any) any
	}{
		{
			name:  "create",
			route: "POST /pools.v1.PoolsService/CreatePool",
			args: []string{
				"pools", "create",
				"--pool-name", "pool-name",
				"--url", "stratum+tcp://pool:3333",
				"--username", "pool-user",
				"--pool-password-stdin",
			},
			password: func(body map[string]any) any {
				poolConfig, _ := body["pool_config"].(map[string]any)
				return poolConfig["password"]
			},
		},
		{
			name:  "update",
			route: "POST /pools.v1.PoolsService/UpdatePool",
			args: []string{
				"pools", "update",
				"--pool-id", "12",
				"--pool-password-stdin",
			},
			password: func(body map[string]any) any {
				return body["password"]
			},
		},
		{
			name:  "validate",
			route: "POST /pools.v1.PoolsService/ValidatePool",
			args: []string{
				"pools", "validate",
				"--url", "stratum+tcp://pool:3333",
				"--username", "pool-user",
				"--pool-password-stdin",
			},
			password: func(body map[string]any) any {
				return body["password"]
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pinFleetAuthEnv(t, nil)

			var gotBody map[string]any
			mux := http.NewServeMux()
			mux.HandleFunc(tt.route, func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
					t.Errorf("decode pool request: %v", err)
				}
				w.Header().Set("Content-Type", contentTypeJSON)
				_, _ = w.Write([]byte("{}"))
			})
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			withStdin(t, "pool-secret\n", func() {
				err := newRootCommand().Run(context.Background(), append([]string{
					"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
				}, tt.args...))
				if err != nil {
					t.Fatalf("fleetcli %s error = %v, want success", strings.Join(tt.args, " "), err)
				}
			})

			if got := tt.password(gotBody); got != "pool-secret" {
				t.Fatalf("pool password = %v, want pool-secret; body = %#v", got, gotBody)
			}
		})
	}
}

func TestPoolsRejectPasswordFlag(t *testing.T) {
	tests := [][]string{
		{"pools", "create", "--password", "pool-secret"},
		{"pools", "update", "--password", "pool-secret"},
		{"pools", "validate", "--password", "pool-secret"},
	}

	for _, args := range tests {
		t.Run(strings.Join(args[:2], " "), func(t *testing.T) {
			pinFleetAuthEnv(t, nil)

			err := newRootCommand().Run(context.Background(), append([]string{"fleetcli"}, args...))
			if err == nil || !strings.Contains(err.Error(), "password") {
				t.Fatalf("fleetcli %s error = %v, want password flag rejection", strings.Join(args, " "), err)
			}
		})
	}
}
