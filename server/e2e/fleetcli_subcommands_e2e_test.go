//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFleetCLISubcommands exercises the fleetcli V0 control-plane surface
// against a real local fleet-api.
func TestFleetCLISubcommands(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	ctx := context.Background()

	t.Log("Waiting for fleet-api to be ready...")
	waitForFleetAPIHealth(t, ctx, 60*time.Second)

	t.Log("Building fleetcli test binary...")
	buildFleetCLIBinary(t)

	env := []string{
		"FLEET_SERVER=" + fleetAPIURL + "/",
		"FLEET_USERNAME=" + testUsername,
		"FLEET_PASSWORD=" + testPassword,
		"NO_COLOR=1",
	}
	ensureFleetCLIAdmin(t, ctx, env)
	deviceIdentifier := ensureFleetCLIPairedMiner(t, ctx, env)

	unique := fmt.Sprintf("e2e-cli-%d", time.Now().UnixNano())

	t.Run("AuthAndAPIKeys", func(t *testing.T) {
		login := runFleetCLIJSON(t, ctx, env, "auth", "login")
		require.NotEmpty(t, jsonMap(t, login, "user_info"), "auth login should return user_info")

		created := runFleetCLIJSON(t, ctx, env, "apikey", "create", "--name", unique+"-key")
		keyID := jsonString(t, created, "info", "key_id")
		require.NotEmpty(t, keyID, "apikey create should return info.key_id")

		listed := runFleetCLIJSON(t, ctx, env, "apikey", "list")
		require.NotEmpty(t, listed, "apikey list should return JSON")

		revoked := runFleetCLIJSON(t, ctx, env, "apikey", "revoke", "--key-id", keyID)
		require.NotNil(t, revoked, "apikey revoke should return JSON")
	})

	t.Run("Miners", func(t *testing.T) {
		miners := runFleetCLIJSON(t, ctx, env, "miners", "list", "--page-size", "25")
		assert.Equal(t, deviceIdentifier, fleetCLIMinerIdentifier(t, miners, deviceIdentifier))
		require.NotEmpty(t, deviceIdentifier, "miners list should expose a usable miner identifier")
	})

	t.Run("Performance", func(t *testing.T) {
		perf := runFleetCLIJSON(t, ctx, env, "performance", "get", "--metric", "hashrate", "--page-size", "25")
		assert.Equal(t, "telemetry.v1.TelemetryService/GetCombinedMetrics", jsonString(t, perf, "source"))
	})

	var groupID string

	t.Run("Groups", func(t *testing.T) {
		created := runFleetCLIJSON(t, ctx, env,
			"groups", "create",
			"--label", unique+"-group",
			"--description", "fleetcli e2e group",
		)
		groupID = jsonString(t, created, "device_set", "id")
		require.NotEmpty(t, groupID, "groups create should return device_set.id")
		t.Cleanup(func() {
			_, _ = runFleetCLI(ctx, env, "groups", "delete", "--device-set-id", groupID)
		})

		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "groups", "get", "--device-set-id", groupID))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "groups", "list", "--page-size", "25"))
		added := runFleetCLIJSON(t, ctx, env,
			"groups", "add-devices",
			"--target-group-id", groupID,
			"--device", deviceIdentifier,
		)
		assert.Equal(t, 1, jsonInt(t, added, "added_count"))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "groups", "members", "--device-set-id", groupID))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "groups", "stats", "--device-set-ids", groupID))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "groups", "device", "--device-identifier", deviceIdentifier))
		removed := runFleetCLIJSON(t, ctx, env,
			"groups", "remove-devices",
			"--target-group-id", groupID,
			"--device", deviceIdentifier,
		)
		assert.Equal(t, 1, jsonInt(t, removed, "removed_count"))

		updated := runFleetCLIJSON(t, ctx, env,
			"groups", "update",
			"--device-set-id", groupID,
			"--label", unique+"-group-updated",
			"--description", "fleetcli e2e group updated",
		)
		assert.Equal(t, unique+"-group-updated", jsonString(t, updated, "device_set", "label"))
	})

	var rackID string

	t.Run("Racks", func(t *testing.T) {
		rackPath := writeFleetCLITestJSON(t, t.TempDir(), "rack.json", map[string]any{
			"label": unique + "-rack",
			"rackInfo": map[string]any{
				"rows":        1,
				"columns":     1,
				"orderIndex":  "RACK_ORDER_INDEX_BOTTOM_LEFT",
				"coolingType": "RACK_COOLING_TYPE_AIR",
			},
			"deviceSelector": map[string]any{
				"deviceList": map[string]any{
					"deviceIdentifiers": []string{},
				},
			},
		})
		saved := runFleetCLIJSON(t, ctx, env, "racks", "save", "--json", rackPath)
		rackID = jsonString(t, saved, "device_set", "id")
		require.NotEmpty(t, rackID, "racks save should return device_set.id")
		t.Cleanup(func() {
			_, _ = runFleetCLI(ctx, env, "racks", "delete", "--device-set-id", rackID)
		})

		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "racks", "get", "--device-set-id", rackID))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "racks", "list", "--page-size", "25"))
		added := runFleetCLIJSON(t, ctx, env,
			"racks", "add-devices",
			"--target-rack-id", rackID,
			"--device", deviceIdentifier,
		)
		assert.Equal(t, 1, jsonInt(t, added, "assigned_count"))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "racks", "members", "--device-set-id", rackID))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "racks", "slots", "--device-set-id", rackID))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "racks", "stats", "--device-set-ids", rackID))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "racks", "device", "--device-identifier", deviceIdentifier))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "racks", "types"))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "racks", "zones"))
	})

	t.Run("Pools", func(t *testing.T) {
		createOutput, createErr := runFleetCLIWithInput(ctx, env, "x\n",
			"pools", "create",
			"--pool-name", unique+"-pool",
			"--url", "stratum+tcp://pool.example.com:3333",
			"--username", unique,
			"--pool-password-stdin",
		)
		require.NoError(t, createErr, "pools create should accept the stdin-backed pool password flag")
		created := parseFleetCLIJSON(t, createOutput)
		poolID := jsonString(t, created, "pool", "pool_id")
		require.NotEmpty(t, poolID, "pools create should return pool.pool_id")
		t.Cleanup(func() {
			_, _ = runFleetCLI(ctx, env, "pools", "delete", "--pool-id", poolID)
		})

		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "pools", "list"))
		updated := runFleetCLIJSON(t, ctx, env,
			"pools", "update",
			"--pool-id", poolID,
			"--pool-name", unique+"-pool-updated",
			"--url", "stratum+tcp://pool2.example.com:3333",
			"--username", unique+"-worker",
		)
		assert.Equal(t, unique+"-pool-updated", jsonString(t, updated, "pool", "pool_name"))

		_, err := runFleetCLI(ctx, env,
			"pools", "validate",
			"--url", "stratum+tcp://pool.example.com:3333",
			"--username", unique,
		)
		require.Error(t, err, "pool validation should reach the server and report the unreachable test pool")
		assert.Contains(t, err.Error(), "ValidatePool returned", "validate failure should be an API response, not CLI parsing")
	})

	t.Run("FirmwareDeploy", func(t *testing.T) {
		require.NotEmpty(t, deviceIdentifier, "deviceIdentifier must be set")

		firmwarePath := writeRandomFirmwareFile(t, t.TempDir(), "fleetcli-destructive.swu", 64*1024)
		uploaded := runFleetCLIJSON(t, ctx, env, "firmware", "upload", "--quiet", firmwarePath)
		firmwareFileID := jsonString(t, uploaded, "firmware_file_id")
		require.NotEmpty(t, firmwareFileID, "firmware upload should return firmware_file_id")
		t.Cleanup(func() {
			_, _ = runFleetCLI(ctx, env, "firmware", "delete", firmwareFileID)
		})

		firmwareUpdate := runFleetCLIJSON(t, ctx, env,
			"firmware", "deploy",
			"--firmware-file-id", firmwareFileID,
			"--device", deviceIdentifier,
		)
		firmwareBatch := jsonString(t, firmwareUpdate, "batch_identifier")
		require.NotEmpty(t, firmwareBatch, "firmware deploy should return a batch identifier")
	})
}

func TestFleetCLILeafCommandCoverage(t *testing.T) {
	expected := []string{
		"auth login",
		"apikey create",
		"apikey list",
		"apikey revoke",
		"performance get",
		"firmware config",
		"firmware check",
		"firmware upload",
		"firmware list",
		"firmware delete",
		"firmware delete-all",
		"firmware deploy",
		"groups add-devices",
		"groups create",
		"groups delete",
		"groups device",
		"groups get",
		"groups list",
		"groups members",
		"groups remove-devices",
		"groups stats",
		"groups update",
		"miners list",
		"onboarding create-admin",
		"pools create",
		"pools delete",
		"pools list",
		"pools update",
		"pools validate",
		"racks add-devices",
		"racks delete",
		"racks device",
		"racks get",
		"racks list",
		"racks members",
		"racks save",
		"racks slots",
		"racks stats",
		"racks types",
		"racks zones",
	}
	coverage := map[string]string{
		"auth login":              "live: TestFleetCLISubcommands/AuthAndAPIKeys",
		"apikey create":           "live: TestFleetCLISubcommands/AuthAndAPIKeys",
		"apikey list":             "live: TestFleetCLISubcommands/AuthAndAPIKeys",
		"apikey revoke":           "live: TestFleetCLISubcommands/AuthAndAPIKeys",
		"performance get":         "live: TestFleetCLISubcommands/Performance",
		"firmware config":         "live: TestFleetCLIFirmwareWorkflow",
		"firmware check":          "live: TestFleetCLIFirmwareWorkflow",
		"firmware upload":         "live: TestFleetCLIFirmwareWorkflow",
		"firmware list":           "live: TestFleetCLIFirmwareWorkflow",
		"firmware delete":         "live: TestFleetCLIFirmwareWorkflow",
		"firmware delete-all":     "live: TestFleetCLIFirmwareWorkflow",
		"firmware deploy":         "live destructive: TestFleetCLISubcommands/FirmwareDeploy",
		"groups add-devices":      "live: TestFleetCLISubcommands/Groups",
		"groups create":           "live: TestFleetCLISubcommands/Groups",
		"groups delete":           "live cleanup: TestFleetCLISubcommands/Groups",
		"groups device":           "live: TestFleetCLISubcommands/Groups",
		"groups get":              "live: TestFleetCLISubcommands/Groups",
		"groups list":             "live: TestFleetCLISubcommands/Groups",
		"groups members":          "live: TestFleetCLISubcommands/Groups",
		"groups remove-devices":   "live: TestFleetCLISubcommands/Groups",
		"groups stats":            "live: TestFleetCLISubcommands/Groups",
		"groups update":           "live: TestFleetCLISubcommands/Groups",
		"miners list":             "live: TestFleetCLISubcommands/Miners",
		"onboarding create-admin": "live/rerunnable: TestFleetCLIWorkflow and TestFleetCLISubcommands bootstrap",
		"pools create":            "live: TestFleetCLISubcommands/Pools",
		"pools delete":            "live cleanup: TestFleetCLISubcommands/Pools",
		"pools list":              "live: TestFleetCLISubcommands/Pools",
		"pools update":            "live: TestFleetCLISubcommands/Pools",
		"pools validate":          "live expected API error: unreachable test pool still confirms CLI request path",
		"racks add-devices":       "live: TestFleetCLISubcommands/Racks",
		"racks delete":            "live cleanup: TestFleetCLISubcommands/Racks",
		"racks device":            "live: TestFleetCLISubcommands/Racks",
		"racks get":               "live: TestFleetCLISubcommands/Racks",
		"racks list":              "live: TestFleetCLISubcommands/Racks",
		"racks members":           "live: TestFleetCLISubcommands/Racks",
		"racks save":              "live: TestFleetCLISubcommands/Racks",
		"racks slots":             "live: TestFleetCLISubcommands/Racks",
		"racks stats":             "live: TestFleetCLISubcommands/Racks",
		"racks types":             "live: TestFleetCLISubcommands/Racks",
		"racks zones":             "live: TestFleetCLISubcommands/Racks",
	}

	for _, command := range expected {
		status, ok := coverage[command]
		require.Truef(t, ok, "missing fleetcli coverage status for %q", command)
		require.NotEmptyf(t, status, "empty fleetcli coverage status for %q", command)
	}
	for command := range coverage {
		assert.Contains(t, expected, command, "coverage status references an unknown fleetcli command")
	}
}

func ensureFleetCLIAdmin(t *testing.T, ctx context.Context, env []string) {
	t.Helper()

	output, err := runFleetCLIWithInput(ctx, env, testPassword+"\n",
		"onboarding", "create-admin",
		"--username", testUsername,
		"--password-stdin",
	)
	if err == nil {
		assert.Contains(t, output, "user_id", "create-admin output should include the new user id")
		t.Log("✓ Admin user created")
		return
	}

	require.Truef(t, isAlreadyOnboardedError(err),
		"create-admin failed for a reason other than existing onboarding: %v", err)

	if _, loginErr := runFleetCLI(ctx, env, "auth", "login"); loginErr != nil {
		require.NoErrorf(t, loginErr,
			"Fleet is already onboarded, but fleetcli cannot authenticate with the e2e credentials %s/%s. Reset the stack or run these tests with matching FLEET_USERNAME/FLEET_PASSWORD.",
			testUsername, testPassword)
	}
	t.Log("Fleet already onboarded and e2e credentials are valid")
}

func ensureFleetCLIPairedMiner(t *testing.T, ctx context.Context, env []string) string {
	t.Helper()

	token := authenticateViaRealAPI(t, ctx, testUsername, testPassword)
	devices := discoverDeviceViaRealAPI(t, ctx, token, protoSimDiscoveryHost, protoSimPort)
	require.NotEmpty(t, devices, "discover should find proto-sim before fleetcli miner commands run")

	deviceIdentifier := devices[0].DeviceIdentifier
	require.NotEmpty(t, deviceIdentifier, "discovered proto-sim device should have an identifier")
	pairDeviceViaRealAPI(t, ctx, token, deviceIdentifier)

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		miners := runFleetCLIJSON(t, ctx, env, "miners", "list", "--page-size", "25")
		if got := fleetCLIMinerIdentifierOrEmpty(t, miners, deviceIdentifier); got == deviceIdentifier {
			return deviceIdentifier
		}
		time.Sleep(2 * time.Second)
	}
	require.Failf(t, "paired miner did not appear in fleetcli miners list", "device_identifier=%s", deviceIdentifier)
	return ""
}

func runFleetCLIJSON(t *testing.T, ctx context.Context, env []string, args ...string) map[string]any {
	t.Helper()

	output, err := runFleetCLI(ctx, env, args...)
	require.NoErrorf(t, err, "fleetcli %s should succeed", strings.Join(args, " "))
	return parseFleetCLIJSON(t, output)
}

func parseFleetCLIJSON(t *testing.T, output string) map[string]any {
	t.Helper()

	var decoded map[string]any
	require.NoErrorf(t, json.Unmarshal([]byte(output), &decoded), "fleetcli output should be JSON: %s", output)
	return decoded
}

func writeFleetCLITestJSON(t *testing.T, dir, name string, value any) string {
	t.Helper()

	data, err := json.Marshal(value)
	require.NoError(t, err)

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

func jsonMap(t *testing.T, root map[string]any, path ...string) map[string]any {
	t.Helper()

	value := jsonPath(t, root, path...)
	result, ok := value.(map[string]any)
	require.Truef(t, ok, "JSON path %s should be an object, got %T", strings.Join(path, "."), value)
	return result
}

func jsonSlice(t *testing.T, root map[string]any, path ...string) []any {
	t.Helper()

	value := jsonPath(t, root, path...)
	result, ok := value.([]any)
	require.Truef(t, ok, "JSON path %s should be an array, got %T", strings.Join(path, "."), value)
	return result
}

func jsonString(t *testing.T, root map[string]any, path ...string) string {
	t.Helper()

	return jsonStringValue(t, jsonPath(t, root, path...), strings.Join(path, "."))
}

func jsonStringFromMap(t *testing.T, root any, path ...string) string {
	t.Helper()

	current, ok := root.(map[string]any)
	require.Truef(t, ok, "JSON root should be an object, got %T", root)
	return jsonString(t, current, path...)
}

func jsonPath(t *testing.T, root map[string]any, path ...string) any {
	t.Helper()

	var current any = root
	for _, part := range path {
		object, ok := current.(map[string]any)
		require.Truef(t, ok, "JSON path before %q should be an object, got %T", part, current)
		current, ok = object[part]
		require.Truef(t, ok, "missing JSON path %s", strings.Join(path, "."))
	}
	return current
}

func jsonStringValue(t *testing.T, value any, label string) string {
	t.Helper()

	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	default:
		require.Failf(t, "unexpected JSON value type", "%s should be string-like, got %T", label, value)
		return ""
	}
}

func fleetCLIMinerIdentifier(t *testing.T, miners map[string]any, preferred string) string {
	t.Helper()

	id := fleetCLIMinerIdentifierOrEmpty(t, miners, preferred)
	require.NotEmpty(t, id, "miners list should return at least one miner")
	return id
}

func fleetCLIMinerIdentifierOrEmpty(t *testing.T, miners map[string]any, preferred string) string {
	t.Helper()

	items := jsonSlice(t, miners, "miners")
	if len(items) == 0 {
		return ""
	}

	var fallback string
	for _, item := range items {
		miner, ok := item.(map[string]any)
		require.Truef(t, ok, "miner entry should be an object, got %T", item)

		id := jsonStringValue(t, miner["device_identifier"], "miners[].device_identifier")
		if id == preferred {
			return id
		}
		driver, _ := miner["driver_name"].(string)
		if fallback == "" && driver == "proto" {
			fallback = id
		}
	}
	if fallback != "" {
		return fallback
	}
	return jsonStringFromMap(t, items[0], "device_identifier")
}

func jsonInt(t *testing.T, root map[string]any, path ...string) int {
	t.Helper()

	value := jsonPath(t, root, path...)
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case string:
		parsed, err := strconv.Atoi(typed)
		require.NoErrorf(t, err, "JSON path %s should parse as int", strings.Join(path, "."))
		return parsed
	default:
		require.Failf(t, "unexpected JSON value type", "%s should be int-like, got %T", strings.Join(path, "."), value)
		return 0
	}
}
