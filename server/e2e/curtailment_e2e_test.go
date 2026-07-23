//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strconv"
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

	// Step 0b: Configure pools on the fresh proto-sim so it reports a real
	// mining load. A pool-less sim sits in the NoPools state (0 hashrate,
	// ~200W idle) and the curtailment selector would rightly exclude it
	// below candidate_min_power_w.
	configureProtoSimPools(t, ctx)

	// Step 1: Bootstrap admin + auth.
	username := "e2e-curtailment-admin"
	password := "e2e-test-password"
	createAdminViaAPI(t, ctx, username, password)
	token := authenticateViaRealAPI(t, ctx, username, password)

	// Step 2: Discover + pair the proto-sim miner.
	devices := discoverDeviceViaRealAPI(t, ctx, token, protoSimDiscoveryHost, protoSimPort)
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

	// Step 5: Poll ListActive until the reconciler advances pending → active.
	// 30s tick + telemetry confirmation gives a generous 3-minute budget.
	t.Run("ReconcilerAdvancesToActive", func(t *testing.T) {
		deadline := time.Now().Add(3 * time.Minute)
		var finalState curtailmentv1.CurtailmentEventState
		for time.Now().Before(deadline) {
			req := connect.NewRequest(&curtailmentv1.ListActiveCurtailmentsRequest{})
			req.Header().Set("Authorization", "Bearer "+token)
			resp, err := curtailmentClient.ListActiveCurtailments(ctx, req)
			require.NoError(t, err)
			for _, event := range resp.Msg.Events {
				if event.EventUuid == startedEventUUID {
					finalState = event.State
					if finalState == curtailmentv1.CurtailmentEventState_CURTAILMENT_EVENT_STATE_ACTIVE {
						t.Logf("✓ Event advanced to ACTIVE after %v",
							time.Since(deadline.Add(-3*time.Minute)).Truncate(time.Second))
						return
					}
					break
				}
			}
			time.Sleep(5 * time.Second)
		}
		t.Fatalf("event did not reach ACTIVE within 3 minutes; last state: %s", finalState.String())
	})

	// Step 5b: Confirmation fast path (issue #661). The base compose enables
	// CURTAILMENT_CONFIRMATION_FAST_PATH_ENABLED, so once the tick dispatches
	// Curtail, the pulse samples the proto-sim miner directly and promotes
	// the target to confirmed without waiting for the next 30s tick. The
	// dispatch→completion delta is the discriminating signal: tick-bound
	// confirmation cannot land before the next tick (~30s after dispatch),
	// while the pulse (3s cadence) lands in single-digit seconds.
	t.Run("FastPathConfirmsCurtailBeforeNextTick", func(t *testing.T) {
		delta := pollTargetPhaseCompletionDelta(t, startedEventUUID, "curtail", 2*time.Minute)
		assert.Less(t, delta, 25*time.Second,
			"curtail confirmation must land before the next full tick (fast path)")
		t.Logf("✓ Curtail confirmed %.1fs after dispatch (tick interval is 30s)", delta.Seconds())
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

	// Step 7: Poll ListActive until the event leaves the active set. Restore
	// at default 30s interval + adaptive batch_size for a single miner
	// completes in ~30–90s with telemetry confirmation.
	t.Run("RestoreCompletes", func(t *testing.T) {
		deadline := time.Now().Add(3 * time.Minute)
		listClient := curtailmentClient
		var lastSeenState string
		for time.Now().Before(deadline) {
			req := connect.NewRequest(&curtailmentv1.ListActiveCurtailmentsRequest{})
			req.Header().Set("Authorization", "Bearer "+token)
			resp, err := listClient.ListActiveCurtailments(ctx, req)
			require.NoError(t, err)
			var activeEvent *curtailmentv1.CurtailmentEvent
			for _, event := range resp.Msg.Events {
				if event.EventUuid == startedEventUUID {
					activeEvent = event
					break
				}
			}
			if activeEvent == nil {
				t.Logf("✓ Event left active set — restore completed")
				return
			}
			lastSeenState = activeEvent.State.String()
			time.Sleep(5 * time.Second)
		}
		t.Fatalf("event did not terminate within 3 minutes; last active state: %s", lastSeenState)
	})

	// Step 7b: Restore-side fast path — the pulse resolves the dispatched
	// restore target from a fresh post-Uncurtail sample before the next tick.
	t.Run("FastPathResolvesRestoreBeforeNextTick", func(t *testing.T) {
		delta := pollTargetPhaseCompletionDelta(t, startedEventUUID, "restore", 2*time.Minute)
		assert.Less(t, delta, 25*time.Second,
			"restore resolution must land before the next full tick (fast path)")
		t.Logf("✓ Restore resolved %.1fs after dispatch (tick interval is 30s)", delta.Seconds())
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

	configureProtoSimPools(t, ctx)

	username := "e2e-curtailment-restart"
	password := "e2e-test-password"
	createAdminViaAPI(t, ctx, username, password)
	token := authenticateViaRealAPI(t, ctx, username, password)

	devices := discoverDeviceViaRealAPI(t, ctx, token, protoSimDiscoveryHost, protoSimPort)
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
		req := connect.NewRequest(&curtailmentv1.ListActiveCurtailmentsRequest{})
		req.Header().Set("Authorization", "Bearer "+token)
		resp, err := curtailmentClient.ListActiveCurtailments(ctx, req)
		require.NoError(t, err)
		stillActive := false
		for _, event := range resp.Msg.Events {
			if event.EventUuid == eventUUID {
				stillActive = true
				break
			}
		}
		if !stillActive {
			t.Logf("✓ Event terminal after restart-and-stop")
			return
		}
		time.Sleep(5 * time.Second)
	}
	t.Fatal("event did not terminate after restart within 3 minutes")
}

// configureProtoSimPools authenticates against the proto-sim's REST API on
// its host-mapped port and configures one pool. A fresh sim boots with no
// pools and reports the NoPools mining state (zero hashrate, idle power),
// which the curtailment selector excludes below candidate_min_power_w;
// configuring a pool flips it to Mining at full simulated load, matching a
// production miner. Password mirrors FAKE_RIG_PASSWORD in docker-compose.
func configureProtoSimPools(t *testing.T, ctx context.Context) {
	t.Helper()
	const simBaseURL = "http://localhost:" + protoSimPort

	loginBody, err := json.Marshal(map[string]string{"password": "proto"})
	require.NoError(t, err)
	loginReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		simBaseURL+"/api/v1/auth/login", bytes.NewReader(loginBody))
	require.NoError(t, err)
	loginReq.Header.Set("Content-Type", "application/json")
	loginResp, err := http.DefaultClient.Do(loginReq)
	require.NoError(t, err, "proto-sim login should succeed")
	defer loginResp.Body.Close()
	require.Equal(t, http.StatusOK, loginResp.StatusCode, "proto-sim login should return 200")
	var auth struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.NewDecoder(loginResp.Body).Decode(&auth))
	require.NotEmpty(t, auth.AccessToken)

	poolsBody, err := json.Marshal([]map[string]string{{
		"name":     "e2e-pool",
		"url":      "stratum+tcp://pool.example.com:3333",
		"username": "e2e-worker",
		"password": "x",
	}})
	require.NoError(t, err)
	poolsReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		simBaseURL+"/api/v1/pools", bytes.NewReader(poolsBody))
	require.NoError(t, err)
	poolsReq.Header.Set("Content-Type", "application/json")
	poolsReq.Header.Set("Authorization", "Bearer "+auth.AccessToken)
	poolsResp, err := http.DefaultClient.Do(poolsReq)
	require.NoError(t, err, "proto-sim pool configuration should succeed")
	defer poolsResp.Body.Close()
	require.Equal(t, http.StatusOK, poolsResp.StatusCode, "proto-sim pool configuration should return 200")
	t.Log("✓ Configured pools on proto-sim (miner now reports full mining load)")
}

// pollTargetPhaseCompletionDelta polls the event's single target row until
// the given phase ("curtail" or "restore") has both a dispatch and a
// completion timestamp, then returns completion − dispatch. Reading the
// durable phase columns directly from Postgres pins the same timestamps
// the reconciler's guarded writes stamp, independent of API projection.
func pollTargetPhaseCompletionDelta(t *testing.T, eventUUID, phase string, timeout time.Duration) time.Duration {
	t.Helper()
	require.NotEmpty(t, eventUUID, "event UUID required — Start must have succeeded")
	query := "SELECT EXTRACT(EPOCH FROM (ct." + phase + "_completed_at - ct." + phase + "_dispatched_at)) " +
		"FROM curtailment_target ct " +
		"JOIN curtailment_event ce ON ce.id = ct.curtailment_event_id " +
		"WHERE ce.event_uuid = '" + eventUUID + "' " +
		"AND ct." + phase + "_completed_at IS NOT NULL " +
		"AND ct." + phase + "_dispatched_at IS NOT NULL;"
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		cmd := exec.Command("docker", "exec", containerPrefix+"timescaledb-1",
			"psql", "-U", "fleet", "-d", "fleet", "-t", "-A", "-c", query)
		out, err := cmd.Output()
		require.NoError(t, err, "psql phase timestamp read should succeed")
		raw := strings.TrimSpace(string(out))
		if raw != "" {
			seconds, err := strconv.ParseFloat(raw, 64)
			require.NoError(t, err, "phase delta parse should succeed: %q", raw)
			return time.Duration(seconds * float64(time.Second))
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("target %s phase did not complete within %v", phase, timeout)
	return 0
}

// readHeartbeatLastTickAt reads the singleton heartbeat row directly from
// Postgres so the test asserts what the staleness alert would see, not
// what fleetd reports. Mirrors the runbook's alert predicate.
func readHeartbeatLastTickAt(t *testing.T) time.Time {
	t.Helper()
	// -U fleet: the compose stack provisions only the 'fleet' role
	// (POSTGRES_USER in docker-compose.base.yaml); no 'postgres' superuser exists.
	cmd := exec.Command("docker", "exec", containerPrefix+"timescaledb-1",
		"psql", "-U", "fleet", "-d", "fleet", "-t", "-A", "-c",
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
