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
	"github.com/block/proto-fleet/server/internal/domain/notificationhistory"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

type stubLister struct {
	rows []notificationhistory.StoredNotification
}

func (s stubLister) List(context.Context, int64, *int64, int32) ([]notificationhistory.StoredNotification, error) {
	return s.rows, nil
}

func (s stubLister) ListActive(context.Context, int64, int32) ([]notificationhistory.StoredNotification, error) {
	return s.rows, nil
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
	h := NewHandler(nil, stubLister{rows: []notificationhistory.StoredNotification{deviceAlertRow()}})

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
	h := NewHandler(nil, stubLister{rows: []notificationhistory.StoredNotification{sourceRow}})

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
	h := NewHandler(nil, stubLister{rows: []notificationhistory.StoredNotification{deviceAlertRow()}})

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
