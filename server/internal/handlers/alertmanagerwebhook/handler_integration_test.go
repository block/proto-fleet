package alertmanagerwebhook

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/notificationhistory"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

const testWebhookToken = "test-webhook-token"

// shapedPayload returns the canonical Grafana/Alertmanager v4 envelope the
// receiver emits when the bundled DeviceOffline rule fires for one device.
func shapedPayload() []byte {
	return []byte(`{
		"version": "4",
		"groupKey": "{}:{alertname=\"DeviceOffline\"}",
		"truncatedAlerts": 0,
		"status": "firing",
		"receiver": "protofleet-internal",
		"groupLabels": {"alertname": "DeviceOffline"},
		"commonLabels": {"alertname": "DeviceOffline", "severity": "warning"},
		"commonAnnotations": {"summary": "Device device-42 is offline"},
		"externalURL": "http://grafana:3000",
		"alerts": [
			{
				"status": "firing",
				"labels": {
					"alertname": "DeviceOffline",
					"organization_id": "7",
					"device_id": "device-42",
					"severity": "warning",
					"rule_group": "proto-fleet-defaults",
					"template": "offline"
				},
				"annotations": {
					"summary": "Device device-42 is offline",
					"description": "Device device-42 has been reporting fleet_device_online=0 for at least five minutes."
				},
				"startsAt": "2026-05-20T12:34:56Z",
				"endsAt": "0001-01-01T00:00:00Z",
				"fingerprint": "abc123"
			}
		]
	}`)
}

// selfMonitoringPayload returns the canonical shape Grafana emits for the
// "Metric Ingest Stalled" rule: rule_group=proto-fleet-self and no
// organization_id label, so the receiver fans out per active org.
func selfMonitoringPayload() []byte {
	return []byte(`{
		"version": "4",
		"status": "firing",
		"alerts": [
			{
				"status": "firing",
				"labels": {
					"alertname": "Metric Ingest Stalled",
					"severity": "critical",
					"rule_group": "proto-fleet-self",
					"component": "metric-ingest"
				},
				"annotations": {
					"summary": "Proto Fleet metric ingest has stalled."
				},
				"startsAt": "2026-05-20T12:34:56Z",
				"endsAt": "0001-01-01T00:00:00Z",
				"fingerprint": "ingest-stalled-1"
			}
		]
	}`)
}

// dbHarness binds a freshly-migrated test database, the SQL store under
// test, and a few helpers to assert rows landed where expected.
type dbHarness struct {
	db    *sql.DB
	store notificationhistory.Store
}

// newDBHarness skips the test in `-short` mode (mirrors every other
// integration test in the tree), spins up a per-test database via
// testutil, and returns a real SQLNotificationHistoryStore wired to it.
func newDBHarness(t *testing.T) *dbHarness {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	return &dbHarness{db: db, store: sqlstores.NewSQLNotificationHistoryStore(db)}
}

// countRows reports the number of rows in notification_history. Used to
// assert "nothing was written" without enumerating columns.
func (h *dbHarness) countRows(t *testing.T) int {
	t.Helper()
	var n int
	require.NoError(t, h.db.QueryRowContext(t.Context(), `SELECT count(*) FROM notification_history`).Scan(&n))
	return n
}

// rowSnapshot is a flattened view of a notification_history row used by
// assertions. NullInt64 keeps the unscoped-row case representable.
type rowSnapshot struct {
	AlertName      string
	Status         string
	Severity       string
	RuleGroup      string
	Fingerprint    string
	OrganizationID sql.NullInt64
	DeviceID       string
	Template       string
	Summary        string
}

// fetchRows returns every notification_history row ordered by id so
// assertions can rely on insertion order within a single request.
func (h *dbHarness) fetchRows(t *testing.T) []rowSnapshot {
	t.Helper()
	rows, err := h.db.QueryContext(t.Context(), `
		SELECT alert_name, status, severity, rule_group, fingerprint,
		       organization_id, device_id, template, summary
		FROM notification_history
		ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()

	var out []rowSnapshot
	for rows.Next() {
		var r rowSnapshot
		require.NoError(t, rows.Scan(
			&r.AlertName, &r.Status, &r.Severity, &r.RuleGroup, &r.Fingerprint,
			&r.OrganizationID, &r.DeviceID, &r.Template, &r.Summary,
		))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

// errInjectingStore wraps a real Store and returns queued errors in
// order for the first len(errs) calls; subsequent calls pass through to
// the underlying store. It lets failure-path tests exercise the real DB
// for successful writes while still simulating transient db errors
// without dropping the table or closing the connection mid-test.
type errInjectingStore struct {
	mu    sync.Mutex
	inner notificationhistory.Store
	errs  []error
}

func (s *errInjectingStore) Insert(ctx context.Context, n *notificationhistory.Notification) error {
	s.mu.Lock()
	var injected error
	hasInjected := false
	if len(s.errs) > 0 {
		injected, s.errs = s.errs[0], s.errs[1:]
		hasInjected = true
	}
	s.mu.Unlock()
	if hasInjected && injected != nil {
		return injected
	}
	return s.inner.Insert(ctx, n)
}

// stubOrgLister is a deterministic OrgLister for tests.
type stubOrgLister struct {
	ids []int64
	err error
}

func (s stubOrgLister) ListActiveOrganizationIDs(context.Context) ([]int64, error) {
	return s.ids, s.err
}

// newAuthedRequest builds a POST to the webhook path with the test
// bearer credential attached.
func newAuthedRequest(t *testing.T, body []byte) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, Path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testWebhookToken)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// happy path: Grafana-shaped firing payload lands as one notification_history row.
func TestServeHTTP_FiringPayloadPersistsNotification(t *testing.T) {
	h := newDBHarness(t)
	handler := NewHandler(h.store, testWebhookToken, nil)

	req := newAuthedRequest(t, shapedPayload())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	rows := h.fetchRows(t)
	require.Len(t, rows, 1)
	row := rows[0]
	require.Equal(t, "DeviceOffline", row.AlertName)
	require.Equal(t, "firing", row.Status)
	require.Equal(t, "warning", row.Severity)
	require.Equal(t, "proto-fleet-defaults", row.RuleGroup)
	require.Equal(t, "abc123", row.Fingerprint)
	require.True(t, row.OrganizationID.Valid)
	require.Equal(t, int64(7), row.OrganizationID.Int64)
	require.Equal(t, "device-42", row.DeviceID)
	require.Equal(t, "offline", row.Template)
	require.Equal(t, "Device device-42 is offline", row.Summary)
}

// resolved alerts persist with status=resolved.
func TestServeHTTP_ResolvedPayloadRecordsStatus(t *testing.T) {
	h := newDBHarness(t)
	handler := NewHandler(h.store, testWebhookToken, nil)

	resolved := strings.ReplaceAll(string(shapedPayload()), `"status": "firing"`, `"status": "resolved"`)

	req := newAuthedRequest(t, []byte(resolved))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	rows := h.fetchRows(t)
	require.Len(t, rows, 1)
	require.Equal(t, "resolved", rows[0].Status)
}

// non-POST is rejected and never reaches the DB. Method check runs
// before auth so Grafana operators get a useful Allow header back.
func TestServeHTTP_NonPostRejected(t *testing.T) {
	h := newDBHarness(t)
	handler := NewHandler(h.store, testWebhookToken, nil)

	req := httptest.NewRequest(http.MethodGet, Path, nil)
	req.Header.Set("Authorization", "Bearer "+testWebhookToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	require.Equal(t, http.MethodPost, rec.Header().Get("Allow"))
	require.Equal(t, 0, h.countRows(t))
}

// missing Authorization → 401, no DB writes.
func TestServeHTTP_MissingAuthorizationRejected(t *testing.T) {
	h := newDBHarness(t)
	handler := NewHandler(h.store, testWebhookToken, nil)

	req := httptest.NewRequest(http.MethodPost, Path, bytes.NewReader(shapedPayload()))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, 0, h.countRows(t))
}

// wrong Bearer credential → 401.
func TestServeHTTP_WrongTokenRejected(t *testing.T) {
	h := newDBHarness(t)
	handler := NewHandler(h.store, testWebhookToken, nil)

	req := httptest.NewRequest(http.MethodPost, Path, bytes.NewReader(shapedPayload()))
	req.Header.Set("Authorization", "Bearer not-the-secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, 0, h.countRows(t))
}

// a non-Bearer scheme is rejected even when the credential matches.
func TestServeHTTP_NonBearerSchemeRejected(t *testing.T) {
	h := newDBHarness(t)
	handler := NewHandler(h.store, testWebhookToken, nil)

	req := httptest.NewRequest(http.MethodPost, Path, bytes.NewReader(shapedPayload()))
	req.Header.Set("Authorization", "Token "+testWebhookToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, 0, h.countRows(t))
}

// an unconfigured handler (empty token) must refuse every request, including
// an "empty Bearer" — empty-equals-empty would otherwise round-trip true.
func TestServeHTTP_UnconfiguredHandlerRejectsEverything(t *testing.T) {
	h := newDBHarness(t)
	handler := NewHandler(h.store, "", nil)

	req := httptest.NewRequest(http.MethodPost, Path, bytes.NewReader(shapedPayload()))
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, 0, h.countRows(t))
}

// malformed JSON → 400, no DB writes.
func TestServeHTTP_InvalidJSONRejected(t *testing.T) {
	h := newDBHarness(t)
	handler := NewHandler(h.store, testWebhookToken, nil)

	req := newAuthedRequest(t, []byte("not json"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, 0, h.countRows(t))
}

// empty batch is ack'd without writing.
func TestServeHTTP_EmptyBatchAcked(t *testing.T) {
	h := newDBHarness(t)
	handler := NewHandler(h.store, testWebhookToken, nil)

	req := newAuthedRequest(t, []byte(`{"version":"4","status":"firing","alerts":[]}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, 0, h.countRows(t))
}

// partial store failure within a batch must not block the rest — as long
// as one row lands, the handler acks 204 so Grafana doesn't retry the
// rows that did succeed.
func TestServeHTTP_PartialStoreFailureKeepsBatchProgressing(t *testing.T) {
	h := newDBHarness(t)
	store := &errInjectingStore{inner: h.store, errs: []error{errors.New("transient db error")}}
	handler := NewHandler(store, testWebhookToken, nil)

	body := []byte(`{
		"version": "4",
		"status": "firing",
		"alerts": [
			{"status":"firing","labels":{"alertname":"A","organization_id":"1"}},
			{"status":"firing","labels":{"alertname":"B","organization_id":"1"}}
		]
	}`)

	req := newAuthedRequest(t, body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	// Only the second alert reaches the DB — the first errored before insert.
	rows := h.fetchRows(t)
	require.Len(t, rows, 1)
	require.Equal(t, "B", rows[0].AlertName)
}

// every alert in the batch failing to persist → 5xx so Grafana retries.
func TestServeHTTP_AllStoreFailuresReturn5xx(t *testing.T) {
	h := newDBHarness(t)
	store := &errInjectingStore{inner: h.store, errs: []error{errors.New("transient db error"), errors.New("transient db error")}}
	handler := NewHandler(store, testWebhookToken, nil)

	body := []byte(`{
		"version": "4",
		"status": "firing",
		"alerts": [
			{"status":"firing","labels":{"alertname":"A","organization_id":"1"}},
			{"status":"firing","labels":{"alertname":"B","organization_id":"1"}}
		]
	}`)

	req := newAuthedRequest(t, body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, 0, h.countRows(t))
}

// unknown JSON fields are accepted so a future Grafana version with new
// envelope keys doesn't break the receiver.
func TestServeHTTP_AcceptsUnknownFields(t *testing.T) {
	h := newDBHarness(t)
	handler := NewHandler(h.store, testWebhookToken, nil)

	body := []byte(`{
		"version": "5-future",
		"futureField": "ignored",
		"status": "firing",
		"alerts": [
			{
				"status": "firing",
				"futureAlertField": 123,
				"labels": {"alertname": "DeviceOffline"}
			}
		]
	}`)

	req := newAuthedRequest(t, body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, 1, h.countRows(t))
}

// self-monitoring fan-out → one row per active org.
func TestServeHTTP_SelfMonitoringFansOutToAllOrgs(t *testing.T) {
	h := newDBHarness(t)
	handler := NewHandler(h.store, testWebhookToken, stubOrgLister{ids: []int64{1, 2, 5}})

	req := newAuthedRequest(t, selfMonitoringPayload())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	rows := h.fetchRows(t)
	require.Len(t, rows, 3)
	got := make([]int64, len(rows))
	for i, row := range rows {
		require.True(t, row.OrganizationID.Valid, "fan-out row must be org-scoped")
		require.Equal(t, "Metric Ingest Stalled", row.AlertName)
		require.Equal(t, "firing", row.Status)
		got[i] = row.OrganizationID.Int64
	}
	require.ElementsMatch(t, []int64{1, 2, 5}, got)
}

// lister error falls back to a single unscoped row — visibility degrades
// but the critical signal still lands.
func TestServeHTTP_SelfMonitoringFallsBackOnListerError(t *testing.T) {
	h := newDBHarness(t)
	handler := NewHandler(h.store, testWebhookToken, stubOrgLister{err: errors.New("db down")})

	req := newAuthedRequest(t, selfMonitoringPayload())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	rows := h.fetchRows(t)
	require.Len(t, rows, 1)
	require.False(t, rows[0].OrganizationID.Valid, "fallback row stays unscoped — fan-out couldn't run")
	require.Equal(t, "Metric Ingest Stalled", rows[0].AlertName)
}

// no active orgs → single unscoped row.
func TestServeHTTP_SelfMonitoringNoActiveOrgsFallsBackToUnscoped(t *testing.T) {
	h := newDBHarness(t)
	handler := NewHandler(h.store, testWebhookToken, stubOrgLister{ids: nil})

	req := newAuthedRequest(t, selfMonitoringPayload())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	rows := h.fetchRows(t)
	require.Len(t, rows, 1)
	require.False(t, rows[0].OrganizationID.Valid)
}

// unscoped non-self-monitoring alerts keep the single-row behaviour —
// fan-out is opt-in via rule_group.
func TestServeHTTP_UnscopedNonSelfAlertDoesNotFanOut(t *testing.T) {
	h := newDBHarness(t)
	handler := NewHandler(h.store, testWebhookToken, stubOrgLister{ids: []int64{1, 2, 3}})

	body := []byte(`{
		"version": "4",
		"status": "firing",
		"alerts": [
			{
				"status": "firing",
				"labels": {"alertname": "MysteryUnscopedAlert"}
			}
		]
	}`)

	req := newAuthedRequest(t, body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, 1, h.countRows(t))
}

// fan-out tolerates partial Insert failure the same way a regular batch does.
func TestServeHTTP_SelfMonitoringFanOutToleratesPartialFailure(t *testing.T) {
	h := newDBHarness(t)
	store := &errInjectingStore{inner: h.store, errs: []error{nil, errors.New("transient db error"), nil}}
	handler := NewHandler(store, testWebhookToken, stubOrgLister{ids: []int64{1, 2, 3}})

	req := newAuthedRequest(t, selfMonitoringPayload())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	// 2 of the 3 fan-out rows landed: org 1 (nil → real insert), org 2
	// (errored before insert), org 3 (nil → real insert).
	rows := h.fetchRows(t)
	require.Len(t, rows, 2)
	got := []int64{rows[0].OrganizationID.Int64, rows[1].OrganizationID.Int64}
	require.ElementsMatch(t, []int64{1, 3}, got)
}

// every fan-out Insert failing with no other alerts → 5xx so Grafana retries.
func TestServeHTTP_SelfMonitoringFanOutAllFailuresReturn5xx(t *testing.T) {
	h := newDBHarness(t)
	store := &errInjectingStore{inner: h.store, errs: []error{errors.New("db down"), errors.New("db down")}}
	handler := NewHandler(store, testWebhookToken, stubOrgLister{ids: []int64{1, 2}})

	req := newAuthedRequest(t, selfMonitoringPayload())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, 0, h.countRows(t))
}

// batches above the per-request alert cap are rejected with 413 and never
// reach the DB.
func TestServeHTTP_TooManyAlertsRejected(t *testing.T) {
	h := newDBHarness(t)
	handler := NewHandler(h.store, testWebhookToken, nil)

	alerts := make([]map[string]any, 0, maxAlertsPerRequest+1)
	for i := 0; i <= maxAlertsPerRequest; i++ {
		alerts = append(alerts, map[string]any{
			"status": "firing",
			"labels": map[string]string{"alertname": "A", "organization_id": "1"},
		})
	}
	body, err := json.Marshal(map[string]any{
		"version": "4",
		"status":  "firing",
		"alerts":  alerts,
	})
	require.NoError(t, err)

	req := newAuthedRequest(t, body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Equal(t, 0, h.countRows(t))
}

// a batch sitting exactly at the per-request alert cap still passes.
func TestServeHTTP_AtAlertCapStillAccepted(t *testing.T) {
	h := newDBHarness(t)
	handler := NewHandler(h.store, testWebhookToken, nil)

	alerts := make([]map[string]any, 0, maxAlertsPerRequest)
	for range maxAlertsPerRequest {
		alerts = append(alerts, map[string]any{
			"status": "firing",
			"labels": map[string]string{"alertname": "A", "organization_id": "1"},
		})
	}
	body, err := json.Marshal(map[string]any{
		"version": "4",
		"status":  "firing",
		"alerts":  alerts,
	})
	require.NoError(t, err)

	req := newAuthedRequest(t, body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, maxAlertsPerRequest, h.countRows(t))
}

// fan-out is capped at the per-request row budget; remaining orgs drop.
// Grafana keeps re-firing the rule, so truncation isn't a permanent silence.
func TestServeHTTP_SelfMonitoringFanOutTruncatesAtRowCap(t *testing.T) {
	h := newDBHarness(t)
	orgIDs := make([]int64, maxRowsPerRequest+5)
	for i := range orgIDs {
		orgIDs[i] = int64(i + 1)
	}
	handler := NewHandler(h.store, testWebhookToken, stubOrgLister{ids: orgIDs})

	req := newAuthedRequest(t, selfMonitoringPayload())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, maxRowsPerRequest, h.countRows(t))
}

// per-request row budget is shared across the whole batch: once an early
// alert exhausts it via fan-out, later alerts contribute nothing.
func TestServeHTTP_RowBudgetSharedAcrossBatch(t *testing.T) {
	h := newDBHarness(t)
	orgIDs := make([]int64, maxRowsPerRequest)
	for i := range orgIDs {
		orgIDs[i] = int64(i + 1)
	}
	handler := NewHandler(h.store, testWebhookToken, stubOrgLister{ids: orgIDs})

	body := []byte(`{
		"version": "4",
		"status": "firing",
		"alerts": [
			{
				"status": "firing",
				"labels": {
					"alertname": "Metric Ingest Stalled",
					"rule_group": "proto-fleet-self"
				}
			},
			{
				"status": "firing",
				"labels": {
					"alertname": "Metric Ingest Stalled 2",
					"rule_group": "proto-fleet-self"
				}
			}
		]
	}`)

	req := newAuthedRequest(t, body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, maxRowsPerRequest, h.countRows(t))
}

// payloads larger than the body cap return 413 and never touch the DB.
func TestServeHTTP_OversizedBodyRejected(t *testing.T) {
	h := newDBHarness(t)
	handler := NewHandler(h.store, testWebhookToken, nil)

	junk := bytes.Repeat([]byte("a"), maxBodyBytes+1024)
	body, err := json.Marshal(map[string]any{
		"version": "4",
		"status":  "firing",
		"alerts": []map[string]any{
			{
				"status": "firing",
				"labels": map[string]string{"alertname": "Big", "padding": string(junk)},
			},
		},
	})
	require.NoError(t, err)

	req := newAuthedRequest(t, body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Equal(t, 0, h.countRows(t))
}
