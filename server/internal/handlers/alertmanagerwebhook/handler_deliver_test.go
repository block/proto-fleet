package alertmanagerwebhook

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	alertsdomain "github.com/block/proto-fleet/server/internal/domain/alerts"
	"github.com/block/proto-fleet/server/internal/domain/notificationhistory"
)

type okStore struct{ inserts int }

func (s *okStore) Insert(context.Context, *notificationhistory.Notification) error {
	s.inserts++
	return nil
}

type captureDeliverer struct {
	called bool
	got    []alertsdomain.Alert
}

func (c *captureDeliverer) Deliver(_ context.Context, alerts []alertsdomain.Alert) {
	c.called = true
	c.got = alerts
}

func TestServeHTTP_InvokesDelivererWithParsedAlerts(t *testing.T) {
	store := &okStore{}
	deliverer := &captureDeliverer{}
	handler := NewHandler(store, testWebhookToken, nil, deliverer)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, newAuthedRequest(t, shapedPayload()))

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.True(t, deliverer.called, "deliverer must run after history is stored")
	require.Len(t, deliverer.got, 1)
	assert.Equal(t, "7", deliverer.got[0].Labels["organization_id"])
	assert.Equal(t, "firing", deliverer.got[0].Status)
}

// failDeviceStore fails the insert for one device_id and records the rest, to exercise partial persistence.
type failDeviceStore struct{ failDevice string }

func (s *failDeviceStore) Insert(_ context.Context, n *notificationhistory.Notification) error {
	if n.DeviceID == s.failDevice {
		return errors.New("insert failed")
	}
	return nil
}

func TestServeHTTP_DeliversOnlyPersistedAlerts(t *testing.T) {
	deliverer := &captureDeliverer{}
	handler := NewHandler(&failDeviceStore{failDevice: "device-bad"}, testWebhookToken, nil, deliverer)

	payload := []byte(`{"status":"firing","alerts":[
		{"status":"firing","labels":{"alertname":"A","organization_id":"7","device_id":"device-ok"},"annotations":{},"fingerprint":"ok"},
		{"status":"firing","labels":{"alertname":"B","organization_id":"7","device_id":"device-bad"},"annotations":{},"fingerprint":"bad"}
	]}`)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, newAuthedRequest(t, payload))

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.True(t, deliverer.called)
	require.Len(t, deliverer.got, 1, "only the persisted alert is delivered")
	assert.Equal(t, "device-ok", deliverer.got[0].Labels["device_id"])
}

// A nil deliverer must be tolerated (delivery simply skipped).
func TestServeHTTP_NilDelivererStillPersists(t *testing.T) {
	store := &okStore{}
	handler := NewHandler(store, testWebhookToken, nil, nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, newAuthedRequest(t, shapedPayload()))

	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, 1, store.inserts)
}
