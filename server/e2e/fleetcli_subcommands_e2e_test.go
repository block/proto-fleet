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
		require.NotNil(t, runFleetCLIJSON(t, ctx, env, "miners", "model-groups"))
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

	t.Run("SitesAndBuildings", func(t *testing.T) {
		createdSite := runFleetCLIJSON(t, ctx, env,
			"sites", "create",
			"--name", unique+"-site",
			"--location-city", "Toronto",
			"--location-state", "Ontario",
			"--timezone", "America/Toronto",
		)
		siteID := jsonString(t, createdSite, "site", "id")
		require.NotEmpty(t, siteID, "sites create should return site.id")
		buildingID := ""
		t.Cleanup(func() {
			if buildingID != "" {
				_, _ = runFleetCLI(ctx, env, "buildings", "delete", "--id", buildingID)
			}
			if siteID != "" {
				_, _ = runFleetCLI(ctx, env, "sites", "delete", "--id", siteID)
			}
		})

		createdBuilding := runFleetCLIJSON(t, ctx, env,
			"buildings", "create",
			"--site-id", siteID,
			"--name", unique+"-building",
		)
		buildingID = jsonString(t, createdBuilding, "building", "id")
		require.NotEmpty(t, buildingID, "buildings create should return building.id")

		updatedBuilding := runFleetCLIJSON(t, ctx, env,
			"buildings", "update",
			"--id", buildingID,
			"--name", unique+"-building-updated",
		)
		assert.Equal(t, unique+"-building-updated", jsonString(t, updatedBuilding, "building", "name"))
		updatedSite := runFleetCLIJSON(t, ctx, env,
			"sites", "update",
			"--id", siteID,
			"--name", unique+"-site-updated",
		)
		assert.Equal(t, unique+"-site-updated", jsonString(t, updatedSite, "site", "name"))

		require.NotNil(t, runFleetCLIJSON(t, ctx, env, "buildings", "delete", "--id", buildingID))
		buildingID = ""
		require.NotNil(t, runFleetCLIJSON(t, ctx, env, "sites", "delete", "--id", siteID))
		siteID = ""
	})

	t.Run("Schedules", func(t *testing.T) {
		createPath := writeFleetCLITestJSON(t, t.TempDir(), "schedule.json", map[string]any{
			"name":         unique + "-schedule",
			"action":       "SCHEDULE_ACTION_REBOOT",
			"scheduleType": "SCHEDULE_TYPE_ONE_TIME",
			"startDate":    "2030-01-01",
			"startTime":    "12:00",
			"timezone":     "America/Toronto",
		})
		created := runFleetCLIJSON(t, ctx, env,
			"schedules", "create",
			"--json", createPath,
		)
		scheduleID := jsonString(t, created, "schedule", "id")
		require.NotEmpty(t, scheduleID, "schedules create should return schedule.id")
		t.Cleanup(func() {
			if scheduleID != "" {
				_, _ = runFleetCLI(ctx, env, "schedules", "delete", "--schedule-id", scheduleID)
			}
		})

		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "schedules", "list"))
		require.NotNil(t, runFleetCLIJSON(t, ctx, env, "schedules", "pause", "--schedule-id", scheduleID))
		require.NotNil(t, runFleetCLIJSON(t, ctx, env, "schedules", "resume", "--schedule-id", scheduleID))
		updated := runFleetCLIJSON(t, ctx, env,
			"schedules", "update",
			"--schedule-id", scheduleID,
			"--name", unique+"-schedule-updated",
			"--action", "reboot",
			"--schedule-type", "one-time",
			"--start-date", "2030-01-02",
			"--start-time", "12:30",
			"--timezone", "America/Toronto",
		)
		assert.Equal(t, unique+"-schedule-updated", jsonString(t, updated, "schedule", "name"))
		require.NotNil(t, runFleetCLIJSON(t, ctx, env, "schedules", "reorder", "--schedule-ids", scheduleID))
		require.NotNil(t, runFleetCLIJSON(t, ctx, env, "schedules", "delete", "--schedule-id", scheduleID))
		scheduleID = ""
	})

	t.Run("Roles", func(t *testing.T) {
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "roles", "permissions"))
		created := runFleetCLIJSON(t, ctx, env,
			"roles", "create",
			"--name", unique+"-role",
			"--description", "fleetcli e2e role",
		)
		roleID := jsonString(t, created, "role", "role_id")
		require.NotEmpty(t, roleID, "roles create should return role.role_id")
		t.Cleanup(func() {
			if roleID != "" {
				_, _ = runFleetCLI(ctx, env, "roles", "delete", "--role-id", roleID)
			}
		})

		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "roles", "list"))
		updated := runFleetCLIJSON(t, ctx, env,
			"roles", "update",
			"--role-id", roleID,
			"--name", unique+"-role-updated",
			"--description", "fleetcli e2e role updated",
		)
		assert.Equal(t, unique+"-role-updated", jsonString(t, updated, "role", "name"))
		require.NotNil(t, runFleetCLIJSON(t, ctx, env, "roles", "delete", "--role-id", roleID))
		roleID = ""
	})

	t.Run("CurtailmentProfiles", func(t *testing.T) {
		created := runFleetCLIJSON(t, ctx, env,
			"curtailment", "profiles", "create",
			"--profile-name", unique+"-profile",
			"--mode", "full-fleet",
		)
		profileID := jsonString(t, created, "profile", "profile_id")
		require.NotEmpty(t, profileID, "curtailment profile create should return profile.profile_id")
		t.Cleanup(func() {
			if profileID != "" {
				_, _ = runFleetCLI(ctx, env, "curtailment", "profiles", "delete", "--profile-id", profileID)
			}
		})

		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "curtailment", "profiles", "list"))
		require.NotEmpty(t, runFleetCLIJSON(t, ctx, env, "curtailment", "profiles", "get", "--profile-id", profileID))
		updated := runFleetCLIJSON(t, ctx, env,
			"curtailment", "profiles", "update",
			"--profile-id", profileID,
			"--profile-name", unique+"-profile-updated",
			"--mode", "full-fleet",
		)
		assert.Equal(t, unique+"-profile-updated", jsonString(t, updated, "profile", "profile_name"))
		require.NotNil(t, runFleetCLIJSON(t, ctx, env, "curtailment", "profiles", "delete", "--profile-id", profileID))
		profileID = ""
	})

	t.Run("FirmwareDeploy", func(t *testing.T) {
		require.NotEmpty(t, deviceIdentifier, "deviceIdentifier must be set")

		firmwarePath := writeRandomFirmwareFile(t, t.TempDir(), "fleetcli-destructive.swu", 64*1024)
		uploaded := runFleetCLIJSON(t, ctx, env, "firmware", "upload", "--quiet", "--target-manufacturer", firmwareE2ETargetManufacturer, "--target-model", firmwareE2ETargetModel, firmwarePath)
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
	manifestData, err := os.ReadFile(filepath.Join("..", "tools", "generate-fleet-cli", "commands.json"))
	require.NoError(t, err)
	var manifest struct {
		Commands []struct {
			Group    string `json:"group"`
			Subgroup string `json:"subgroup"`
			Command  string `json:"command"`
		} `json:"commands"`
	}
	require.NoError(t, json.Unmarshal(manifestData, &manifest))

	coverage := map[string]string{}
	for _, command := range manifest.Commands {
		parts := []string{command.Group}
		if command.Subgroup != "" {
			parts = append(parts, command.Subgroup)
		}
		parts = append(parts, command.Command)
		path := strings.Join(parts, " ")
		require.NotContains(t, coverage, path, "duplicate generated fleetcli path")
		coverage[path] = "help/manifest coverage: generated protobuf command"
	}

	manualCoverage := map[string]string{
		"auth login":          "live: TestFleetCLISubcommands/AuthAndAPIKeys",
		"auth audit-info":     "help/manual coverage: session-only current-user audit info",
		"auth users":          "help/manual coverage: session-only user listing",
		"apikey create":       "live: TestFleetCLISubcommands/AuthAndAPIKeys",
		"apikey list":         "live: TestFleetCLISubcommands/AuthAndAPIKeys",
		"apikey revoke":       "live: TestFleetCLISubcommands/AuthAndAPIKeys",
		"performance get":     "live: TestFleetCLISubcommands/Performance",
		"firmware config":     "live: TestFleetCLIFirmwareWorkflow",
		"firmware check":      "live: TestFleetCLIFirmwareWorkflow",
		"firmware upload":     "live: TestFleetCLIFirmwareWorkflow",
		"firmware list":       "live: TestFleetCLIFirmwareWorkflow",
		"firmware delete":     "live: TestFleetCLIFirmwareWorkflow",
		"firmware delete-all": "live: TestFleetCLIFirmwareWorkflow",
		"firmware deploy":     "live destructive: TestFleetCLISubcommands/FirmwareDeploy",
	}
	for path, status := range manualCoverage {
		require.NotContains(t, coverage, path, "manual command collides with generated command")
		coverage[path] = status
	}

	require.Len(t, manifest.Commands, 115)
	require.Len(t, manualCoverage, 14)
	require.Len(t, coverage, 129)
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

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		miners := runFleetCLIJSON(t, ctx, env, "miners", "list", "--page-size", "1000")
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
