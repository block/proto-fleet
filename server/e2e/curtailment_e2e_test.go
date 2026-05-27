//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	curtailmentv1 "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/generated/grpc/curtailment/v1/curtailmentv1connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCurtailmentLifecycle validates the full operator-facing curtailment
// flow against the real fleet-api inside docker-compose:
//
//	pair miners → Preview → Start → reconciler dispatches Curtail →
//	telemetry confirms → Stop → reconciler dispatches Uncurtail →
//	restore confirms → event terminal
//
// The test exercises the same path a grid-program-call operator would
// take: select a small fleet, command a kW reduction, wait for telemetry
// verification, then restore. The proto-sim plugin honors Curtail/Uncurtail
// against its in-process miner so the telemetry side of the loop produces
// real before/after numbers.
//
// Prerequisites: relies on the docker-compose environment with a paired
// proto-sim miner. Reuses the same setup helpers (createAdminViaAPI,
// authenticateViaRealAPI, discoverDeviceViaRealAPI, pairDeviceViaRealAPI)
// that TestCompletePluginWorkflow uses.
func TestCurtailmentLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	ctx := context.Background()

	// Step 0: Reset docker-compose so the test owns the environment.
	t.Log("Running 'just rebuild-all' to reset docker-compose environment...")
	rebuildCmd := exec.Command("just", "rebuild-all")
	rebuildCmd.Stdout = os.Stdout
	rebuildCmd.Stderr = os.Stderr
	require.NoError(t, rebuildCmd.Run(), "just rebuild-all should succeed")

	t.Log("Waiting for fleet-api to be ready...")
	waitForFleetAPIHealth(t, ctx, 60*time.Second)

	// Step 1: Bootstrap admin + auth.
	username := "e2e-curtailment-admin"
	password := "e2e-test-password"
	createAdminViaAPI(t, ctx, username, password)
	token := authenticateViaRealAPI(t, ctx, username, password)

	// Step 2: Discover + pair the proto-sim miner.
	devices := discoverDeviceViaRealAPI(t, ctx, token, protoSimIP, protoSimPort)
	require.Len(t, devices, 1, "should discover exactly one proto-sim device")
	deviceID := devices[0].DeviceIdentifier
	require.NotEmpty(t, deviceID, "device identifier must be set")
	pairDeviceViaRealAPI(t, ctx, token, deviceID)
	t.Logf("✓ Paired device: %s", deviceID)

	// Wait for the first telemetry sample so the selector has dual-signal
	// candidate data to rank against.
	t.Log("Waiting for initial telemetry...")
	_ = pollForTelemetryViaRealAPI(t, ctx, token, deviceID, 60*time.Second)

	curtailmentClient := curtailmentv1connect.NewCurtailmentServiceClient(http.DefaultClient, fleetAPIURL)

	// Step 3: Preview. A 1 kW target on a single-miner fleet should select
	// the miner (over-curtail accepted per the FIXED_KW contract).
	t.Run("Preview", func(t *testing.T) {
		previewReq := connect.NewRequest(&curtailmentv1.PreviewCurtailmentPlanRequest{
			Scope: &curtailmentv1.PreviewCurtailmentPlanRequest_WholeOrg{
				WholeOrg: &curtailmentv1.ScopeWholeOrg{},
			},
			Mode:     curtailmentv1.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
			Strategy: curtailmentv1.CurtailmentStrategy_CURTAILMENT_STRATEGY_LEAST_EFFICIENT_FIRST,
			Level:    curtailmentv1.CurtailmentLevel_CURTAILMENT_LEVEL_FULL,
			Priority: curtailmentv1.CurtailmentPriority_CURTAILMENT_PRIORITY_NORMAL,
			ModeParams: &curtailmentv1.PreviewCurtailmentPlanRequest_FixedKw{
				FixedKw: &curtailmentv1.FixedKwParams{TargetKw: 1.0},
			},
		})
		previewReq.Header().Set("Authorization", "Bearer "+token)

		resp, err := curtailmentClient.PreviewCurtailmentPlan(ctx, previewReq)
		require.NoError(t, err, "preview should succeed against a paired fleet")
		require.NotEmpty(t, resp.Msg.Candidates, "preview should pick at least one candidate")
		t.Logf("✓ Preview selected %d candidate(s); estimated reduction %.2f kW",
			len(resp.Msg.Candidates), resp.Msg.EstimatedReductionKw)
	})

	// Step 4: Start. Persists the event + targets, the reconciler picks
	// up the pending event on its next tick and dispatches Curtail.
	var startedEventUUID string
	t.Run("Start", func(t *testing.T) {
		startReq := connect.NewRequest(&curtailmentv1.StartCurtailmentRequest{
			Scope: &curtailmentv1.StartCurtailmentRequest_WholeOrg{
				WholeOrg: &curtailmentv1.ScopeWholeOrg{},
			},
			Mode:     curtailmentv1.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
			Strategy: curtailmentv1.CurtailmentStrategy_CURTAILMENT_STRATEGY_LEAST_EFFICIENT_FIRST,
			Level:    curtailmentv1.CurtailmentLevel_CURTAILMENT_LEVEL_FULL,
			Priority: curtailmentv1.CurtailmentPriority_CURTAILMENT_PRIORITY_NORMAL,
			ModeParams: &curtailmentv1.StartCurtailmentRequest_FixedKw{
				FixedKw: &curtailmentv1.FixedKwParams{TargetKw: 1.0},
			},
			Reason: "e2e curtailment lifecycle test",
		})
		startReq.Header().Set("Authorization", "Bearer "+token)

		resp, err := curtailmentClient.StartCurtailment(ctx, startReq)
		require.NoError(t, err, "start should succeed")
		require.NotNil(t, resp.Msg.Event, "start response must carry the event")
		startedEventUUID = resp.Msg.Event.EventUuid
		require.NotEmpty(t, startedEventUUID, "event_uuid must be set")
		t.Logf("✓ Started curtailment event %s in state %s",
			startedEventUUID, resp.Msg.Event.State.String())
	})

	// Step 5: Poll GetActive until the reconciler advances pending → active.
	// 30s tick + telemetry confirmation gives a generous 3-minute budget.
	t.Run("ReconcilerAdvancesToActive", func(t *testing.T) {
		deadline := time.Now().Add(3 * time.Minute)
		var finalState curtailmentv1.CurtailmentEventState
		for time.Now().Before(deadline) {
			req := connect.NewRequest(&curtailmentv1.GetActiveCurtailmentRequest{})
			req.Header().Set("Authorization", "Bearer "+token)
			resp, err := curtailmentClient.GetActiveCurtailment(ctx, req)
			require.NoError(t, err)
			require.NotNil(t, resp.Msg.Event, "active event must be present while curtailment is in flight")
			finalState = resp.Msg.Event.State
			if finalState == curtailmentv1.CurtailmentEventState_CURTAILMENT_EVENT_STATE_ACTIVE {
				t.Logf("✓ Event advanced to ACTIVE after %v",
					time.Since(deadline.Add(-3*time.Minute)).Truncate(time.Second))
				return
			}
			time.Sleep(5 * time.Second)
		}
		t.Fatalf("event did not reach ACTIVE within 3 minutes; last state: %s", finalState.String())
	})

	// Step 6: Stop the event. The handler flips desired_state to active in
	// the same tx, and the reconciler's restore arm picks up the batch.
	t.Run("Stop", func(t *testing.T) {
		stopReq := connect.NewRequest(&curtailmentv1.StopCurtailmentRequest{
			EventUuid: startedEventUUID,
			Force:     true, // bypass min_curtailed_duration_sec on the short e2e run
		})
		stopReq.Header().Set("Authorization", "Bearer "+token)

		_, err := curtailmentClient.StopCurtailment(ctx, stopReq)
		require.NoError(t, err, "stop should succeed (Force=true bypasses min duration)")
		t.Logf("✓ Stop accepted for event %s", startedEventUUID)
	})

	// Step 7: Poll GetActive until the event reaches a terminal state
	// (COMPLETED or COMPLETED_WITH_FAILURES). Restore at default 30s
	// interval + adaptive batch_size for a single miner completes in
	// ~30–90s with telemetry confirmation.
	t.Run("RestoreCompletes", func(t *testing.T) {
		deadline := time.Now().Add(3 * time.Minute)
		listClient := curtailmentClient
		var lastSeenState string
		for time.Now().Before(deadline) {
			// GetActive returns nil event once the event is terminal.
			req := connect.NewRequest(&curtailmentv1.GetActiveCurtailmentRequest{})
			req.Header().Set("Authorization", "Bearer "+token)
			resp, err := listClient.GetActiveCurtailment(ctx, req)
			require.NoError(t, err)
			if resp.Msg.Event == nil {
				t.Logf("✓ No active event — restore completed")
				return
			}
			lastSeenState = resp.Msg.Event.State.String()
			time.Sleep(5 * time.Second)
		}
		t.Fatalf("event did not terminate within 3 minutes; last active state: %s", lastSeenState)
	})

	// Step 8: List the event via the read API and assert it lands in a
	// terminal state. Cursor pagination + decision-snapshot trimming are
	// exercised by hitting the list path; the response shape contract is
	// pinned by the handler unit tests, so the e2e just confirms the
	// terminated event surfaces here.
	t.Run("ListSurfacesTerminalEvent", func(t *testing.T) {
		listReq := connect.NewRequest(&curtailmentv1.ListCurtailmentEventsRequest{
			PageSize: 10,
		})
		listReq.Header().Set("Authorization", "Bearer "+token)

		resp, err := curtailmentClient.ListCurtailmentEvents(ctx, listReq)
		require.NoError(t, err)
		require.NotEmpty(t, resp.Msg.Events, "history must include the just-terminated event")

		var found bool
		for _, ev := range resp.Msg.Events {
			if ev.EventUuid == startedEventUUID {
				found = true
				assert.Contains(t,
					[]curtailmentv1.CurtailmentEventState{
						curtailmentv1.CurtailmentEventState_CURTAILMENT_EVENT_STATE_COMPLETED,
						curtailmentv1.CurtailmentEventState_CURTAILMENT_EVENT_STATE_COMPLETED_WITH_FAILURES,
						curtailmentv1.CurtailmentEventState_CURTAILMENT_EVENT_STATE_FAILED,
						curtailmentv1.CurtailmentEventState_CURTAILMENT_EVENT_STATE_CANCELLED,
					},
					ev.State, "event must land in a terminal state after restore")
				assert.Empty(t, ev.Targets,
					"list response must omit per-target rows (trimmed by handler)")
				break
			}
		}
		require.True(t, found, "started event %s must surface in the history list", startedEventUUID)
		t.Logf("✓ Event %s surfaced in history with a terminal state", startedEventUUID)
	})
}

// TestCurtailmentReconcilerKillAndResume validates the operational contract
// that the reconciler's restart-safety machinery handles a hard fleet-api
// restart mid-event without losing the event. The heartbeat row tells the
// staleness-alert SQL when the reconciler last ticked; on restart, the
// next tick re-claims the pending event from persisted state.
//
// The test:
//  1. Starts a curtailment event
//  2. Captures last_tick_at while the event is in flight
//  3. Restarts the fleet-api container
//  4. Verifies last_tick_at advances again after restart (proves the
//     reconciler resumed and processed the in-flight event)
//  5. Stops + waits for terminal
//
// Builds on TestCurtailmentLifecycle's helpers; runs as a separate test
// so the lifecycle test doesn't inherit restart latency.
func TestCurtailmentReconcilerKillAndResume(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	ctx := context.Background()

	t.Log("Running 'just rebuild-all' to reset docker-compose environment...")
	rebuildCmd := exec.Command("just", "rebuild-all")
	rebuildCmd.Stdout = os.Stdout
	rebuildCmd.Stderr = os.Stderr
	require.NoError(t, rebuildCmd.Run(), "just rebuild-all should succeed")

	t.Log("Waiting for fleet-api to be ready...")
	waitForFleetAPIHealth(t, ctx, 60*time.Second)

	username := "e2e-curtailment-restart"
	password := "e2e-test-password"
	createAdminViaAPI(t, ctx, username, password)
	token := authenticateViaRealAPI(t, ctx, username, password)

	devices := discoverDeviceViaRealAPI(t, ctx, token, protoSimIP, protoSimPort)
	require.Len(t, devices, 1)
	deviceID := devices[0].DeviceIdentifier
	pairDeviceViaRealAPI(t, ctx, token, deviceID)
	_ = pollForTelemetryViaRealAPI(t, ctx, token, deviceID, 60*time.Second)

	curtailmentClient := curtailmentv1connect.NewCurtailmentServiceClient(http.DefaultClient, fleetAPIURL)

	// Start a curtailment event.
	startReq := connect.NewRequest(&curtailmentv1.StartCurtailmentRequest{
		Scope: &curtailmentv1.StartCurtailmentRequest_WholeOrg{
			WholeOrg: &curtailmentv1.ScopeWholeOrg{},
		},
		Mode:     curtailmentv1.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
		Strategy: curtailmentv1.CurtailmentStrategy_CURTAILMENT_STRATEGY_LEAST_EFFICIENT_FIRST,
		Level:    curtailmentv1.CurtailmentLevel_CURTAILMENT_LEVEL_FULL,
		Priority: curtailmentv1.CurtailmentPriority_CURTAILMENT_PRIORITY_NORMAL,
		ModeParams: &curtailmentv1.StartCurtailmentRequest_FixedKw{
			FixedKw: &curtailmentv1.FixedKwParams{TargetKw: 1.0},
		},
		Reason: "e2e reconciler restart test",
	})
	startReq.Header().Set("Authorization", "Bearer "+token)
	startResp, err := curtailmentClient.StartCurtailment(ctx, startReq)
	require.NoError(t, err)
	eventUUID := startResp.Msg.Event.EventUuid
	t.Logf("✓ Started event %s; waiting for at least one tick to land...", eventUUID)

	// Give the reconciler a tick or two so the heartbeat advances and the
	// pending event has at least one DISPATCHING/DISPATCHED transition
	// persisted. 90s = 3× the default tick interval.
	time.Sleep(90 * time.Second)

	preRestartHeartbeat := readHeartbeatLastTickAt(t)
	require.False(t, preRestartHeartbeat.IsZero(),
		"heartbeat must have a tick timestamp by 90s after Start")
	t.Logf("Pre-restart heartbeat last_tick_at = %s", preRestartHeartbeat.Format(time.RFC3339))

	// Hard-restart the fleet-api container. The reconciler goroutine dies
	// with the process; restart-safety machinery has to re-claim the
	// pending event from persisted state.
	t.Log("Restarting fleet-api container...")
	restartCmd := exec.Command("docker", "restart", containerPrefix+"fleet-api-1")
	restartCmd.Stdout = os.Stdout
	restartCmd.Stderr = os.Stderr
	require.NoError(t, restartCmd.Run(), "docker restart fleet-api should succeed")

	t.Log("Waiting for fleet-api to be ready after restart...")
	waitForFleetAPIHealth(t, ctx, 120*time.Second)
	// Re-authenticate — the previous token was issued by the
	// pre-restart process and the new process treats it as opaque.
	token = authenticateViaRealAPI(t, ctx, username, password)

	// After restart, the reconciler should tick again within 60s and
	// advance the heartbeat past its pre-restart timestamp.
	t.Log("Polling heartbeat for post-restart tick...")
	deadline := time.Now().Add(120 * time.Second)
	var postRestartHeartbeat time.Time
	for time.Now().Before(deadline) {
		postRestartHeartbeat = readHeartbeatLastTickAt(t)
		if postRestartHeartbeat.After(preRestartHeartbeat) {
			t.Logf("✓ Reconciler resumed; heartbeat advanced to %s (Δ %v)",
				postRestartHeartbeat.Format(time.RFC3339),
				postRestartHeartbeat.Sub(preRestartHeartbeat).Truncate(time.Second))
			break
		}
		time.Sleep(5 * time.Second)
	}
	require.True(t, postRestartHeartbeat.After(preRestartHeartbeat),
		"heartbeat must advance after restart; restart-safety machinery should have resumed the reconciler")

	// Stop the event via the operator path and let it drain.
	stopReq := connect.NewRequest(&curtailmentv1.StopCurtailmentRequest{
		EventUuid: eventUUID,
		Force:     true,
	})
	stopReq.Header().Set("Authorization", "Bearer "+token)
	_, err = curtailmentClient.StopCurtailment(ctx, stopReq)
	require.NoError(t, err, "Stop should succeed after restart")

	// Drain to terminal — the resumed reconciler should drive the restore.
	t.Log("Waiting for event to terminate after restart + stop...")
	deadline = time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		req := connect.NewRequest(&curtailmentv1.GetActiveCurtailmentRequest{})
		req.Header().Set("Authorization", "Bearer "+token)
		resp, err := curtailmentClient.GetActiveCurtailment(ctx, req)
		require.NoError(t, err)
		if resp.Msg.Event == nil {
			t.Logf("✓ Event terminal after restart-and-stop")
			return
		}
		time.Sleep(5 * time.Second)
	}
	t.Fatal("event did not terminate after restart within 3 minutes")
}

// readHeartbeatLastTickAt reads the singleton heartbeat row directly from
// Postgres so the test asserts what the staleness alert would see, not
// what fleetd reports. Mirrors the runbook's alert predicate.
func readHeartbeatLastTickAt(t *testing.T) time.Time {
	t.Helper()
	cmd := exec.Command("docker", "exec", containerPrefix+"timescaledb-1",
		"psql", "-U", "postgres", "-d", "fleet", "-t", "-A", "-c",
		"SELECT to_char(last_tick_at AT TIME ZONE 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS.MS\"Z\"') FROM curtailment_reconciler_heartbeat WHERE id = 1;")
	out, err := cmd.Output()
	require.NoError(t, err, "psql heartbeat read should succeed")
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return time.Time{}
	}
	parsed, err := time.Parse("2006-01-02T15:04:05.000Z", raw)
	require.NoError(t, err, "heartbeat timestamp parse should succeed: %q", raw)
	return parsed
}
