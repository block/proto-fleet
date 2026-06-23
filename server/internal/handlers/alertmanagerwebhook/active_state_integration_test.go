package alertmanagerwebhook

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/notificationhistory"
)

// orgIDPtr makes an org id addressable for Notification.OrganizationID.
func orgIDPtr(v int64) *int64 { return &v }

// insertEvent appends one alert event, firing the notification_active sync trigger.
func insertEvent(t *testing.T, h *dbHarness, n notificationhistory.Notification) {
	t.Helper()
	require.NoError(t, h.store.Insert(t.Context(), &n))
}

// activeKeys returns "alertName/deviceID" for each currently-active row, asserting all are firing.
func activeKeys(t *testing.T, h *dbHarness, orgID int64) []string {
	t.Helper()
	lister, ok := h.store.(notificationhistory.Lister)
	require.True(t, ok)
	rows, err := lister.ListActive(t.Context(), orgID, 100)
	require.NoError(t, err)
	keys := make([]string, 0, len(rows))
	for _, r := range rows {
		require.Equal(t, "firing", r.Status)
		keys = append(keys, r.AlertName+"/"+r.DeviceID)
	}
	return keys
}

// A firing event populates notification_active, a resolved event clears it, and a re-fire restores it.
func TestNotificationActiveTrigger_FiringResolvedLifecycle(t *testing.T) {
	h := newDBHarness(t)
	const org = int64(7)

	insertEvent(t, h, notificationhistory.Notification{
		AlertName: "DeviceOffline", Status: "firing", Fingerprint: "fp1",
		OrganizationID: orgIDPtr(org), DeviceID: "device-1", Summary: "Device device-1 is offline",
	})
	require.Equal(t, []string{"DeviceOffline/device-1"}, activeKeys(t, h, org))

	insertEvent(t, h, notificationhistory.Notification{
		AlertName: "DeviceOffline", Status: "resolved", Fingerprint: "fp1",
		OrganizationID: orgIDPtr(org), DeviceID: "device-1",
	})
	require.Empty(t, activeKeys(t, h, org))

	insertEvent(t, h, notificationhistory.Notification{
		AlertName: "DeviceOffline", Status: "firing", Fingerprint: "fp1",
		OrganizationID: orgIDPtr(org), DeviceID: "device-1",
	})
	require.Equal(t, []string{"DeviceOffline/device-1"}, activeKeys(t, h, org))
}

// Fingerprintless alerts on the same rule must key per device, not collapse into one active row.
func TestNotificationActiveTrigger_FingerprintlessKeyedPerDevice(t *testing.T) {
	h := newDBHarness(t)
	const org = int64(8)

	insertEvent(t, h, notificationhistory.Notification{
		AlertName: "DeviceOffline", Status: "firing",
		OrganizationID: orgIDPtr(org), DeviceID: "device-1",
	})
	insertEvent(t, h, notificationhistory.Notification{
		AlertName: "DeviceOffline", Status: "firing",
		OrganizationID: orgIDPtr(org), DeviceID: "device-2",
	})
	require.Len(t, activeKeys(t, h, org), 2)

	// Resolving one device leaves the other firing.
	insertEvent(t, h, notificationhistory.Notification{
		AlertName: "DeviceOffline", Status: "resolved",
		OrganizationID: orgIDPtr(org), DeviceID: "device-1",
	})
	require.Equal(t, []string{"DeviceOffline/device-2"}, activeKeys(t, h, org))
}

// Unscoped (NULL org) alerts are not tracked as active state.
func TestNotificationActiveTrigger_SkipsUnscopedAlerts(t *testing.T) {
	h := newDBHarness(t)

	insertEvent(t, h, notificationhistory.Notification{
		AlertName: "MetricIngestStalled", Status: "firing", Fingerprint: "fp-self",
		DeviceID: "device-1",
	})
	require.Empty(t, activeKeys(t, h, 0))
}
