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

// TestFleetCLISubcommands exercises the broad fleetcli surface against a real
// local fleet-api, including destructive commands. The final destructive steps
// re-pair the proto simulator so the local fleet remains usable after the run.
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

	var deviceIdentifier string

	t.Run("PairingAndMiners", func(t *testing.T) {
		discovered := runFleetCLIJSON(t, ctx, env,
			"pairing", "discover",
			"--ip", protoSimDiscoveryHost,
			"--port", protoSimPort,
		)
		devices := jsonSlice(t, discovered, "devices")
		require.NotEmpty(t, devices, "pairing discover should return devices")
		deviceIdentifier = jsonStringFromMap(t, devices[0], "device_identifier")
		require.NotEmpty(t, deviceIdentifier, "discovered device should have an identifier")

		paired := runFleetCLIJSON(t, ctx, env, "pairing", "pair", "--device", deviceIdentifier)
		require.NotEmpty(t, paired, "pairing pair should return JSON")

		miners := runFleetCLIJSON(t, ctx, env, "miners", "list", "--page-size", "25")
		deviceIdentifier = fleetCLIMinerIdentifier(t, miners, deviceIdentifier)
		require.NotEmpty(t, deviceIdentifier, "miners list should expose a usable miner identifier")
	})

	t.Run("PerformanceAndNetworkInfo", func(t *testing.T) {
		perf := runFleetCLIJSON(t, ctx, env, "performance", "get", "--metric", "hashrate", "--page-size", "25")
		assert.Equal(t, "telemetry.v1.TelemetryService/GetCombinedMetrics", jsonString(t, perf, "source"))

		info := runFleetCLIJSON(t, ctx, env, "networkinfo", "get")
		require.NotEmpty(t, jsonMap(t, info, "network_info"), "networkinfo get should return network_info")

		_, err := runFleetCLI(ctx, env, "networkinfo", "set-nickname", "--network-nickname", unique)
		require.Error(t, err, "networkinfo set-nickname is currently unimplemented server-side")
		assert.Contains(t, err.Error(), "unimplemented", "set-nickname failure should come from the API")
	})

	var groupID string

	t.Run("Groups", func(t *testing.T) {
		created := runFleetCLIJSON(t, ctx, env,
			"groups", "create",
			"--label", unique+"-group",
			"--description", "fleetcli e2e group",
		)
		groupID = jsonString(t, created, "collection", "id")
		require.NotEmpty(t, groupID, "groups create should return collection.id")
		t.Cleanup(func() {
			_, _ = runFleetCLI(ctx, env, "groups", "delete", "--collection-id", groupID)
		})

		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "groups", "get", "--collection-id", groupID))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "groups", "list", "--page-size", "25"))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "groups", "members", "--collection-id", groupID))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "groups", "stats", "--collection-ids", groupID))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "groups", "device", "--device-identifier", deviceIdentifier))

		updated := runFleetCLIJSON(t, ctx, env,
			"groups", "update",
			"--collection-id", groupID,
			"--label", unique+"-group-updated",
			"--description", "fleetcli e2e group updated",
		)
		assert.Equal(t, unique+"-group-updated", jsonString(t, updated, "collection", "label"))
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
		rackID = jsonString(t, saved, "collection", "id")
		require.NotEmpty(t, rackID, "racks save should return collection.id")
		t.Cleanup(func() {
			_, _ = runFleetCLI(ctx, env, "racks", "delete", "--collection-id", rackID)
		})

		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "racks", "get", "--collection-id", rackID))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "racks", "list", "--page-size", "25"))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "racks", "members", "--collection-id", rackID))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "racks", "slots", "--collection-id", rackID))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "racks", "stats", "--collection-ids", rackID))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "racks", "device", "--device-identifier", deviceIdentifier))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "racks", "types"))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "racks", "zones"))
	})

	t.Run("Pools", func(t *testing.T) {
		poolPath := writeFleetCLITestJSON(t, t.TempDir(), "pool.json", map[string]any{
			"poolConfig": map[string]any{
				"url":      "stratum+tcp://pool.example.com:3333",
				"username": unique,
				"poolName": unique + "-pool",
			},
		})
		created := runFleetCLIJSON(t, ctx, env, "pools", "create", "--json", poolPath)
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

	t.Run("Schedule", func(t *testing.T) {
		created := runFleetCLIJSON(t, ctx, env,
			"schedule", "create",
			"--name", unique+"-schedule",
			"--action", "reboot",
			"--schedule-type", "one-time",
			"--start-date", "2099-01-01",
			"--start-time", "00:00",
			"--timezone", "UTC",
		)
		scheduleID := jsonString(t, created, "schedule", "id")
		require.NotEmpty(t, scheduleID, "schedule create should return schedule.id")
		t.Cleanup(func() {
			_, _ = runFleetCLI(ctx, env, "schedule", "delete", "--schedule-id", scheduleID)
		})

		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "schedule", "list", "--status", "active"))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "schedule", "pause", "--schedule-id", scheduleID))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "schedule", "resume", "--schedule-id", scheduleID))
		reorderArgs := []string{"schedule", "reorder"}
		for _, id := range activeFleetCLIScheduleIDs(t, runFleetCLIJSON(t, ctx, env, "schedule", "list", "--status", "active"), scheduleID) {
			reorderArgs = append(reorderArgs, "--schedule-ids", id)
		}
		require.NotNil(t, runFleetCLIJSON(t, ctx, env, reorderArgs...))

		updated := runFleetCLIJSON(t, ctx, env,
			"schedule", "update",
			"--schedule-id", scheduleID,
			"--name", unique+"-schedule-updated",
			"--action", "reboot",
			"--schedule-type", "one-time",
			"--start-date", "2099-01-02",
			"--start-time", "01:00",
			"--timezone", "UTC",
		)
		assert.Equal(t, unique+"-schedule-updated", jsonString(t, updated, "schedule", "name"))
	})

	t.Run("LowImpactMinerCommands", func(t *testing.T) {
		require.NotEmpty(t, deviceIdentifier, "deviceIdentifier must be set")

		capabilities := runFleetCLIJSON(t, ctx, env,
			"minercommand", "check-capabilities",
			"--command-type", "blink-led",
			"--device", deviceIdentifier,
		)
		require.NotEmpty(t, capabilities, "check-capabilities should return JSON")

		blink := runFleetCLIJSON(t, ctx, env, "minercommand", "blink-led", "--device", deviceIdentifier)
		blinkBatch := jsonString(t, blink, "batch_identifier")
		require.NotEmpty(t, blinkBatch, "blink-led should return a batch identifier")

		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env,
			"minercommand", "get-command-batch-device-results",
			"--batch-identifier", blinkBatch,
		))

		for _, tc := range []struct {
			name string
			args []string
		}{
			{name: "download-logs", args: []string{"minercommand", "download-logs", "--device", deviceIdentifier}},
			{name: "set-cooling-mode", args: []string{"minercommand", "set-cooling-mode", "--mode", "air-cooled", "--device", deviceIdentifier}},
			{name: "set-power-target", args: []string{"minercommand", "set-power-target", "--performance-mode", "efficiency", "--device", deviceIdentifier}},
			{name: "stop", args: []string{"minercommand", "stop", "--device", deviceIdentifier}},
			{name: "start", args: []string{"minercommand", "start", "--device", deviceIdentifier}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				resp := runFleetCLIJSON(t, ctx, env, tc.args...)
				batchID := jsonString(t, resp, "batch_identifier")
				require.NotEmpty(t, batchID, "%s should return a batch identifier", tc.name)
				waitFleetCLICommandBatchSuccess(t, ctx, env, batchID)
			})
		}

		poolsPath := writeFleetCLITestJSON(t, t.TempDir(), "miner-pools.json", map[string]any{
			"deviceSelector": map[string]any{
				"includeDevices": map[string]any{
					"deviceIdentifiers": []string{deviceIdentifier},
				},
			},
			"defaultPool": map[string]any{
				"rawPool": map[string]any{
					"url":      "stratum+tcp://pool.example.com:3333",
					"username": unique + "-worker",
					"password": "x",
				},
			},
		})
		updatePools := runFleetCLIJSON(t, ctx, env,
			"minercommand", "update-pools",
			"--json", poolsPath,
			"--user-username", testUsername,
			"--user-password", testPassword,
		)
		updatePoolsBatch := jsonString(t, updatePools, "batch_identifier")
		require.NotEmpty(t, updatePoolsBatch, "update-pools should return a batch identifier")
		waitFleetCLICommandBatchSuccess(t, ctx, env, updatePoolsBatch)

		firmwarePath := writeRandomFirmwareFile(t, t.TempDir(), "fleetcli-destructive.swu", 64*1024)
		uploaded := runFleetCLIJSON(t, ctx, env, "firmware", "upload", "--quiet", firmwarePath)
		firmwareFileID := jsonString(t, uploaded, "firmware_file_id")
		require.NotEmpty(t, firmwareFileID, "firmware upload should return firmware_file_id")
		t.Cleanup(func() {
			_, _ = runFleetCLI(ctx, env, "firmware", "delete", firmwareFileID)
		})

		firmwareUpdate := runFleetCLIJSON(t, ctx, env,
			"minercommand", "firmware-update",
			"--firmware-file-id", firmwareFileID,
			"--device", deviceIdentifier,
		)
		firmwareBatch := jsonString(t, firmwareUpdate, "batch_identifier")
		require.NotEmpty(t, firmwareBatch, "firmware-update should return a batch identifier")
		waitFleetCLICommandBatchSuccess(t, ctx, env, firmwareBatch)

		tempPassword := unique + "-miner-password"
		finalPassword := "e2e-miner-password-restored"
		updatePasswordBatch := runFleetCLIUpdateMinerPasswordWithCurrentCandidates(t, ctx, env, deviceIdentifier, tempPassword,
			finalPassword,
			"proto",
			"defaultPass123",
			"correctPassword",
			"currentPassword",
			"placeholder-current-password",
		)
		waitFleetCLICommandBatchSuccess(t, ctx, env, updatePasswordBatch)
		pairFleetCLIDevice(t, ctx, env, deviceIdentifier)

		restorePassword := runFleetCLIJSON(t, ctx, env,
			"minercommand", "update-password",
			"--device", deviceIdentifier,
			"--current-password", tempPassword,
			"--new-password", finalPassword,
			"--user-username", testUsername,
			"--user-password", testPassword,
		)
		restorePasswordBatch := jsonString(t, restorePassword, "batch_identifier")
		require.NotEmpty(t, restorePasswordBatch, "password restore should return a batch identifier")
		waitFleetCLICommandBatchSuccess(t, ctx, env, restorePasswordBatch)
		pairFleetCLIDevice(t, ctx, env, deviceIdentifier)

		unpair := runFleetCLIJSON(t, ctx, env, "minercommand", "unpair", "--device", deviceIdentifier)
		unpairBatch := jsonString(t, unpair, "batch_identifier")
		require.NotEmpty(t, unpairBatch, "unpair should return a batch identifier")
		waitFleetCLICommandBatchSuccess(t, ctx, env, unpairBatch)
		pairFleetCLIDevice(t, ctx, env, deviceIdentifier)

		reboot := runFleetCLIJSON(t, ctx, env, "minercommand", "reboot", "--device", deviceIdentifier)
		rebootBatch := jsonString(t, reboot, "batch_identifier")
		require.NotEmpty(t, rebootBatch, "reboot should return a batch identifier")
		waitFleetCLICommandBatchSuccess(t, ctx, env, rebootBatch)
	})
}

func TestFleetCLILeafCommandCoverage(t *testing.T) {
	expected := []string{
		"auth login",
		"apikey create",
		"apikey list",
		"apikey revoke",
		"pairing discover",
		"pairing pair",
		"performance get",
		"firmware config",
		"firmware check",
		"firmware upload",
		"firmware list",
		"firmware delete",
		"firmware delete-all",
		"groups create",
		"groups delete",
		"groups device",
		"groups get",
		"groups list",
		"groups members",
		"groups stats",
		"groups update",
		"minercommand blink-led",
		"minercommand check-capabilities",
		"minercommand download-logs",
		"minercommand firmware-update",
		"minercommand get-command-batch-device-results",
		"minercommand reboot",
		"minercommand set-cooling-mode",
		"minercommand set-power-target",
		"minercommand start",
		"minercommand stop",
		"minercommand unpair",
		"minercommand update-password",
		"minercommand update-pools",
		"miners list",
		"networkinfo get",
		"networkinfo set-nickname",
		"onboarding create-admin",
		"pools create",
		"pools delete",
		"pools list",
		"pools update",
		"pools validate",
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
		"schedule create",
		"schedule delete",
		"schedule list",
		"schedule pause",
		"schedule reorder",
		"schedule resume",
		"schedule update",
	}
	coverage := map[string]string{
		"auth login":                      "live: TestFleetCLISubcommands/AuthAndAPIKeys",
		"apikey create":                   "live: TestFleetCLISubcommands/AuthAndAPIKeys",
		"apikey list":                     "live: TestFleetCLISubcommands/AuthAndAPIKeys",
		"apikey revoke":                   "live: TestFleetCLISubcommands/AuthAndAPIKeys",
		"pairing discover":                "live: TestFleetCLISubcommands/PairingAndMiners",
		"pairing pair":                    "live: TestFleetCLISubcommands/PairingAndMiners",
		"performance get":                 "live: TestFleetCLISubcommands/PerformanceAndNetworkInfo",
		"firmware config":                 "live: TestFleetCLIFirmwareWorkflow",
		"firmware check":                  "live: TestFleetCLIFirmwareWorkflow",
		"firmware upload":                 "live: TestFleetCLIFirmwareWorkflow",
		"firmware list":                   "live: TestFleetCLIFirmwareWorkflow",
		"firmware delete":                 "live: TestFleetCLIFirmwareWorkflow",
		"firmware delete-all":             "live: TestFleetCLIFirmwareWorkflow",
		"groups create":                   "live: TestFleetCLISubcommands/Groups",
		"groups delete":                   "live cleanup: TestFleetCLISubcommands/Groups",
		"groups device":                   "live: TestFleetCLISubcommands/Groups",
		"groups get":                      "live: TestFleetCLISubcommands/Groups",
		"groups list":                     "live: TestFleetCLISubcommands/Groups",
		"groups members":                  "live: TestFleetCLISubcommands/Groups",
		"groups stats":                    "live: TestFleetCLISubcommands/Groups",
		"groups update":                   "live: TestFleetCLISubcommands/Groups",
		"minercommand blink-led":          "live: TestFleetCLISubcommands/LowImpactMinerCommands",
		"minercommand check-capabilities": "live: TestFleetCLISubcommands/LowImpactMinerCommands",
		"minercommand download-logs":      "live: TestFleetCLISubcommands/LowImpactMinerCommands",
		"minercommand firmware-update":    "live: TestFleetCLISubcommands/LowImpactMinerCommands",
		"minercommand get-command-batch-device-results": "live: TestFleetCLISubcommands/LowImpactMinerCommands",
		"minercommand reboot":                           "live: TestFleetCLISubcommands/LowImpactMinerCommands",
		"minercommand set-cooling-mode":                 "live: TestFleetCLISubcommands/LowImpactMinerCommands",
		"minercommand set-power-target":                 "live: TestFleetCLISubcommands/LowImpactMinerCommands",
		"minercommand start":                            "live: TestFleetCLISubcommands/LowImpactMinerCommands",
		"minercommand stop":                             "live: TestFleetCLISubcommands/LowImpactMinerCommands",
		"minercommand unpair":                           "live with re-pair: TestFleetCLISubcommands/LowImpactMinerCommands",
		"minercommand update-password":                  "live with known password restore: TestFleetCLISubcommands/LowImpactMinerCommands",
		"minercommand update-pools":                     "live: TestFleetCLISubcommands/LowImpactMinerCommands",
		"miners list":                                   "live: TestFleetCLISubcommands/PairingAndMiners",
		"networkinfo get":                               "live: TestFleetCLISubcommands/PerformanceAndNetworkInfo",
		"networkinfo set-nickname":                      "covered known API error: handler currently returns unimplemented",
		"onboarding create-admin":                       "live/rerunnable: TestFleetCLIWorkflow and TestFleetCLISubcommands bootstrap",
		"pools create":                                  "live: TestFleetCLISubcommands/Pools",
		"pools delete":                                  "live cleanup: TestFleetCLISubcommands/Pools",
		"pools list":                                    "live: TestFleetCLISubcommands/Pools",
		"pools update":                                  "live: TestFleetCLISubcommands/Pools",
		"pools validate":                                "live expected API error: unreachable test pool still confirms CLI request path",
		"racks delete":                                  "live cleanup: TestFleetCLISubcommands/Racks",
		"racks device":                                  "live: TestFleetCLISubcommands/Racks",
		"racks get":                                     "live: TestFleetCLISubcommands/Racks",
		"racks list":                                    "live: TestFleetCLISubcommands/Racks",
		"racks members":                                 "live: TestFleetCLISubcommands/Racks",
		"racks save":                                    "live: TestFleetCLISubcommands/Racks",
		"racks slots":                                   "live: TestFleetCLISubcommands/Racks",
		"racks stats":                                   "live: TestFleetCLISubcommands/Racks",
		"racks types":                                   "live: TestFleetCLISubcommands/Racks",
		"racks zones":                                   "live: TestFleetCLISubcommands/Racks",
		"schedule create":                               "live: TestFleetCLISubcommands/Schedule",
		"schedule delete":                               "live cleanup: TestFleetCLISubcommands/Schedule",
		"schedule list":                                 "live: TestFleetCLISubcommands/Schedule",
		"schedule pause":                                "live: TestFleetCLISubcommands/Schedule",
		"schedule reorder":                              "live: TestFleetCLISubcommands/Schedule",
		"schedule resume":                               "live: TestFleetCLISubcommands/Schedule",
		"schedule update":                               "live: TestFleetCLISubcommands/Schedule",
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

	if _, err := runFleetCLI(ctx, env,
		"onboarding", "create-admin",
		"--username", testUsername,
		"--password", testPassword,
	); err != nil {
		require.Truef(t, isAlreadyOnboardedError(err),
			"create-admin failed for a reason other than existing onboarding: %v", err)
	}
}

func runFleetCLIJSON(t *testing.T, ctx context.Context, env []string, args ...string) map[string]any {
	t.Helper()

	output, err := runFleetCLI(ctx, env, args...)
	require.NoErrorf(t, err, "fleetcli %s should succeed", strings.Join(args, " "))

	var decoded map[string]any
	require.NoErrorf(t, json.Unmarshal([]byte(output), &decoded),
		"fleetcli %s output should be JSON: %s", strings.Join(args, " "), output)
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

	items := jsonSlice(t, miners, "miners")
	require.NotEmpty(t, items, "miners list should return at least one miner")

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

func waitFleetCLICommandBatchSuccess(t *testing.T, ctx context.Context, env []string, batchID string) map[string]any {
	t.Helper()

	deadline := time.Now().Add(90 * time.Second)
	var last map[string]any
	for time.Now().Before(deadline) {
		last = runFleetCLIJSON(t, ctx, env,
			"minercommand", "get-command-batch-device-results",
			"--batch-identifier", batchID,
		)
		if jsonString(t, last, "status") == "finished" {
			require.Positive(t, jsonInt(t, last, "success_count"), "command batch %s should have successes: %#v", batchID, last)
			require.Zero(t, jsonInt(t, last, "failure_count"), "command batch %s should not have failures: %#v", batchID, last)
			return last
		}
		time.Sleep(2 * time.Second)
	}

	require.Failf(t, "command batch did not finish", "batch %s last response: %#v", batchID, last)
	return nil
}

func waitFleetCLICommandBatchFinished(t *testing.T, ctx context.Context, env []string, batchID string) map[string]any {
	t.Helper()

	deadline := time.Now().Add(90 * time.Second)
	var last map[string]any
	for time.Now().Before(deadline) {
		last = runFleetCLIJSON(t, ctx, env,
			"minercommand", "get-command-batch-device-results",
			"--batch-identifier", batchID,
		)
		if jsonString(t, last, "status") == "finished" {
			return last
		}
		time.Sleep(2 * time.Second)
	}

	require.Failf(t, "command batch did not finish", "batch %s last response: %#v", batchID, last)
	return nil
}

func runFleetCLIUpdateMinerPasswordWithCurrentCandidates(
	t *testing.T,
	ctx context.Context,
	env []string,
	deviceIdentifier string,
	newPassword string,
	candidates ...string,
) string {
	t.Helper()

	var attempts []string
	for _, currentPassword := range candidates {
		resp := runFleetCLIJSON(t, ctx, env,
			"minercommand", "update-password",
			"--device", deviceIdentifier,
			"--current-password", currentPassword,
			"--new-password", newPassword,
			"--user-username", testUsername,
			"--user-password", testPassword,
		)
		batchID := jsonString(t, resp, "batch_identifier")
		result := waitFleetCLICommandBatchFinished(t, ctx, env, batchID)
		if jsonInt(t, result, "success_count") > 0 && jsonInt(t, result, "failure_count") == 0 {
			return batchID
		}
		attempts = append(attempts, fmt.Sprintf("%s => %#v", currentPassword, result))
	}

	require.Failf(t, "no current miner password candidate worked", "%s", strings.Join(attempts, "\n"))
	return ""
}

func pairFleetCLIDevice(t *testing.T, ctx context.Context, env []string, deviceIdentifier string) {
	t.Helper()

	resp := runFleetCLIJSON(t, ctx, env, "pairing", "pair", "--device", deviceIdentifier)
	if failed, ok := resp["failed_device_ids"].([]any); ok {
		require.Empty(t, failed, "pairing should not report failed devices")
	}
}

func activeFleetCLIScheduleIDs(t *testing.T, schedules map[string]any, requiredID string) []string {
	t.Helper()

	items := jsonSlice(t, schedules, "schedules")
	ids := make([]string, 0, len(items)+1)
	foundRequired := false
	for _, item := range items {
		schedule, ok := item.(map[string]any)
		require.Truef(t, ok, "schedule entry should be an object, got %T", item)
		id := jsonStringValue(t, schedule["id"], "schedules[].id")
		if id == requiredID {
			foundRequired = true
		}
		ids = append(ids, id)
	}
	if !foundRequired {
		ids = append(ids, requiredID)
	}
	return ids
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
