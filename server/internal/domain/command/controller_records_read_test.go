package command

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/runtimepolicy"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

type controllerRecordsMiner struct {
	*containerMiningSiteMiner
	request sdk.ControllerRecordsRequest
	result  sdk.ControllerRecordsResult
	err     error
}

func (m *controllerRecordsMiner) ControllerRecords(_ context.Context, req sdk.ControllerRecordsRequest) (sdk.ControllerRecordsResult, error) {
	m.calls++
	m.request = req
	return m.result, m.err
}

func setupControllerRecordsTest(t *testing.T) (*containerMiningSiteTest, *controllerRecordsMiner) {
	t.Helper()
	f := setupContainerMiningSiteTest(t)
	miner := &controllerRecordsMiner{containerMiningSiteMiner: f.miner}
	f.execution.minerService = containerMiningSiteGetter{miner: miner}
	return f, miner
}

var controllerLogsQuery = sdk.ControllerRecordsRequest{Kind: sdk.ControllerRecordsKindLogs, ControllerID: "ct1-b", Logs: &sdk.ControllerLogQuery{Limit: 50, MinSeverity: sdk.ControllerLogWarn}}

func TestControllerRecordsReadAuthorizesAtCurrentSiteWithinServerBudget(t *testing.T) {
	f, miner := setupControllerRecordsTest(t)
	f.move(t, f.siteB)
	ctx := authn.SetInfo(t.Context(), f.session)
	before := time.Now()
	denied := errors.New("stop before controller I/O")
	var site *int64
	var deadline time.Time
	_, err := f.svc.ReadControllerRecords(ctx, f.identifier, controllerLogsQuery, func(ctx context.Context, siteID *int64) error {
		site = siteID
		deadline, _ = ctx.Deadline()
		return denied
	})
	require.ErrorIs(t, err, denied)
	require.NotNil(t, site)
	require.Equal(t, f.siteB, *site)
	require.WithinDuration(t, before.Add(runtimepolicy.ControllerRecordsReadTimeout), deadline, time.Second)
	require.Zero(t, miner.calls, "a denied caller never reaches the controller")

	miner.result = sdk.ControllerRecordsResult{ControllerID: "ct1-b", ControllerIDs: []string{"ct1-a", "ct1-b"}, Logs: &sdk.ControllerLogSnapshot{
		Entries:  []sdk.ControllerLogEntry{{Severity: sdk.ControllerLogError, Message: "pump fault"}},
		Coverage: sdk.ControllerLogRetained,
	}}
	result, err := f.svc.ReadControllerRecords(ctx, f.identifier, controllerLogsQuery, func(context.Context, *int64) error { return nil })
	require.NoError(t, err)
	require.Equal(t, miner.result, result)
	require.Equal(t, controllerLogsQuery, miner.request)
}

// Placement can change between the unlocked lookup and the locked re-read;
// authorization uses the site the locked re-read sees.
func TestControllerRecordsReadAuthorizesAtSiteSeenUnderLock(t *testing.T) {
	f, miner := setupControllerRecordsTest(t)
	f.move(t, f.siteA)
	f.svc.controllerRecordsTargetLookedUp = func() { f.move(t, f.siteB) }
	var site *int64
	_, err := f.svc.ReadControllerRecords(authn.SetInfo(t.Context(), f.session), f.identifier, controllerLogsQuery, func(_ context.Context, siteID *int64) error {
		site = siteID
		return errors.New("stop before controller I/O")
	})
	require.Error(t, err)
	require.NotNil(t, site)
	require.Equal(t, f.siteB, *site)
	require.Zero(t, miner.calls)
}

func TestControllerRecordsReadRejectsBadInputAndResults(t *testing.T) {
	f, miner := setupControllerRecordsTest(t)
	ctx := authn.SetInfo(t.Context(), f.session)
	allow := func(context.Context, *int64) error { return nil }

	_, err := f.svc.ReadControllerRecords(ctx, f.identifier, sdk.ControllerRecordsRequest{Kind: "sql"}, allow)
	require.True(t, fleeterror.IsInvalidArgumentError(err), "got %v", err)
	_, err = f.svc.ReadControllerRecords(ctx, f.identifier, controllerLogsQuery, nil)
	require.True(t, fleeterror.IsForbiddenError(err), "got %v", err)
	_, err = f.svc.ReadControllerRecords(ctx, "missing-container", controllerLogsQuery, allow)
	require.True(t, fleeterror.IsNotFoundError(err), "got %v", err)
	require.Zero(t, miner.calls)

	miner.err = fmt.Errorf("send: %w", control.ErrCommandProtocolUpgradeRequired)
	_, err = f.svc.ReadControllerRecords(ctx, f.identifier, controllerLogsQuery, allow)
	require.True(t, fleeterror.IsFailedPreconditionError(err), "got %v", err)
	require.ErrorContains(t, err, "Upgrade this container's Fleet Node")

	miner.err = nil
	miner.result = sdk.ControllerRecordsResult{Audit: &sdk.ControllerAuditPage{}}
	_, err = f.svc.ReadControllerRecords(ctx, f.identifier, controllerLogsQuery, allow)
	require.Error(t, err, "a result of the wrong kind is refused")
}
