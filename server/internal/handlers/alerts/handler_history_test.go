package alerts

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	alertsv1 "github.com/block/proto-fleet/server/generated/grpc/alerts/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
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

// The rollup reports rule identity and counts only, so a viewer without miner:read reads the same rows a
// miner reader does — there is no device identity or rule-annotation free text in it to redact.
func TestListActiveAlertGroups_NeedsNoMinerRead(t *testing.T) {
	groups := []notificationhistory.ActiveAlertGroup{
		{AlertName: "MinerOffline", DeviceCount: 5_000, AlertCount: 5_000},
	}

	withoutMiner, err := NewHandler(nil, &stubLister{groups: groups}).ListActiveAlertGroups(
		ctxWithPerms(authz.PermAlertRead),
		connect.NewRequest(&alertsv1.ListActiveAlertGroupsRequest{}),
	)
	require.NoError(t, err)
	withMiner, err := NewHandler(nil, &stubLister{groups: groups}).ListActiveAlertGroups(
		ctxWithPerms(authz.PermAlertRead, authz.PermMinerRead),
		connect.NewRequest(&alertsv1.ListActiveAlertGroupsRequest{}),
	)
	require.NoError(t, err)

	require.Len(t, withoutMiner.Msg.Groups, 1)
	require.Equal(t, withMiner.Msg.Groups[0].AlertName, withoutMiner.Msg.Groups[0].AlertName)
	require.Equal(t, withMiner.Msg.Groups[0].DeviceCount, withoutMiner.Msg.Groups[0].DeviceCount)
	require.Equal(t, int64(5_000), withoutMiner.Msg.Groups[0].DeviceCount)
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
			RuleGroup:  "proto-fleet-defaults",
			PageSize:   25,
			BeforeId:   "0a1b2c3d4e5f60718293a4b5c6d7e8f9",
		}),
	)
	require.NoError(t, err)
	require.Len(t, resp.Msg.Alerts, 1)
	require.Equal(t, "MinerOffline", lister.lastFilter.AlertName)
	require.Equal(t, "proto-fleet-defaults", lister.lastFilter.RuleGroup)
	require.Equal(t, "0a1b2c3d4e5f60718293a4b5c6d7e8f9", lister.lastFilter.AfterKey)
	require.Equal(t, int32(26), lister.lastLimit)
}

// "" is the group of rules carrying no rule label, so a group without a name can't mean "any group": it would
// silently return the whole firing set where the caller asked for one rule's.
func TestListAlerts_ActiveRuleGroupWithoutAlertNameIsRejected(t *testing.T) {
	lister := &stubLister{rows: []notificationhistory.StoredNotification{deviceAlertRow()}}

	_, err := NewHandler(nil, lister).ListAlerts(
		ctxWithPerms(authz.PermAlertRead),
		connect.NewRequest(&alertsv1.ListAlertsRequest{ActiveOnly: true, RuleGroup: "proto-fleet-defaults"}),
	)
	require.True(t, fleeterror.IsInvalidArgumentError(err))
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
