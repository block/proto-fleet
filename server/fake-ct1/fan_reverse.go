package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The dry-cooler fan reversal models Frigate's RunDryCoolerFanReverse on the
// Trident96 PLC: the controller retains a 3-10 whole-minute duration, the PLC
// owns the procedure (starting, in progress, finished), and the RPC returns
// only once the PLC reports a terminal state. The procedure never touches the
// dry cooler's power, auto/manual mode or speed controls.
//
// One simulated procedure minute lasts CT1_FAN_REVERSE_MINUTE of wall time so
// local runs finish quickly. Frigate's 15 s activation window and duration+2
// minute completion window scale with it.

const (
	fanReverseMinDuration     = 3
	fanReverseMaxDuration     = 10
	fanReverseDefaultDuration = 5
	fanReverseDefaultMinute   = 2 * time.Second
	fanReversePollInterval    = 50 * time.Millisecond

	// CT1_FAN_REVERSE_SCENARIO values.
	fanReverseNotPrimary   = "not-primary"
	fanReverseNoActivation = "no-activation"
	fanReverseInterrupted  = "interrupted"
	fanReverseLostResponse = "lost-response"
)

const (
	fanReverseStopped    = "DRY_COOLER_FAN_REVERSE_STATE_STOPPED"
	fanReverseStarting   = "DRY_COOLER_FAN_REVERSE_STATE_STARTING"
	fanReverseInProgress = "DRY_COOLER_FAN_REVERSE_STATE_IN_PROGRESS"
	fanReverseFinished   = "DRY_COOLER_FAN_REVERSE_STATE_FINISHED"
)

type dryCoolerFanReverseStatus struct {
	State           string `json:"state"`
	DurationMinutes uint32 `json:"durationMinutes"`
}

// simulatedFanReverse is the PLC's procedure registers. A run's state is a
// function of the clock since it started, so reads never depend on request order.
type simulatedFanReverse struct {
	durationMinutes uint32
	// startedAt is zero until the first run, while the PLC reports stopped
	// (never run). After a run it reports finished (standby), or stopped when
	// the run was interrupted.
	startedAt   time.Time
	interrupted bool
}

func fanReverseScenario() string { return os.Getenv("CT1_FAN_REVERSE_SCENARIO") }

func (s *ContainerState) fanReverseMinute() time.Duration {
	if s.Config.FanReverseMinute > 0 {
		return s.Config.FanReverseMinute
	}
	return fanReverseDefaultMinute
}

// scaled converts a real-controller interval into simulator wall time.
func (s *ContainerState) scaled(real time.Duration) time.Duration {
	return time.Duration(float64(real) * float64(s.fanReverseMinute()) / float64(time.Minute))
}

// fanReverseStatusAt reads the procedure registers at now. Call while holding s.mu.
func (s *ContainerState) fanReverseStatusAt(now time.Time) dryCoolerFanReverseStatus {
	run := s.fanReverse
	out := dryCoolerFanReverseStatus{State: fanReverseStopped, DurationMinutes: run.durationMinutes}
	if run.startedAt.IsZero() {
		return out
	}
	elapsed := now.Sub(run.startedAt)
	length := time.Duration(run.durationMinutes) * s.fanReverseMinute()
	switch {
	case elapsed < s.scaled(10*time.Second):
		out.State = fanReverseStarting
	case run.interrupted && elapsed >= length/2:
		out.State = fanReverseStopped
	case elapsed < length:
		out.State = fanReverseInProgress
	default:
		out.State = fanReverseFinished
	}
	return out
}

func fanReverseActive(state string) bool {
	return state == fanReverseStarting || state == fanReverseInProgress
}

// fanReverseControls reports the readback GetContainerControls carries. Call while holding s.mu.
func (s *ContainerState) fanReverseControls() (*dryCoolerFanReverseStatus, bool) {
	if s.Config.FanReverseUnsupported {
		return nil, false
	}
	current := s.fanReverseStatusAt(s.now())
	return &current, true
}

// startFanReverse validates and starts one run. It returns the run's start
// time, or zero when the PLC silently ignored the start. Call while holding s.mu.
func (s *ContainerState) startFanReverse(durationMinutes uint32) (time.Time, error) {
	if durationMinutes < fanReverseMinDuration || durationMinutes > fanReverseMaxDuration {
		return time.Time{}, &apiError{http.StatusBadRequest, codes.InvalidArgument, "duration_minutes must be a whole number from 3 through 10"}
	}
	if s.Config.FanReverseUnsupported {
		return time.Time{}, &apiError{http.StatusNotImplemented, codes.Unimplemented, "container controls are not supported by this backend"}
	}
	if fanReverseScenario() == fanReverseNotPrimary {
		return time.Time{}, &apiError{http.StatusPreconditionFailed, codes.FailedPrecondition, "this node is not the container control primary"}
	}
	now := s.now()
	if fanReverseActive(s.fanReverseStatusAt(now).State) {
		return time.Time{}, &apiError{http.StatusPreconditionFailed, codes.FailedPrecondition, "a dry-cooler fan reversal is already in progress"}
	}
	s.fanReverse.durationMinutes = durationMinutes
	if fanReverseScenario() == fanReverseNoActivation {
		// The PLC keeps the duration but never leaves standby, as when one of
		// its interlocks blocks the procedure.
		return time.Time{}, nil
	}
	s.fanReverse.startedAt = now
	s.fanReverse.interrupted = fanReverseScenario() == fanReverseInterrupted
	return now, nil
}

// runDryCoolerFanReverse starts the procedure inside the shared request
// lifecycle, then waits for it outside, so other API calls continue during a
// run as they do on Frigate. It mirrors Frigate's outcome rules: no active
// state within the activation window, or no terminal state within the
// completion window, is DeadlineExceeded (outcome unknown).
func (s *ContainerState) runDryCoolerFanReverse(ctx context.Context, durationMinutes uint32) (*mdk.RunDryCoolerFanReverseResponse, error) {
	var startedAt time.Time
	var startErr error
	if err := s.serve(ctx, true, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		startedAt, startErr = s.startFanReverse(durationMinutes)
	}); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	if startErr != nil {
		return nil, startErr
	}
	activation := s.scaled(15 * time.Second)
	if startedAt.IsZero() {
		if err := sleepContext(ctx, activation); err != nil {
			return nil, fanReverseUnknown("waiting for the reversal to start", err)
		}
		return nil, status.Error(codes.DeadlineExceeded, "running dry-cooler fan reversal; reconcile authoritative controller state: reversal did not enter starting/in-progress state")
	}
	completion := min(time.Duration(durationMinutes+2)*time.Minute, 12*time.Minute)
	deadline := time.Now().Add(activation + s.scaled(completion))
	ticker := time.NewTicker(fanReversePollInterval)
	defer ticker.Stop()
	for {
		s.mu.RLock()
		current := s.fanReverseStatusAt(s.now())
		s.mu.RUnlock()
		if !fanReverseActive(current.State) {
			if fanReverseScenario() == fanReverseLostResponse {
				return nil, status.Error(codes.DeadlineExceeded, "running dry-cooler fan reversal; reconcile authoritative controller state: waiting for reversal completion: context deadline exceeded")
			}
			state := mdk.DryCoolerFanReverseState(mdk.DryCoolerFanReverseState_value[current.State])
			return &mdk.RunDryCoolerFanReverseResponse{Status: &mdk.DryCoolerFanReverseStatus{State: state, DurationMinutes: current.DurationMinutes}}, nil
		}
		if time.Now().After(deadline) {
			return nil, fanReverseUnknown("waiting for reversal completion", context.DeadlineExceeded)
		}
		select {
		case <-ctx.Done():
			return nil, fanReverseUnknown("waiting for reversal completion", ctx.Err())
		case <-ticker.C:
		}
	}
}

func fanReverseUnknown(phase string, err error) error {
	return status.Error(codes.DeadlineExceeded, fmt.Sprintf("running dry-cooler fan reversal; reconcile authoritative controller state: %s: %v", phase, err))
}

func sleepContext(ctx context.Context, wait time.Duration) error {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// selfServed procedures enter the shared request lifecycle themselves because
// they outlive a single serialized request.
func selfServed(procedure string) bool {
	return procedure == mdk.PowerService_RunDryCoolerFanReverse_FullMethodName
}

func (a *grpcAPI) RunDryCoolerFanReverse(ctx context.Context, in *mdk.RunDryCoolerFanReverseRequest) (*mdk.RunDryCoolerFanReverseResponse, error) {
	return a.state.runDryCoolerFanReverse(ctx, in.GetDurationMinutes())
}

// withFanReverseWriteDeadline lifts the server's write deadline for the fan
// reversal call, which answers only when the simulated procedure ends. That can
// be longer than the deadline that bounds every other response, for example
// with CT1_FAN_REVERSE_MINUTE=1m. The procedure bounds the call itself.
func withFanReverseWriteDeadline(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/RunDryCoolerFanReverse") {
			// Best effort: a writer that cannot change its deadline keeps the server's.
			_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
		}
		next.ServeHTTP(w, r)
	})
}
