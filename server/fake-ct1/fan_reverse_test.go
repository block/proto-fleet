package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2/mdkv2connect"
)

// A 20 ms procedure minute keeps a 3-minute run to 60 ms of wall time.
const testFanReverseMinute = 20 * time.Millisecond

func newFanReverseClient(t *testing.T, mutate func(*ContainerConfig)) (mdkv2connect.PowerServiceClient, *ContainerState) {
	t.Helper()
	cfg := testConfig()
	cfg.FanReverseMinute = testFanReverseMinute
	if mutate != nil {
		mutate(&cfg)
	}
	srv, state := newAlarmConnectServer(t, cfg)
	// Run the clock on wall time from the state's start, so the first request
	// doesn't catch up on months of simulated history.
	began := time.Now()
	state.now = func() time.Time { return state.StartTime.Add(time.Since(began)) }
	return mdkv2connect.NewPowerServiceClient(http.DefaultClient, srv.URL), state
}

func runFanReverse(t *testing.T, client mdkv2connect.PowerServiceClient, minutes uint32) (*mdk.RunDryCoolerFanReverseResponse, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := client.RunDryCoolerFanReverse(ctx, connect.NewRequest(&mdk.RunDryCoolerFanReverseRequest{DurationMinutes: minutes}))
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

func fanReverseControls(t *testing.T, client mdkv2connect.PowerServiceClient) *mdk.GetContainerControlsResponse {
	t.Helper()
	res, err := client.GetContainerControls(context.Background(), connect.NewRequest(&mdk.GetContainerControlsRequest{}))
	if err != nil {
		t.Fatalf("GetContainerControls: %v", err)
	}
	return res.Msg
}

func requireCode(t *testing.T, err error, want connect.Code) {
	t.Helper()
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) || connectErr.Code() != want {
		t.Fatalf("error = %v, want %s", err, want)
	}
}

func TestFanReverseRunsToFinishedAndKeepsCoolingSettings(t *testing.T) {
	client, state := newFanReverseClient(t, nil)
	initial := fanReverseControls(t, client)
	if !initial.GetDryCoolerFanReverseSupported() ||
		initial.GetDryCoolerFanReverse().GetState() != mdk.DryCoolerFanReverseState_DRY_COOLER_FAN_REVERSE_STATE_STOPPED ||
		initial.GetDryCoolerFanReverse().GetDurationMinutes() != fanReverseDefaultDuration {
		t.Fatalf("initial readback = %+v", initial.GetDryCoolerFanReverse())
	}
	state.mu.RLock()
	auto, speed := state.ContainerControls["dry_cooler_auto"], state.ContainerSetpoints["dry_cooler_manual_speed_percent"]
	state.mu.RUnlock()

	res, err := runFanReverse(t, client, 3)
	if err != nil {
		t.Fatalf("RunDryCoolerFanReverse: %v", err)
	}
	if res.GetStatus().GetState() != mdk.DryCoolerFanReverseState_DRY_COOLER_FAN_REVERSE_STATE_FINISHED || res.GetStatus().GetDurationMinutes() != 3 {
		t.Fatalf("terminal status = %+v", res.GetStatus())
	}
	after := fanReverseControls(t, client).GetDryCoolerFanReverse()
	if after.GetState() != mdk.DryCoolerFanReverseState_DRY_COOLER_FAN_REVERSE_STATE_FINISHED || after.GetDurationMinutes() != 3 {
		t.Fatalf("readback after run = %+v", after)
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	if state.ContainerControls["dry_cooler_auto"] != auto || state.ContainerSetpoints["dry_cooler_manual_speed_percent"] != speed {
		t.Fatal("the procedure must not change the dry cooler's mode or speed")
	}
}

func TestFanReverseRunDoesNotBlockOtherCallsAndRefusesASecondRun(t *testing.T) {
	client, _ := newFanReverseClient(t, func(cfg *ContainerConfig) { cfg.FanReverseMinute = 100 * time.Millisecond })
	done := make(chan error, 1)
	go func() {
		_, err := runFanReverse(t, client, 3)
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		state := fanReverseControls(t, client).GetDryCoolerFanReverse().GetState()
		if state == mdk.DryCoolerFanReverseState_DRY_COOLER_FAN_REVERSE_STATE_STARTING ||
			state == mdk.DryCoolerFanReverseState_DRY_COOLER_FAN_REVERSE_STATE_IN_PROGRESS {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("readback never reported the run active; last state %s", state)
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, err := runFanReverse(t, client, 4)
	requireCode(t, err, connect.CodeFailedPrecondition)
	if err := <-done; err != nil {
		t.Fatalf("first run: %v", err)
	}
}

func TestFanReverseRefusals(t *testing.T) {
	client, _ := newFanReverseClient(t, nil)
	for _, minutes := range []uint32{0, 2, 11} {
		_, err := runFanReverse(t, client, minutes)
		requireCode(t, err, connect.CodeInvalidArgument)
	}

	unsupported, _ := newFanReverseClient(t, func(cfg *ContainerConfig) { cfg.FanReverseUnsupported = true })
	controls := fanReverseControls(t, unsupported)
	if controls.GetDryCoolerFanReverseSupported() || controls.DryCoolerFanReverse != nil {
		t.Fatalf("unsupported controller advertised the procedure: %+v", controls)
	}
	_, err := runFanReverse(t, unsupported, 3)
	requireCode(t, err, connect.CodeUnimplemented)

	t.Setenv("CT1_FAN_REVERSE_SCENARIO", fanReverseNotPrimary)
	_, err = runFanReverse(t, client, 3)
	requireCode(t, err, connect.CodeFailedPrecondition)
}

func TestFanReverseScenarios(t *testing.T) {
	t.Run("no activation is an unknown outcome and stays stopped", func(t *testing.T) {
		t.Setenv("CT1_FAN_REVERSE_SCENARIO", fanReverseNoActivation)
		client, _ := newFanReverseClient(t, nil)
		_, err := runFanReverse(t, client, 4)
		requireCode(t, err, connect.CodeDeadlineExceeded)
		readback := fanReverseControls(t, client).GetDryCoolerFanReverse()
		if readback.GetState() != mdk.DryCoolerFanReverseState_DRY_COOLER_FAN_REVERSE_STATE_STOPPED || readback.GetDurationMinutes() != 4 {
			t.Fatalf("readback = %+v", readback)
		}
	})
	t.Run("interrupted run ends stopped", func(t *testing.T) {
		t.Setenv("CT1_FAN_REVERSE_SCENARIO", fanReverseInterrupted)
		client, _ := newFanReverseClient(t, nil)
		res, err := runFanReverse(t, client, 3)
		if err != nil {
			t.Fatal(err)
		}
		if res.GetStatus().GetState() != mdk.DryCoolerFanReverseState_DRY_COOLER_FAN_REVERSE_STATE_STOPPED {
			t.Fatalf("terminal status = %+v", res.GetStatus())
		}
	})
	t.Run("lost response finishes on the PLC", func(t *testing.T) {
		t.Setenv("CT1_FAN_REVERSE_SCENARIO", fanReverseLostResponse)
		client, _ := newFanReverseClient(t, nil)
		_, err := runFanReverse(t, client, 3)
		requireCode(t, err, connect.CodeDeadlineExceeded)
		if state := fanReverseControls(t, client).GetDryCoolerFanReverse().GetState(); state != mdk.DryCoolerFanReverseState_DRY_COOLER_FAN_REVERSE_STATE_FINISHED {
			t.Fatalf("readback state = %s", state)
		}
	})
}

func TestFanReverseOverRESTControls(t *testing.T) {
	cfg := testConfig()
	srv, _ := newAlarmConnectServer(t, cfg)
	res, err := http.Get(srv.URL + "/api/v2/power/container/controls")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		DryCoolerFanReverse          *dryCoolerFanReverseStatus `json:"dryCoolerFanReverse"`
		DryCoolerFanReverseSupported bool                       `json:"dryCoolerFanReverseSupported"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.DryCoolerFanReverseSupported || body.DryCoolerFanReverse == nil || body.DryCoolerFanReverse.State != fanReverseStopped {
		t.Fatalf("REST controls = %+v", body)
	}
}

func TestFanReverseCallOutlivesTheServerWriteDeadline(t *testing.T) {
	slow := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(150 * time.Millisecond)
		_, _ = w.Write([]byte("done"))
	})
	server := httptest.NewUnstartedServer(withFanReverseWriteDeadline(slow))
	server.Config.WriteTimeout = 20 * time.Millisecond
	server.Start()
	defer server.Close()

	response, err := http.Post(server.URL+"/mdk.v2.PowerService/RunDryCoolerFanReverse", "application/json", nil)
	if err != nil {
		t.Fatalf("the long call lost its answer: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "done" {
		t.Fatalf("body = %q, err = %v", body, err)
	}
	if other, err := http.Post(server.URL+"/other", "application/json", nil); err == nil {
		other.Body.Close()
		t.Fatal("other routes keep the server write deadline")
	}
}
