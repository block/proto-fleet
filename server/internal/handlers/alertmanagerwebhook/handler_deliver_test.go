package alertmanagerwebhook

import (
	"context"
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

// A nil deliverer must be tolerated (delivery simply skipped).
func TestServeHTTP_NilDelivererStillPersists(t *testing.T) {
	store := &okStore{}
	handler := NewHandler(store, testWebhookToken, nil, nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, newAuthedRequest(t, shapedPayload()))

	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, 1, store.inserts)
}
