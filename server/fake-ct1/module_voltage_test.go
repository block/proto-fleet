package main

import (
	"context"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2/mdkv2connect"
)

func newModuleVoltageClient(t *testing.T) (mdkv2connect.MiningServiceClient, *ContainerState) {
	t.Helper()
	srv, state := newConnectTestServer(t)
	return mdkv2connect.NewMiningServiceClient(http.DefaultClient, srv.URL), state
}

func setBoardState(state *ContainerState, moduleID string, slot int, boardState string) string {
	state.mu.Lock()
	defer state.mu.Unlock()
	board := state.modulesByID[moduleID].Slots[slot].Board
	board.State = boardState
	return board.MAC
}

func listVoltage(t *testing.T, client mdkv2connect.MiningServiceClient, moduleID string) *mdk.ModuleVoltageSetting {
	t.Helper()
	resp, err := client.ListModuleVoltageSettings(context.Background(), connect.NewRequest(&mdk.ListModuleVoltageSettingsRequest{}))
	if err != nil {
		t.Fatalf("ListModuleVoltageSettings: %v", err)
	}
	for _, setting := range resp.Msg.GetSettings() {
		if setting.GetModuleId() == moduleID {
			return setting
		}
	}
	t.Fatalf("module %s not listed", moduleID)
	return nil
}

func TestModuleVoltageListReportsAutomaticModeAndRange(t *testing.T) {
	client, _ := newModuleVoltageClient(t)
	setting := listVoltage(t, client, "T1-M01")
	if setting.FixedMillivolts != nil {
		t.Errorf("fixed = %d, want absent (automatic)", setting.GetFixedMillivolts())
	}
	if setting.GetReadiness() != mdk.ModuleVoltageReadiness_MODULE_VOLTAGE_READINESS_READY ||
		setting.GetSupportedMinimumMillivolts() != 26000 || setting.GetSupportedMaximumMillivolts() != 30000 ||
		setting.GetBoardCount() != 3 {
		t.Errorf("setting = %v, want ready with 3 boards and 26000-30000 mV", setting)
	}
	if setting.GetUpdatedBy() != "" || setting.GetUpdatedAt() != nil {
		t.Errorf("never-changed setting reports updatedBy=%q updatedAt=%v", setting.GetUpdatedBy(), setting.GetUpdatedAt())
	}
}

func TestModuleVoltageSetAndClearRecordActorAndReadBack(t *testing.T) {
	client, _ := newModuleVoltageClient(t)
	ctx := context.Background()

	set := connect.NewRequest(&mdk.SetModuleFixedVoltageRequest{ModuleId: "T1-M01", Millivolts: 28000})
	set.Header().Set(actorHeader, "proto-os")
	resp, err := client.SetModuleFixedVoltage(ctx, set)
	if err != nil {
		t.Fatalf("SetModuleFixedVoltage: %v", err)
	}
	if resp.Msg.GetBoardsUpdated() != 3 || len(resp.Msg.GetResults()) != 3 {
		t.Errorf("boardsUpdated = %d, results = %d, want 3 and 3", resp.Msg.GetBoardsUpdated(), len(resp.Msg.GetResults()))
	}
	setting := listVoltage(t, client, "T1-M01")
	if setting.FixedMillivolts == nil || setting.GetFixedMillivolts() != 28000 || setting.GetUpdatedBy() != "proto-os" || setting.GetUpdatedAt() == nil {
		t.Errorf("after set = %v, want fixed 28000 mV by proto-os", setting)
	}

	if _, err := client.ClearModuleFixedVoltage(ctx, connect.NewRequest(&mdk.ClearModuleFixedVoltageRequest{ModuleId: "T1-M01"})); err != nil {
		t.Fatalf("ClearModuleFixedVoltage: %v", err)
	}
	setting = listVoltage(t, client, "T1-M01")
	if setting.FixedMillivolts != nil || setting.GetUpdatedBy() != defaultVoltageActor {
		t.Errorf("after clear = %v, want automatic by %s", setting, defaultVoltageActor)
	}
}

func TestModuleVoltageReportsBoardsThatDidNotApply(t *testing.T) {
	client, state := newModuleVoltageClient(t)
	faulted := setBoardState(state, "T1-M02", 1, miningStateError)

	resp, err := client.SetModuleFixedVoltage(context.Background(), connect.NewRequest(&mdk.SetModuleFixedVoltageRequest{ModuleId: "T1-M02", Millivolts: 27000}))
	if err != nil {
		t.Fatalf("SetModuleFixedVoltage: %v", err)
	}
	if resp.Msg.GetBoardsUpdated() != 2 {
		t.Errorf("boardsUpdated = %d, want 2", resp.Msg.GetBoardsUpdated())
	}
	for _, result := range resp.Msg.GetResults() {
		if (result.GetMacAddress() == faulted) == result.GetApplied() {
			t.Errorf("result %v: applied = %v for faulted board %s", result, result.GetApplied(), faulted)
		}
	}
	// The setting is saved even though one board has not taken it yet.
	if got := listVoltage(t, client, "T1-M02").GetFixedMillivolts(); got != 27000 {
		t.Errorf("saved fixed = %d, want 27000", got)
	}
}

func TestModuleVoltageRefusesWithoutLimitsAndOutsideRange(t *testing.T) {
	client, state := newModuleVoltageClient(t)
	ctx := context.Background()
	setBoardState(state, "T1-M03", 0, miningStateDisconnected)

	setting := listVoltage(t, client, "T1-M03")
	if setting.GetReadiness() != mdk.ModuleVoltageReadiness_MODULE_VOLTAGE_READINESS_LIMITS_UNAVAILABLE ||
		setting.GetSupportedMinimumMillivolts() != 0 || setting.GetSupportedMaximumMillivolts() != 0 {
		t.Errorf("setting = %v, want limits unavailable with no range", setting)
	}

	for _, tc := range []struct {
		name     string
		moduleID string
		mv       uint32
		want     connect.Code
	}{
		{"limits unavailable", "T1-M03", 28000, connect.CodeFailedPrecondition},
		{"below range", "T1-M01", 25999, connect.CodeInvalidArgument},
		{"above range", "T1-M01", 30001, connect.CodeInvalidArgument},
		{"zero", "T1-M01", 0, connect.CodeInvalidArgument},
		{"unknown module", "T9-M09", 28000, connect.CodeNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.SetModuleFixedVoltage(ctx, connect.NewRequest(&mdk.SetModuleFixedVoltageRequest{ModuleId: tc.moduleID, Millivolts: tc.mv}))
			if got := connect.CodeOf(err); got != tc.want {
				t.Fatalf("code = %v, want %v", got, tc.want)
			}
		})
	}
	if listVoltage(t, client, "T1-M01").FixedMillivolts != nil {
		t.Error("a refused write changed the saved setting")
	}

	// Restoring automatic voltage needs no limits; the disconnected board is retried.
	resp, err := client.ClearModuleFixedVoltage(ctx, connect.NewRequest(&mdk.ClearModuleFixedVoltageRequest{ModuleId: "T1-M03"}))
	if err != nil {
		t.Fatalf("ClearModuleFixedVoltage: %v", err)
	}
	if resp.Msg.GetBoardsUpdated() != 2 {
		t.Errorf("boardsUpdated = %d, want 2", resp.Msg.GetBoardsUpdated())
	}
}

func TestModuleVoltageScenarios(t *testing.T) {
	ctx := context.Background()
	set := func(client mdkv2connect.MiningServiceClient) error {
		_, err := client.SetModuleFixedVoltage(ctx, connect.NewRequest(&mdk.SetModuleFixedVoltageRequest{ModuleId: "T1-M01", Millivolts: 28000}))
		return err
	}

	t.Run("write-refused saves nothing", func(t *testing.T) {
		t.Setenv("CT1_MODULE_VOLTAGE_SCENARIO", "write-refused")
		client, _ := newModuleVoltageClient(t)
		if got := connect.CodeOf(set(client)); got != connect.CodeFailedPrecondition {
			t.Fatalf("code = %v, want FailedPrecondition", got)
		}
		if listVoltage(t, client, "T1-M01").FixedMillivolts != nil {
			t.Error("refused write was saved")
		}
	})

	t.Run("write-unknown saves before failing", func(t *testing.T) {
		t.Setenv("CT1_MODULE_VOLTAGE_SCENARIO", "write-unknown")
		client, _ := newModuleVoltageClient(t)
		if got := connect.CodeOf(set(client)); got != connect.CodeInternal {
			t.Fatalf("code = %v, want Internal", got)
		}
		if got := listVoltage(t, client, "T1-M01").GetFixedMillivolts(); got != 28000 {
			t.Errorf("saved fixed = %d, want 28000", got)
		}
	})
}

func TestModuleVoltageRESTOmitsFixedForAutomatic(t *testing.T) {
	srv, _ := newTestServer(t)
	raw := getRaw(t, srv, "/api/v2/mining/modules/voltage")
	settings, ok := raw["settings"].([]any)
	if !ok || len(settings) == 0 {
		t.Fatalf("settings = %v, want a list", raw["settings"])
	}
	first := settings[0].(map[string]any)
	if _, present := first["fixedMillivolts"]; present {
		t.Errorf("automatic module reports fixedMillivolts = %v", first["fixedMillivolts"])
	}
}
