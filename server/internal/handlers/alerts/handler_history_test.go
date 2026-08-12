package alerts

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/protobuf/proto"

	alertsv1 "github.com/block/proto-fleet/server/generated/grpc/alerts/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/notificationhistory"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

type stubLister struct {
	rows       []notificationhistory.StoredNotification
	groups     []notificationhistory.ActiveAlertGroup
	lastFilter notificationhistory.ActiveAlertFilter
	lastLimit  int32
}

func (s *stubLister) List(_ context.Context, _ int64, _ *int64, limit int32) ([]notificationhistory.StoredNotification, error) {
	s.lastLimit = limit
	return s.rows, nil
}

func (s *stubLister) ListActive(_ context.Context, _ int64, limit int32) ([]notificationhistory.StoredNotification, error) {
	s.lastLimit = limit
	return s.rows, nil
}

func (s *stubLister) ListActiveByAlert(_ context.Context, _ int64, filter notificationhistory.ActiveAlertFilter) ([]notificationhistory.StoredNotification, error) {
	s.lastFilter, s.lastLimit = filter, filter.Limit
	return s.rows, nil
}

func (s *stubLister) ListActiveGroups(_ context.Context, _ int64, limit int32) ([]notificationhistory.ActiveAlertGroup, error) {
	s.lastLimit = limit
	return s.groups, nil
}

func ctxWithPerms(perms ...string) context.Context {
	ctx := authn.SetInfo(context.Background(), &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 1,
		Username:       "alice",
	})
	return middleware.WithEffectivePermissions(ctx, authz.NewEffectivePermissions([]authz.Assignment{
		{AssignmentID: 1, ScopeType: authz.ScopeOrg, Permissions: perms},
	}))
}

func deviceAlertRow() notificationhistory.StoredNotification {
	return notificationhistory.StoredNotification{
		ID:         1,
		ReceivedAt: time.Unix(1_700_000_000, 0),
		DeviceName: "Antminer S19",
		DeviceMAC:  "aa:bb:cc:dd:ee:ff",
		AlertKey:   "5f2b0c9a1d3e4f60718293a4b5c6d7e8",
		Notification: notificationhistory.Notification{
			AlertName: "MinerOffline",
			Status:    "firing",
			Severity:  "critical",
			DeviceID:  "device-42",
			Template:  "device_offline",
			Summary:   "Device device-42 is offline",
		},
	}
}

func TestListAlerts_RedactsMinerDataWithoutMinerRead(t *testing.T) {
	h := NewHandler(nil, &stubLister{rows: []notificationhistory.StoredNotification{deviceAlertRow()}})

	resp, err := h.ListAlerts(
		ctxWithPerms(authz.PermAlertRead),
		connect.NewRequest(&alertsv1.ListAlertsRequest{ActiveOnly: true}),
	)
	require.NoError(t, err)
	require.Len(t, resp.Msg.Alerts, 1)

	got := resp.Msg.Alerts[0]
	// Rule-level fields stay visible, including the template slug — the client
	// relies on it to classify source-level alerts even without miner:read.
	require.Equal(t, "MinerOffline", got.AlertName)
	require.Equal(t, "critical", got.Severity)
	require.Equal(t, "device_offline", got.Template)
	// Miner identity — including the free-text summary that names the device — is redacted.
	require.Empty(t, got.DeviceId)
	require.Empty(t, got.DeviceName)
	require.Empty(t, got.DeviceMac)
	require.Empty(t, got.Summary)
}

func TestListAlerts_SourceTemplateSummaryVisibleWithoutMinerRead(t *testing.T) {
	sourceRow := notificationhistory.StoredNotification{
		ID:         2,
		ReceivedAt: time.Unix(1_700_000_000, 0),
		Notification: notificationhistory.Notification{
			AlertName: "Curtailment Source Unreachable",
			Status:    "firing",
			Severity:  "critical",
			Template:  "mqtt-disconnected",
			Summary:   "Curtailment source maestro-a is disconnected; automatic curtailment is unavailable.",
		},
	}
	h := NewHandler(nil, &stubLister{rows: []notificationhistory.StoredNotification{sourceRow}})

	resp, err := h.ListAlerts(
		ctxWithPerms(authz.PermAlertRead),
		connect.NewRequest(&alertsv1.ListAlertsRequest{ActiveOnly: true}),
	)
	require.NoError(t, err)
	require.Len(t, resp.Msg.Alerts, 1)

	got := resp.Msg.Alerts[0]
	// The summary names only the MQTT source, so it stays visible; the UI
	// relies on it to distinguish which source is alerting.
	require.Equal(t, "mqtt-disconnected", got.Template)
	require.Equal(t, sourceRow.Summary, got.Summary)
	require.Empty(t, got.DeviceId)
	require.Empty(t, got.DeviceName)
	require.Empty(t, got.DeviceMac)
}

func TestListAlerts_IncludesMinerDataWithMinerRead(t *testing.T) {
	h := NewHandler(nil, &stubLister{rows: []notificationhistory.StoredNotification{deviceAlertRow()}})

	resp, err := h.ListAlerts(
		ctxWithPerms(authz.PermAlertRead, authz.PermMinerRead),
		connect.NewRequest(&alertsv1.ListAlertsRequest{ActiveOnly: true}),
	)
	require.NoError(t, err)
	require.Len(t, resp.Msg.Alerts, 1)

	got := resp.Msg.Alerts[0]
	require.Equal(t, "device-42", got.DeviceId)
	require.Equal(t, "Antminer S19", got.DeviceName)
	require.Equal(t, "aa:bb:cc:dd:ee:ff", got.DeviceMac)
	require.Equal(t, "Device device-42 is offline", got.Summary)
	require.Equal(t, "device_offline", got.Template)
}

func TestListActiveAlertGroups_ReturnsRollup(t *testing.T) {
	started := time.Unix(1_700_000_000, 0)
	lister := &stubLister{groups: []notificationhistory.ActiveAlertGroup{{
		AlertName:      "MinerOffline",
		RuleGroup:      "proto-fleet-defaults",
		Severity:       "critical",
		Template:       "device_offline",
		Summary:        "Device device-42 is offline",
		AlertCount:     5_000,
		DeviceCount:    5_000,
		FirstStartedAt: started,
	}}}
	h := NewHandler(nil, lister)

	resp, err := h.ListActiveAlertGroups(
		ctxWithPerms(authz.PermAlertRead),
		connect.NewRequest(&alertsv1.ListActiveAlertGroupsRequest{}),
	)
	require.NoError(t, err)
	require.Len(t, resp.Msg.Groups, 1)
	require.False(t, resp.Msg.HasMore)

	got := resp.Msg.Groups[0]
	require.Equal(t, "MinerOffline", got.AlertName)
	require.Equal(t, "proto-fleet-defaults", got.RuleGroup)
	require.Equal(t, int64(5_000), got.DeviceCount)
	require.Equal(t, int64(5_000), got.AlertCount)
	require.Equal(t, started.Unix(), got.FirstStartedAt.AsTime().Unix())
	// Over-fetch by one so a fleet past the cap is flagged rather than silently truncated.
	require.Equal(t, int32(activeGroupsMaxPageSize+1), lister.lastLimit)
}

// summary is free-text from rule annotations, so it follows the same miner:read gate as the drill-in rows.
func TestListActiveAlertGroups_RedactsSummaryWithoutMinerRead(t *testing.T) {
	groups := []notificationhistory.ActiveAlertGroup{
		{AlertName: "MinerOffline", Template: "device_offline", Summary: "Device device-42 is offline"},
		{AlertName: "Curtailment Source Unreachable", Template: "mqtt-disconnected", Summary: "Source maestro-a is unreachable"},
	}
	h := NewHandler(nil, &stubLister{groups: groups})

	resp, err := h.ListActiveAlertGroups(
		ctxWithPerms(authz.PermAlertRead),
		connect.NewRequest(&alertsv1.ListActiveAlertGroupsRequest{}),
	)
	require.NoError(t, err)
	require.Len(t, resp.Msg.Groups, 2)
	require.Empty(t, resp.Msg.Groups[0].Summary)
	// Source-level templates name the MQTT source, not a miner, so they stay visible.
	require.Equal(t, "Source maestro-a is unreachable", resp.Msg.Groups[1].Summary)

	withMiner, err := h.ListActiveAlertGroups(
		ctxWithPerms(authz.PermAlertRead, authz.PermMinerRead),
		connect.NewRequest(&alertsv1.ListActiveAlertGroupsRequest{}),
	)
	require.NoError(t, err)
	require.Equal(t, "Device device-42 is offline", withMiner.Msg.Groups[0].Summary)
}

func TestListActiveAlertGroups_FlagsMoreBeyondCap(t *testing.T) {
	groups := make([]notificationhistory.ActiveAlertGroup, activeGroupsMaxPageSize+1)
	h := NewHandler(nil, &stubLister{groups: groups})

	resp, err := h.ListActiveAlertGroups(
		ctxWithPerms(authz.PermAlertRead),
		connect.NewRequest(&alertsv1.ListActiveAlertGroupsRequest{}),
	)
	require.NoError(t, err)
	require.Len(t, resp.Msg.Groups, activeGroupsMaxPageSize)
	require.True(t, resp.Msg.HasMore)
}

func TestListActiveAlertGroups_RequiresAlertRead(t *testing.T) {
	h := NewHandler(nil, &stubLister{})

	_, err := h.ListActiveAlertGroups(
		ctxWithPerms(authz.PermMinerRead),
		connect.NewRequest(&alertsv1.ListActiveAlertGroupsRequest{}),
	)
	require.Error(t, err)
}

// The drill-in is unbounded during an outage, so it pages instead of returning the whole active set. Its
// cursor is the alert key, which a re-assert can't move — a row id can, so paging on one skips rows.
func TestListAlerts_ActiveAlertDrillInPagesOnAlertKey(t *testing.T) {
	lister := &stubLister{rows: []notificationhistory.StoredNotification{deviceAlertRow()}}
	h := NewHandler(nil, lister)

	resp, err := h.ListAlerts(
		ctxWithPerms(authz.PermAlertRead, authz.PermMinerRead),
		connect.NewRequest(&alertsv1.ListAlertsRequest{
			ActiveOnly: true,
			AlertName:  "MinerOffline",
			RuleGroup:  proto.String("proto-fleet-defaults"),
			PageSize:   25,
			BeforeId:   "0a1b2c3d4e5f60718293a4b5c6d7e8f9",
		}),
	)
	require.NoError(t, err)
	require.Len(t, resp.Msg.Alerts, 1)
	require.Equal(t, "MinerOffline", lister.lastFilter.AlertName)
	require.Equal(t, "proto-fleet-defaults", *lister.lastFilter.RuleGroup)
	require.Equal(t, "0a1b2c3d4e5f60718293a4b5c6d7e8f9", lister.lastFilter.AfterKey)
	require.Equal(t, int32(26), lister.lastLimit)
}

// Unfiltered active_only is the pre-drill-in current-state view: the whole firing set under the 200-row cap,
// with page_size and before_id ignored, so a client from before the drill-in still sees every firing alert.
func TestListAlerts_UnfilteredActiveIsUnpaged(t *testing.T) {
	lister := &stubLister{rows: []notificationhistory.StoredNotification{deviceAlertRow()}}

	resp, err := NewHandler(nil, lister).ListAlerts(
		ctxWithPerms(authz.PermAlertRead),
		connect.NewRequest(&alertsv1.ListAlertsRequest{ActiveOnly: true, PageSize: 1, BeforeId: "999"}),
	)
	require.NoError(t, err)
	require.Len(t, resp.Msg.Alerts, 1)
	require.Empty(t, lister.lastFilter.AfterKey)
	require.Equal(t, int32(historyMaxPageSize+1), lister.lastLimit)
	// No cursor: there is nothing for a caller to resume from, so an old client can't page itself into a gap.
	require.Empty(t, resp.Msg.NextCursor)
}

// The cursor comes off the last returned row, never the over-fetched one that only flags has_more.
func TestListAlerts_NextCursorEndsPageAndSet(t *testing.T) {
	second := deviceAlertRow()
	second.ID, second.AlertKey = 2, "aaaa0c9a1d3e4f60718293a4b5c6d7e8"
	overFetched := deviceAlertRow()
	overFetched.ID, overFetched.AlertKey = 3, "ffff0c9a1d3e4f60718293a4b5c6d7e8"
	rows := []notificationhistory.StoredNotification{deviceAlertRow(), second, overFetched}

	page, err := NewHandler(nil, &stubLister{rows: rows}).ListAlerts(
		ctxWithPerms(authz.PermAlertRead),
		connect.NewRequest(&alertsv1.ListAlertsRequest{ActiveOnly: true, AlertName: "MinerOffline", PageSize: 2}),
	)
	require.NoError(t, err)
	require.True(t, page.Msg.HasMore)
	require.Equal(t, second.AlertKey, page.Msg.NextCursor)

	// A final page advertises no cursor, so the client stops rather than re-reading the last row.
	last, err := NewHandler(nil, &stubLister{rows: rows[:1]}).ListAlerts(
		ctxWithPerms(authz.PermAlertRead),
		connect.NewRequest(&alertsv1.ListAlertsRequest{ActiveOnly: true, AlertName: "MinerOffline", PageSize: 2}),
	)
	require.NoError(t, err)
	require.False(t, last.Msg.HasMore)
	require.Empty(t, last.Msg.NextCursor)
}

// History pages on the row id, so its cursor is the last row's id rather than an alert key.
func TestListAlerts_HistoryNextCursorIsRowID(t *testing.T) {
	rows := []notificationhistory.StoredNotification{deviceAlertRow(), deviceAlertRow()}

	resp, err := NewHandler(nil, &stubLister{rows: rows}).ListAlerts(
		ctxWithPerms(authz.PermAlertRead),
		connect.NewRequest(&alertsv1.ListAlertsRequest{PageSize: 1}),
	)
	require.NoError(t, err)
	require.True(t, resp.Msg.HasMore)
	require.Equal(t, "1", resp.Msg.NextCursor)
}

// History keeps its numeric row-id cursor, so a malformed one is still rejected rather than read as page one.
func TestListAlerts_HistoryRejectsBadCursor(t *testing.T) {
	h := NewHandler(nil, &stubLister{})

	_, err := h.ListAlerts(
		ctxWithPerms(authz.PermAlertRead),
		connect.NewRequest(&alertsv1.ListAlertsRequest{BeforeId: "not-a-number"}),
	)
	require.Error(t, err)
}

// countingLister counts rollup loads and can hold them open, so a test can prove concurrent callers share one.
type countingLister struct {
	stubLister
	calls atomic.Int32
	gate  chan struct{}
}

func (c *countingLister) ListActiveGroups(_ context.Context, _ int64, _ int32) ([]notificationhistory.ActiveAlertGroup, error) {
	c.calls.Add(1)
	if c.gate != nil {
		<-c.gate
	}
	return c.groups, nil
}

func activeGroupFixture() []notificationhistory.ActiveAlertGroup {
	return []notificationhistory.ActiveAlertGroup{
		{AlertName: "MinerOffline", Template: "device_offline", Summary: "Device device-42 is offline", DeviceCount: 5_000},
	}
}

// The rollup is org-global and only moves when Alertmanager re-asserts, so concurrent dashboards share one
// aggregate rather than each driving a full scan of the org's firing set — the load that lands during an outage.
func TestListActiveAlertGroups_ConcurrentViewersShareOneLoad(t *testing.T) {
	lister := &countingLister{gate: make(chan struct{})}
	lister.groups = activeGroupFixture()
	h := NewHandler(nil, lister)

	const viewers = 8
	var wg sync.WaitGroup
	errs := make([]error, viewers)
	for i := range viewers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = h.ListActiveAlertGroups(
				ctxWithPerms(authz.PermAlertRead),
				connect.NewRequest(&alertsv1.ListActiveAlertGroupsRequest{}),
			)
		}()
	}
	// Let every viewer reach the cache before the load returns, so they queue on one slot rather than serialize.
	require.Eventually(t, func() bool { return lister.calls.Load() >= 1 }, time.Second, time.Millisecond)
	close(lister.gate)
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, int32(1), lister.calls.Load(), "eight concurrent viewers, one aggregate")
}

// A second poll inside the TTL is served from the snapshot rather than re-aggregating.
func TestListActiveAlertGroups_RepeatPollWithinTTLReusesSnapshot(t *testing.T) {
	lister := &countingLister{}
	lister.groups = activeGroupFixture()
	h := NewHandler(nil, lister)

	for range 3 {
		_, err := h.ListActiveAlertGroups(
			ctxWithPerms(authz.PermAlertRead),
			connect.NewRequest(&alertsv1.ListActiveAlertGroupsRequest{}),
		)
		require.NoError(t, err)
	}
	assert.Equal(t, int32(1), lister.calls.Load())
}

// The cache holds the store's rows, not the response: two callers sharing one snapshot must still be redacted
// against their own miner:read grant, or the cache would leak device detail to a caller denied it.
func TestListActiveAlertGroups_SharedSnapshotRedactsPerCaller(t *testing.T) {
	lister := &countingLister{}
	lister.groups = activeGroupFixture()
	h := NewHandler(nil, lister)

	withMiner, err := h.ListActiveAlertGroups(
		ctxWithPerms(authz.PermAlertRead, authz.PermMinerRead),
		connect.NewRequest(&alertsv1.ListActiveAlertGroupsRequest{}),
	)
	require.NoError(t, err)
	withoutMiner, err := h.ListActiveAlertGroups(
		ctxWithPerms(authz.PermAlertRead),
		connect.NewRequest(&alertsv1.ListActiveAlertGroupsRequest{}),
	)
	require.NoError(t, err)

	assert.Equal(t, int32(1), lister.calls.Load(), "both callers served from one aggregate")
	assert.Equal(t, "Device device-42 is offline", withMiner.Msg.Groups[0].Summary)
	assert.Empty(t, withoutMiner.Msg.Groups[0].Summary, "a shared snapshot must not carry another caller's grant")
}
