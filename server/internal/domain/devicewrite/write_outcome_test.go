package devicewrite_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestAuthoritativeWriteOutcomesRetireOnlyFinishedInvocations(t *testing.T) {
	for _, tc := range []struct {
		name, outcome, status string
	}{
		{"refused", sdk.WriteNotDispatched, "complete"},
		{"terminal failure", sdk.WriteCompleted, "complete"},
		{"accepted background work", sdk.WriteUnknown, "unknown"},
		{"lost response", "", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database, org, device, message, _ := queuedFenceFixture(t, commandtype.ApplyCurtailmentConfig)
			service := devicewrite.NewService(database.DB)
			attempt, err := service.BeginQueuedHost(t.Context(), message)
			require.NoError(t, err)
			failure := errors.New("firmware may echo private-pool-password")
			if tc.outcome != "" {
				failure = sdk.WithWriteOutcome(failure, tc.outcome, "capability", "unsupported")
			} else {
				failure = devicewrite.UncertainDelivery(failure)
			}
			err = devicewrite.Dispatch(attempt.Context(t.Context()), func(context.Context) error { return failure })
			require.Error(t, err)
			// Completion uses captured evidence even when an outer layer sanitizes errors.
			require.NoError(t, attempt.Finish(t.Context(), errors.New("public failure")))
			var status string
			var diagnostic []byte
			require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT status,write_outcome FROM device_write_attempt WHERE device_id=$1", device).Scan(&status, &diagnostic))
			require.Equal(t, tc.status, status)
			require.NotContains(t, string(diagnostic), "private-pool-password")
			if tc.outcome != "" {
				var info map[string]string
				require.NoError(t, json.Unmarshal(diagnostic, &info))
				require.Equal(t, tc.outcome, info["outcome"])
			}
			_, reserveErr := service.Reserve(t.Context(), org, device)
			require.Equal(t, tc.status == "complete", reserveErr == nil)
		})
	}
}

func TestFinishedWriteDoesNotClearEarlierAsynchronousWork(t *testing.T) {
	database, org, device, message, _ := queuedFenceFixture(t, commandtype.Reboot)
	service := devicewrite.NewService(database.DB)
	attempt, err := service.BeginQueuedHost(t.Context(), message)
	require.NoError(t, err)
	ctx := attempt.Context(t.Context())
	require.NoError(t, devicewrite.Dispatch(ctx, func(context.Context) error { return nil }))
	err = devicewrite.Dispatch(ctx, func(context.Context) error {
		return sdk.WithWriteOutcome(errors.New("separate terminal failure"), sdk.WriteCompleted, "dispatch", "finished")
	})
	require.NoError(t, attempt.Finish(t.Context(), err))
	_, err = service.Reserve(t.Context(), org, device)
	require.Error(t, err)
}

func TestDirectRefusalRetainsSanitizedDiagnosticWithoutBlocking(t *testing.T) {
	database, org, device, _, _ := queuedFenceFixture(t, commandtype.ApplyCurtailmentConfig)
	service := devicewrite.NewService(database.DB)
	err := service.WithHostAttempt(t.Context(), org, device, 0, commandtype.ApplyCurtailmentConfig, func(ctx context.Context) error {
		return devicewrite.Dispatch(ctx, func(context.Context) error {
			return sdk.WithWriteOutcome(errors.New("unsupported"), sdk.WriteNotDispatched, "capability", "unsupported")
		})
	})
	require.Error(t, err)
	var count int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_write_attempt WHERE device_id=$1 AND queue_message_id IS NULL AND status='complete' AND write_outcome->>'stage'='capability'", device).Scan(&count))
	require.Equal(t, 1, count)
}

func TestAttemptEvidenceCoversEveryInvocation(t *testing.T) {
	for _, tc := range []struct {
		name, first, status, outcome string
		kind                         commandtype.Type
	}{
		{"ordered write then refusal", "", "complete", "", commandtype.UpdateMiningPools},
		{"completed write then refusal", sdk.WriteCompleted, "complete", sdk.WriteCompleted, commandtype.UpdateMiningPools},
		{"unknown write then refusal", sdk.WriteUnknown, "unknown", sdk.WriteUnknown, commandtype.UpdateMiningPools},
		{"asynchronous write then refusal", "", "unknown", sdk.WriteUnknown, commandtype.Reboot},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database, org, device, message, _ := queuedFenceFixture(t, tc.kind)
			service := devicewrite.NewService(database.DB)
			attempt, err := service.BeginQueuedHost(t.Context(), message)
			require.NoError(t, err)
			ctx := attempt.Context(t.Context())
			var firstErr error
			if tc.first != "" {
				firstErr = sdk.WithWriteOutcome(errors.New("first invocation"), tc.first, sdk.WriteStageDispatch, sdk.WriteReasonFinished)
			}
			require.Equal(t, firstErr, devicewrite.Dispatch(ctx, func(context.Context) error { return firstErr }))
			refused := sdk.WriteRefused(errors.New("later invocation refused"), sdk.WriteStageCapability, sdk.WriteReasonUnsupported)
			err = devicewrite.Dispatch(ctx, func(context.Context) error { return refused })
			require.Error(t, err)
			require.NoError(t, attempt.Finish(t.Context(), err))
			var status string
			var diagnostic []byte
			require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT status,write_outcome FROM device_write_attempt WHERE queue_message_id=$1", message.ID).Scan(&status, &diagnostic))
			require.Equal(t, tc.status, status)
			var evidence map[string]string
			require.NoError(t, json.Unmarshal(diagnostic, &evidence))
			require.Equal(t, tc.outcome, evidence["outcome"], "a later refusal must not describe the whole attempt")
			_, err = service.Reserve(t.Context(), org, device)
			require.Equal(t, tc.status == "complete", err == nil)
		})
	}
}

func TestUncertaintyOverridesRefusalAndCompletionMarkers(t *testing.T) {
	for _, outcome := range []string{sdk.WriteNotDispatched, sdk.WriteCompleted} {
		err := devicewrite.UncertainDelivery(sdk.WithWriteOutcome(errors.New("accepted work may remain"), outcome, sdk.WriteStageDispatch, sdk.WriteReasonUnknown))
		require.True(t, devicewrite.IsUncertainDelivery(err))
		require.False(t, devicewrite.IsNotDispatched(err))
		require.False(t, devicewrite.IsCompleted(err))
	}
}
