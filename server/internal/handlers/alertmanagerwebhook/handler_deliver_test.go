package alertmanagerwebhook

import (
	"context"
	"encoding/json"
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

func (s *okStore) InsertBatch(_ context.Context, notifs []*notificationhistory.Notification) error {
	s.inserts += len(notifs)
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

// failBatchStore fails the atomic batch insert, to exercise the all-or-nothing persist path.
type failBatchStore struct{}

func (failBatchStore) Insert(context.Context, *notificationhistory.Notification) error { return nil }
func (failBatchStore) InsertBatch(context.Context, []*notificationhistory.Notification) error {
	return errors.New("batch insert failed")
}

// When the atomic batch fails, the handler must 500 (so Grafana retries) and never deliver.
func TestServeHTTP_BatchInsertFailureReturns500AndNoDelivery(t *testing.T) {
	deliverer := &captureDeliverer{}
	handler := NewHandler(failBatchStore{}, testWebhookToken, nil, deliverer)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, newAuthedRequest(t, shapedPayload()))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.False(t, deliverer.called, "no delivery when nothing persisted")
}

// Synthetic evaluation-failure alerts inherit a user rule's static org label
// but must persist org-less (tenant-invisible) and never fan out; a real alert
// merely named like one (no datasource_uid) keeps its org.
func TestBuildRowsStripsOrgFromSyntheticEvaluationAlerts(t *testing.T) {
	synthetic := alertmanagerAlert{Labels: map[string]string{
		labelAlertName:      "DatasourceError",
		labelOrganizationID: "7",
		"datasource_uid":    "protofleet-timescaledb",
	}}
	lookalike := alertmanagerAlert{Labels: map[string]string{
		labelAlertName:      "DatasourceError",
		labelOrganizationID: "7",
	}}

	rows, overflowed := buildRows([]alertmanagerAlert{synthetic, lookalike}, []int64{1, 2, 3})
	require.False(t, overflowed)
	require.Len(t, rows, 2)
	assert.Nil(t, rows[0].OrganizationID)
	require.NotNil(t, rows[1].OrganizationID)
	assert.Equal(t, int64(7), *rows[1].OrganizationID)
}

// buildRows bounds total expanded rows so many self-monitoring alerts can't amplify (via fan-out)
// into an unbounded write.
func TestBuildRowsCapsFanOutExpansion(t *testing.T) {
	orgIDs := make([]int64, maxFanOutOrgs)
	for i := range orgIDs {
		orgIDs[i] = int64(i + 1)
	}
	selfMon := func() alertmanagerAlert {
		return alertmanagerAlert{Labels: map[string]string{labelAlertName: "X", labelRuleGroup: ruleGroupSelfMonitoring}}
	}

	// One self-monitoring alert fans out to at most maxFanOutOrgs, without overflow.
	rows, overflowed := buildRows([]alertmanagerAlert{selfMon()}, orgIDs)
	require.False(t, overflowed)
	assert.Len(t, rows, maxFanOutOrgs)

	// Enough self-monitoring alerts to blow past maxPersistRows must overflow (→ handler rejects).
	n := maxPersistRows/maxFanOutOrgs + 2
	batch := make([]alertmanagerAlert, n)
	for i := range batch {
		batch[i] = selfMon()
	}
	rows, overflowed = buildRows(batch, orgIDs)
	require.True(t, overflowed)
	assert.LessOrEqual(t, len(rows), maxPersistRows)
}

// Batches over the alert-count cap are rejected before any row is built or persisted.
func TestServeHTTP_OverCapBatchRejected(t *testing.T) {
	store := &okStore{}
	handler := NewHandler(store, testWebhookToken, nil, &captureDeliverer{})

	alerts := make([]map[string]any, 0, maxAlertsPerRequest+1)
	for range maxAlertsPerRequest + 1 {
		alerts = append(alerts, map[string]any{
			"status": "firing",
			"labels": map[string]string{"alertname": "A", "organization_id": "1"},
		})
	}
	body, err := json.Marshal(map[string]any{"version": "4", "status": "firing", "alerts": alerts})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, newAuthedRequest(t, body))

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Equal(t, 0, store.inserts, "over-cap batch must not persist anything")
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
