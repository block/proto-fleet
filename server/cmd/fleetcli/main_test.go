package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

func leafCommandPaths(commands []*cli.Command) map[string]bool {
	paths := map[string]bool{}
	var visit func([]string, *cli.Command)
	visit = func(parent []string, command *cli.Command) {
		path := append(append([]string{}, parent...), command.Name)
		if len(command.Commands) == 0 {
			paths[strings.Join(path, " ")] = true
			return
		}
		for _, child := range command.Commands {
			visit(path, child)
		}
	}
	for _, command := range commands {
		visit(nil, command)
	}
	return paths
}

func TestGeneratedLeafCommandsMatchManifest(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "tools", "generate-fleet-cli", "commands.json"))
	if err != nil {
		t.Fatalf("read commands manifest: %v", err)
	}
	var manifest struct {
		Commands []struct {
			Group    string `json:"group"`
			Subgroup string `json:"subgroup"`
			Command  string `json:"command"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse commands manifest: %v", err)
	}

	want := map[string]bool{}
	for _, command := range manifest.Commands {
		parts := []string{command.Group}
		if command.Subgroup != "" {
			parts = append(parts, command.Subgroup)
		}
		parts = append(parts, command.Command)
		want[strings.Join(parts, " ")] = true
	}
	got := leafCommandPaths(generatedCommands())
	if len(got) != 117 || len(want) != 117 {
		t.Fatalf("generated leaves = %d, manifest leaves = %d, want 117 each", len(got), len(want))
	}
	for path := range want {
		if !got[path] {
			t.Errorf("manifest command %q missing from generated command tree", path)
		}
	}
	for path := range got {
		if !want[path] {
			t.Errorf("generated command %q missing from manifest", path)
		}
	}
	if gotAll := leafCommandPaths(allCommands()); len(gotAll) != 131 {
		t.Fatalf("all command leaves = %d, want 131", len(gotAll))
	}
}

func TestGeneratedRequiredFieldsAcceptJSONOrFlags(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	var request map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /schedule.v1.ScheduleService/CreateSchedule", func(w http.ResponseWriter, r *http.Request) {
		request = requestBodyMap(t, r)
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	requestPath := filepath.Join(t.TempDir(), "schedule.json")
	requestJSON := []byte(`{
		"name":"nightly-reboot",
		"action":"SCHEDULE_ACTION_REBOOT",
		"schedule_type":"SCHEDULE_TYPE_ONE_TIME",
		"start_date":"2030-01-01",
		"start_time":"12:00",
		"timezone":"UTC"
	}`)
	if err := os.WriteFile(requestPath, requestJSON, 0o600); err != nil {
		t.Fatalf("write schedule request: %v", err)
	}

	err := newRootCommand().Run(context.Background(), []string{
		"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
		"schedules", "create", "--json", requestPath,
	})
	if err != nil {
		t.Fatalf("schedule create with JSON-only required fields: %v", err)
	}
	if request["name"] != "nightly-reboot" || request["start_date"] != "2030-01-01" {
		t.Fatalf("schedule request = %#v, want JSON fields", request)
	}

	err = newRootCommand().Run(context.Background(), []string{
		"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
		"schedules", "create",
	})
	if err == nil || !strings.Contains(err.Error(), `required field "action" must be provided with --action or --json`) {
		t.Fatalf("schedule create missing fields error = %v, want merged-input validation", err)
	}
}

func TestGeneratedJSONOnlyCommandCanMakeJSONOptional(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	requestCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /fleetmanagement.v1.FleetManagementService/GetMinerModelGroups", func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if body := requestBodyMap(t, r); len(body) != 0 {
			t.Fatalf("model groups request = %#v, want empty request", body)
		}
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	err := newRootCommand().Run(context.Background(), []string{
		"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
		"miners", "model-groups",
	})
	if err != nil {
		t.Fatalf("model groups without JSON: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("request count = %d, want 1", requestCount)
	}
}

func TestGeneratedJSONOnlyCommandAcceptsSelectorFlags(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	var request map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /fleetmanagement.v1.FleetManagementService/RenameMiners", func(w http.ResponseWriter, r *http.Request) {
		request = requestBodyMap(t, r)
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	requestPath := filepath.Join(t.TempDir(), "rename.json")
	requestJSON := []byte(`{
		"name_config": {
			"properties": [{"string_value": {"value": "rig"}}],
			"separator": "-"
		}
	}`)
	if err := os.WriteFile(requestPath, requestJSON, 0o600); err != nil {
		t.Fatalf("write rename request: %v", err)
	}

	err := newRootCommand().Run(context.Background(), []string{
		"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
		"miners", "rename", "--json", requestPath, "--device", "miner-1",
	})
	if err != nil {
		t.Fatalf("rename with selector flags: %v", err)
	}
	selector, ok := request["device_selector"].(map[string]any)
	if !ok {
		t.Fatalf("rename request = %#v, want device_selector", request)
	}
	include, ok := selector["include_devices"].(map[string]any)
	if !ok {
		t.Fatalf("device selector = %#v, want include_devices", selector)
	}
	deviceIDs, ok := include["device_identifiers"].([]any)
	if !ok || len(deviceIDs) != 1 || deviceIDs[0] != "miner-1" {
		t.Fatalf("device identifiers = %#v, want miner-1", include["device_identifiers"])
	}
}

func TestGeneratedHelpLabelsRequiredInputs(t *testing.T) {
	schedules := findSubcommand(t, generatedSchedulesCommand(), "create")
	var nameFlag *cli.StringFlag
	for _, flag := range schedules.Flags {
		if flag.Names()[0] == "name" {
			nameFlag, _ = flag.(*cli.StringFlag)
			break
		}
	}
	if nameFlag == nil || !strings.Contains(nameFlag.Usage, "required unless provided by --json") || nameFlag.Required {
		t.Fatalf("schedule name flag = %#v, want JSON-aware required help without urfave prevalidation", nameFlag)
	}

	roles := findSubcommand(t, generatedRolesCommand(), "create")
	var roleNameFlag *cli.StringFlag
	for _, flag := range roles.Flags {
		if flag.Names()[0] == "name" {
			roleNameFlag, _ = flag.(*cli.StringFlag)
			break
		}
	}
	if roleNameFlag == nil || !strings.Contains(roleNameFlag.Usage, "(required)") || !roleNameFlag.Required {
		t.Fatalf("role name flag = %#v, want visibly required flag", roleNameFlag)
	}
}

func TestGeneratedSetEnabledRequiresExplicitBoolean(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	requestCount := 0
	var request map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /curtailment.v1.CurtailmentService/SetCurtailmentAutomationRuleEnabled", func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		request = requestBodyMap(t, r)
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	baseArgs := []string{
		"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
		"curtailment", "automation-rules", "set-enabled", "--rule-id", "42",
	}
	err := newRootCommand().Run(context.Background(), baseArgs)
	if err == nil || !strings.Contains(err.Error(), `Required flag "enabled" not set`) {
		t.Fatalf("set-enabled missing boolean error = %v, want required flag error", err)
	}
	if requestCount != 0 {
		t.Fatalf("request count = %d, want no request without --enabled", requestCount)
	}

	err = newRootCommand().Run(context.Background(), append(baseArgs, "--enabled=false"))
	if err != nil {
		t.Fatalf("set-enabled with explicit false: %v", err)
	}
	if requestCount != 1 || request["rule_id"] != "42" {
		t.Fatalf("set-enabled request count/body = %d/%#v, want explicit false request", requestCount, request)
	}
	if enabled, present := request["enabled"]; present && enabled != false {
		t.Fatalf("set-enabled body = %#v, want false or omitted proto default", request)
	}
}

func TestGeneratedComplexMutationsRequireJSON(t *testing.T) {
	tests := []struct {
		name    string
		command *cli.Command
	}{
		{name: "building rack assignment", command: findSubcommand(t, generatedBuildingsCommand(), "assign-racks")},
		{name: "rack slot position", command: findSubcommand(t, generatedRacksCommand(), "set-slot")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.command.Flags) != 1 || tt.command.Flags[0].Names()[0] != "json" {
				t.Fatalf("flags = %#v, want only --json", tt.command.Flags)
			}
			jsonFlag, ok := tt.command.Flags[0].(*cli.StringFlag)
			if !ok || !jsonFlag.Required || !strings.Contains(jsonFlag.Usage, "(required)") {
				t.Fatalf("json flag = %#v, want visibly required JSON", tt.command.Flags[0])
			}
		})
	}
}

func TestGeneratedAssignmentsRequireTargetsAndMembers(t *testing.T) {
	tests := []struct {
		name          string
		command       *cli.Command
		requiredFlags []string
	}{
		{name: "sites assign devices", command: findSubcommand(t, generatedSitesCommand(), "assign-devices"), requiredFlags: []string{"target-site-id", "device-identifiers"}},
		{name: "sites assign buildings", command: findSubcommand(t, generatedSitesCommand(), "assign-buildings"), requiredFlags: []string{"target-site-id", "building-ids"}},
		{name: "sites assign racks", command: findSubcommand(t, generatedSitesCommand(), "assign-racks"), requiredFlags: []string{"target-site-id", "rack-ids"}},
		{name: "buildings assign devices", command: findSubcommand(t, generatedBuildingsCommand(), "assign-devices"), requiredFlags: []string{"target-building-id", "device-identifiers"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, requiredFlag := range tt.requiredFlags {
				found := false
				for _, flag := range tt.command.Flags {
					if flag.Names()[0] != requiredFlag {
						continue
					}
					found = true
					switch typed := flag.(type) {
					case *cli.Int64Flag:
						if !typed.Required || !strings.Contains(typed.Usage, "(required)") {
							t.Fatalf("flag = %#v, want visibly required target", typed)
						}
					case *cli.Int64SliceFlag:
						if !typed.Required || !strings.Contains(typed.Usage, "(required)") {
							t.Fatalf("flag = %#v, want visibly required member list", typed)
						}
					case *cli.StringSliceFlag:
						if !typed.Required || !strings.Contains(typed.Usage, "(required)") {
							t.Fatalf("flag = %#v, want visibly required member list", typed)
						}
					default:
						t.Fatalf("flag = %#v, want supported required flag type", flag)
					}
				}
				if !found {
					t.Fatalf("required flag %q not found", requiredFlag)
				}
			}
		})
	}
}

func TestGeneratedReviewedCommandsRejectIncompleteRequestsLocally(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "request should not be called", http.StatusTeapot)
	}))
	t.Cleanup(srv.Close)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "mqtt create",
			args: []string{"curtailment", "mqtt-sources", "create", "--mqtt-password-stdin"},
			want: "source-name",
		},
		{
			name: "mqtt test",
			args: []string{"curtailment", "mqtt-sources", "test", "--mqtt-password-stdin"},
			want: "topic",
		},
		{
			name: "automation create",
			args: []string{"curtailment", "automation-rules", "create"},
			want: "rule-name",
		},
		{
			name: "response profile partial fan settings update",
			args: []string{
				"curtailment", "profiles", "update",
				"--profile-id", "1",
				"--profile-name", "Test profile",
				"--mode", "full-fleet",
				"--fan-off-delay-sec", "30",
			},
			want: "must be provided together",
		},
		{
			name: "fan off delay exceeds uint32",
			args: []string{
				"curtailment", "events", "start",
				"--mode", "full-fleet",
				"--reason", "test",
				"--fan-off-delay-sec", "4294967296",
			},
			want: "invalid value for --fan-off-delay-sec: 4294967296 exceeds maximum 4294967295",
		},
		{
			name: "fan restore delay exceeds uint32",
			args: []string{
				"curtailment", "events", "start",
				"--mode", "full-fleet",
				"--reason", "test",
				"--fan-restore-delay-sec", "4294967296",
			},
			want: "invalid value for --fan-restore-delay-sec: 4294967296 exceeds maximum 4294967295",
		},
		{
			name: "pool validate",
			args: []string{"pools", "validate", "--pool-password-stdin"},
			want: `required field "url"`,
		},
	}

	stdinPath := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(stdinPath, []byte("unused-secret\n"), 0o600); err != nil {
		t.Fatalf("write stdin fixture: %v", err)
	}
	stdin, err := os.Open(stdinPath)
	if err != nil {
		t.Fatalf("open stdin fixture: %v", err)
	}
	t.Cleanup(func() { _ = stdin.Close() })
	oldStdin := os.Stdin
	os.Stdin = stdin
	t.Cleanup(func() { os.Stdin = oldStdin })

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := stdin.Seek(0, io.SeekStart); err != nil {
				t.Fatalf("rewind stdin: %v", err)
			}
			err := newRootCommand().Run(context.Background(), append([]string{
				"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
			}, tt.args...))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			offset, err := stdin.Seek(0, io.SeekCurrent)
			if err != nil {
				t.Fatalf("read stdin offset: %v", err)
			}
			if offset != 0 {
				t.Fatalf("stdin offset = %d, want secret unread", offset)
			}
		})
	}
	if requestCount != 0 {
		t.Fatalf("request count = %d, want 0", requestCount)
	}
}

func TestGeneratedJSONOnlyAssignmentRequiresTarget(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	requestCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /buildings.v1.BuildingService/AssignRacksToBuilding", func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	requestPath := filepath.Join(t.TempDir(), "assign-racks.json")
	if err := os.WriteFile(requestPath, []byte(`{"racks":[{"rack_id":"1"}]}`), 0o600); err != nil {
		t.Fatalf("write assignment request: %v", err)
	}
	err := newRootCommand().Run(context.Background(), []string{
		"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
		"buildings", "assign-racks", "--json", requestPath,
	})
	if err == nil || !strings.Contains(err.Error(), `required field "target_building_id" must be provided with --target-building-id or --json`) {
		t.Fatalf("assignment missing target error = %v, want explicit target validation", err)
	}
	if requestCount != 0 {
		t.Fatalf("request count = %d, want no request without assignment target", requestCount)
	}
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

func TestDeviceSetDeleteSkipsTypePreflight(t *testing.T) {
	t.Run("groups delete calls delete directly", func(t *testing.T) {
		pinFleetAuthEnv(t, nil)

		var deleteAuth string
		var deleteBody map[string]any
		deleteCount := 0
		mux := http.NewServeMux()
		mux.HandleFunc("POST /device_set.v1.DeviceSetService/GetDeviceSet", func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("unexpected type preflight request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "preflight should not be called", http.StatusTeapot)
		})
		mux.HandleFunc("POST /device_set.v1.DeviceSetService/DeleteDeviceSet", func(w http.ResponseWriter, r *http.Request) {
			deleteCount++
			deleteAuth = r.Header.Get("Authorization")
			deleteBody = requestBodyMap(t, r)
			http.Error(w, "device set 42 is a rack, not a group", http.StatusBadRequest)
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
		if deleteAuth != "Bearer test-key" {
			t.Errorf("DeleteDeviceSet Authorization = %q, want %q", deleteAuth, "Bearer test-key")
		}
		if deleteCount != 1 {
			t.Fatalf("delete count = %d, want 1", deleteCount)
		}
		if deleteBody["device_set_id"] != "42" {
			t.Fatalf("delete body = %v, want device_set_id 42", deleteBody)
		}
	})

	t.Run("racks delete calls delete directly", func(t *testing.T) {
		pinFleetAuthEnv(t, nil)

		var deleteBody map[string]any
		deleteCount := 0
		mux := http.NewServeMux()
		mux.HandleFunc("POST /device_set.v1.DeviceSetService/GetDeviceSet", func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("unexpected type preflight request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "preflight should not be called", http.StatusTeapot)
		})
		mux.HandleFunc("POST /device_set.v1.DeviceSetService/DeleteDeviceSet", func(w http.ResponseWriter, r *http.Request) {
			deleteCount++
			deleteBody = requestBodyMap(t, r)
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
		if deleteCount != 1 {
			t.Fatalf("delete count = %d, want 1", deleteCount)
		}
		if deleteBody["device_set_id"] != "42" {
			t.Fatalf("delete body = %v, want device_set_id 42", deleteBody)
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

func TestDeviceSetManageCommandsSkipTypePreflight(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		mutationRoute string
		wantError     string
	}{
		{
			name:          "groups add-devices relies on server group validation",
			args:          []string{"groups", "add-devices", "--target-group-id", "42", "--all-devices"},
			mutationRoute: "POST /device_set.v1.DeviceSetService/AddDevicesToGroup",
			wantError:     "device set 42 is a rack, not a group",
		},
		{
			name:          "groups remove-devices relies on server group validation",
			args:          []string{"groups", "remove-devices", "--target-group-id", "42", "--all-devices"},
			mutationRoute: "POST /device_set.v1.DeviceSetService/RemoveDevicesFromGroup",
			wantError:     "device set 42 is a rack, not a group",
		},
		{
			name:          "groups update calls update directly",
			args:          []string{"groups", "update", "--device-set-id", "42", "--label", "group-label"},
			mutationRoute: "POST /device_set.v1.DeviceSetService/UpdateDeviceSet",
			wantError:     "device set 42 is a rack, not a group",
		},
		{
			name:          "racks add-devices relies on server rack validation",
			args:          []string{"racks", "add-devices", "--target-rack-id", "42", "--device", "miner-1"},
			mutationRoute: "POST /device_set.v1.DeviceSetService/AssignDevicesToRack",
			wantError:     "device set 42 is a group, not a rack",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pinFleetAuthEnv(t, nil)

			mutationCount := 0
			var mutationBody map[string]any
			mux := http.NewServeMux()
			mux.HandleFunc("POST /device_set.v1.DeviceSetService/GetDeviceSet", func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected type preflight request: %s %s", r.Method, r.URL.Path)
				http.Error(w, "preflight should not be called", http.StatusTeapot)
			})
			mux.HandleFunc(tt.mutationRoute, func(w http.ResponseWriter, r *http.Request) {
				mutationCount++
				mutationBody = requestBodyMap(t, r)
				http.Error(w, tt.wantError, http.StatusBadRequest)
			})
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			err := newRootCommand().Run(context.Background(), append([]string{
				"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
			}, tt.args...))
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("fleetcli %s error = %v, want %q", strings.Join(tt.args, " "), err, tt.wantError)
			}
			if mutationCount != 1 {
				t.Fatalf("mutation count = %d, want 1", mutationCount)
			}
			if mutationBody == nil {
				t.Fatalf("mutation body was not captured")
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
		},
		"deviceSelector": {
			"deviceList": {
				"deviceIdentifiers": ["miner-1"]
			}
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
		},
		"deviceSelector": {
			"deviceList": {
				"deviceIdentifiers": ["miner-1"]
			}
		}
	}`), 0o600); err != nil {
		t.Fatalf("write rack json: %v", err)
	}

	saveCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /device_set.v1.DeviceSetService/GetDeviceSet", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected type preflight request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "preflight should not be called", http.StatusTeapot)
	})
	mux.HandleFunc("POST /device_set.v1.DeviceSetService/SaveRack", func(w http.ResponseWriter, r *http.Request) {
		saveCount++
		http.Error(w, "device set 42 is a group, not a rack", http.StatusBadRequest)
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
	if saveCount != 1 {
		t.Fatalf("save count = %d, want 1", saveCount)
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

func TestFleetManagementSelectorRequiresExplicitScope(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantAll    bool
		wantDevice []string
		wantErr    string
	}{
		{name: "all devices", args: []string{"--all-devices"}, wantAll: true},
		{name: "explicit devices", args: []string{"--device", "miner-2", "--device", "miner-1"}, wantDevice: []string{"miner-1", "miner-2"}},
		{name: "missing scope", wantErr: "one of --device, --group-id, --group, --rack-id, or --rack is required"},
		{name: "conflicting scope", args: []string{"--all-devices", "--device", "miner-1"}, wantErr: "use either --all-devices or explicit device/group/rack selectors"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotAll bool
			var gotDevices []string
			cmd := &cli.Command{
				Name:  "selector-test",
				Flags: generatedMinerSelectorFlags(),
				Action: func(ctx context.Context, cmd *cli.Command) error {
					selector, err := generatedBuildFleetSelector(ctx, cmd, nil)
					if err != nil {
						return err
					}
					gotAll = selector.GetAllDevices() != nil
					gotDevices = selector.GetIncludeDevices().GetDeviceIdentifiers()
					return nil
				},
			}
			err := cmd.Run(context.Background(), append([]string{"selector-test"}, tt.args...))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("selector error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("selector error = %v", err)
			}
			if gotAll != tt.wantAll {
				t.Fatalf("all devices = %t, want %t", gotAll, tt.wantAll)
			}
			if strings.Join(gotDevices, ",") != strings.Join(tt.wantDevice, ",") {
				t.Fatalf("devices = %v, want %v", gotDevices, tt.wantDevice)
			}
		})
	}
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

func requestBodyMap(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	return body
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

func TestServerLogsListUsesSessionOnlyAuth(t *testing.T) {
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
	mux.HandleFunc("POST /serverlog.v1.ServerLogService/ListServerLogs", func(w http.ResponseWriter, r *http.Request) {
		listAuthHeader = r.Header.Get("Authorization")
		if cookie, err := r.Cookie("fleet_session"); err == nil {
			listCookie = cookie.Value
		}
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte(`{"logs":[]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	err := newRootCommand().Run(context.Background(), []string{
		"fleetcli", "--server", srv.URL + "/", "serverlogs", "list",
	})
	if err != nil {
		t.Fatalf("serverlogs list with env key and creds should succeed, got: %v", err)
	}
	if authBody["username"] != "admin" || authBody["password"] != "proto" {
		t.Errorf("authenticate body = %v, want env username/password", authBody)
	}
	if listAuthHeader != "" {
		t.Errorf("ListServerLogs Authorization = %q, want empty for session-only command", listAuthHeader)
	}
	if listCookie != "sess" {
		t.Errorf("ListServerLogs session cookie = %q, want %q", listCookie, "sess")
	}
}

func TestGeneratedSecretFlagsAreNotStringFlags(t *testing.T) {
	var visit func(path []string, command *cli.Command)
	visit = func(path []string, command *cli.Command) {
		path = append(path, command.Name)
		for _, flag := range command.Flags {
			name := flag.Names()[0]
			if !strings.Contains(name, "password") && !strings.Contains(name, "bearer") && !strings.Contains(name, "webhook") {
				continue
			}
			if _, ok := flag.(*cli.StringFlag); ok {
				t.Errorf("%s exposes secret flag --%s as a string", strings.Join(path, " "), name)
			}
		}
		for _, child := range command.Commands {
			visit(path, child)
		}
	}
	for _, command := range generatedCommands() {
		visit(nil, command)
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

func TestGeneratedAuthenticatedCommandReadsPasswordBeforeJSONStdin(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	var authBody map[string]any
	var listBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth.v1.AuthService/Authenticate", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&authBody); err != nil {
			t.Errorf("decode authenticate request: %v", err)
		}
		http.SetCookie(w, &http.Cookie{Name: "fleet_session", Value: "sess", Path: "/", Secure: true})
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte("{}"))
	})
	mux.HandleFunc("POST /fleetmanagement.v1.FleetManagementService/ListMinerStateSnapshots", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&listBody); err != nil {
			t.Errorf("decode miners list request: %v", err)
		}
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte("{}"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	withStdin(t, "fleet-secret\n{\"page_size\":7}\n", func() {
		err := newRootCommand().Run(context.Background(), []string{
			"fleetcli", "--server", srv.URL + "/", "--username", "admin", "--password-stdin",
			"miners", "list", "--json", "-",
		})
		if err != nil {
			t.Fatalf("miners list with password and JSON stdin error = %v", err)
		}
	})

	if authBody["username"] != "admin" || authBody["password"] != "fleet-secret" {
		t.Errorf("authenticate body = %v, want first stdin line as Fleet password", authBody)
	}
	if listBody["page_size"] != float64(7) {
		t.Errorf("miners list body = %v, want remaining stdin JSON body", listBody)
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
		"pools", "validate", "--url", "stratum+tcp://pool.example.com:3333", "--username", "pooluser",
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
	if gotBody["url"] != "stratum+tcp://pool.example.com:3333" {
		t.Errorf("request url = %v, want the pool url", gotBody["url"])
	}
}

func TestPoolsCreateRequiresCompletePoolDetails(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		http.Error(w, "unexpected request", http.StatusTeapot)
	}))
	t.Cleanup(srv.Close)

	emptyJSONPath := filepath.Join(t.TempDir(), "empty-pool.json")
	if err := os.WriteFile(emptyJSONPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write empty pool json: %v", err)
	}

	tests := []struct {
		name string
		args []string
	}{
		{name: "no inputs"},
		{name: "empty JSON", args: []string{"--json", emptyJSONPath}},
		{name: "partial flags", args: []string{"--pool-name", "incomplete"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := newRootCommand().Run(context.Background(), append([]string{
				"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
				"pools", "create",
			}, tt.args...))
			if err == nil || !strings.Contains(err.Error(), "pool_config.") {
				t.Fatalf("pools create error = %v, want missing nested pool field", err)
			}
		})
	}
	if requestCount != 0 {
		t.Fatalf("request count = %d, want no requests for incomplete pool details", requestCount)
	}
}

func TestPoolsCreateJSONPoolConfigFlagOverridePreservesFields(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	jsonPath := filepath.Join(t.TempDir(), "pool.json")
	if err := os.WriteFile(jsonPath, []byte(`{
		"pool_config": {
			"pool_name": "old-name",
			"url": "stratum+tcp://pool.example.com:3333",
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
		"url":       "stratum+tcp://pool.example.com:3333",
		"username":  "pool-user",
		"password":  "pool-pass",
	}
	for field, wantValue := range want {
		if got := poolConfig[field]; got != wantValue {
			t.Errorf("pool_config.%s = %v, want %q", field, got, wantValue)
		}
	}
}

func TestPoolsDeleteRequiresPoolID(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	err := newRootCommand().Run(context.Background(), []string{"fleetcli", "pools", "delete"})
	if err == nil || !strings.Contains(err.Error(), "pool-id") {
		t.Fatalf("pools delete without pool-id error = %v, want required pool-id error", err)
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
				"--url", "stratum+tcp://pool.example.com:3333",
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
				"--url", "stratum+tcp://pool.example.com:3333",
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

func TestPoolsReadFleetAndPoolPasswordsFromSeparateStdinLines(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	var authBody map[string]any
	var createBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth.v1.AuthService/Authenticate", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&authBody); err != nil {
			t.Errorf("decode authenticate request: %v", err)
		}
		http.SetCookie(w, &http.Cookie{Name: "fleet_session", Value: "sess", Path: "/", Secure: true})
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte("{}"))
	})
	mux.HandleFunc("POST /pools.v1.PoolsService/CreatePool", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
			t.Errorf("decode pool create request: %v", err)
		}
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte("{}"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	withStdin(t, "fleet-secret\npool-secret\n", func() {
		err := newRootCommand().Run(context.Background(), []string{
			"fleetcli", "--server", srv.URL + "/", "--username", "admin", "--password-stdin",
			"pools", "create",
			"--pool-name", "pool-name",
			"--url", "stratum+tcp://pool.example.com:3333",
			"--username", "pool-user",
			"--pool-password-stdin",
		})
		if err != nil {
			t.Fatalf("pools create with Fleet and pool password stdin error = %v", err)
		}
	})

	if authBody["username"] != "admin" || authBody["password"] != "fleet-secret" {
		t.Errorf("authenticate body = %v, want first stdin line as Fleet password", authBody)
	}
	poolConfig, _ := createBody["pool_config"].(map[string]any)
	if poolConfig["password"] != "pool-secret" {
		t.Errorf("pool create body = %v, want second stdin line as pool password", createBody)
	}
}

func TestGeneratedSecretsReadFleetPasswordBeforeCommandSecret(t *testing.T) {
	pinFleetAuthEnv(t, nil)

	var authBody map[string]any
	var channelBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth.v1.AuthService/Authenticate", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&authBody); err != nil {
			t.Errorf("decode authenticate request: %v", err)
		}
		http.SetCookie(w, &http.Cookie{Name: "fleet_session", Value: "sess", Path: "/", Secure: true})
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte("{}"))
	})
	mux.HandleFunc("POST /alerts.v1.ChannelService/CreateChannel", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&channelBody); err != nil {
			t.Errorf("decode channel create request: %v", err)
		}
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte("{}"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	withStdin(t, "fleet-secret\nsmtp-secret\n", func() {
		err := newRootCommand().Run(context.Background(), []string{
			"fleetcli", "--server", srv.URL + "/", "--username", "admin", "--password-stdin",
			"alerts", "channels", "create",
			"--name", "email",
			"--kind", "smtp",
			"--smtp-password-stdin",
		})
		if err != nil {
			t.Fatalf("channel create with Fleet and SMTP password stdin error = %v", err)
		}
	})

	if authBody["password"] != "fleet-secret" {
		t.Errorf("authenticate body = %v, want first stdin line as Fleet password", authBody)
	}
	smtp, _ := channelBody["smtp"].(map[string]any)
	if smtp["password"] != "smtp-secret" {
		t.Errorf("channel create body = %v, want second stdin line as SMTP password", channelBody)
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
