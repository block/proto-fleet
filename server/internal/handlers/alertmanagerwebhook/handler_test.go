package alertmanagerwebhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	"github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

// shapedPayload returns the canonical Grafana/Alertmanager v4 envelope
// that the webhook receiver emits when the bundled DeviceOffline rule
// fires for one device. It's the body shape the handler must accept —
// the test asserts that POSTing this payload lands an activity row.
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

const testWebhookToken = "test-webhook-token"

// stubOrgLister is a deterministic OrgLister for tests. ids drives the
// happy path; err lets a test cover the lister-error fall-back where
// the handler should still record the alert as a single unscoped row.
type stubOrgLister struct {
	ids []int64
	err error
}

func (s stubOrgLister) ListActiveOrganizationIDs(context.Context) ([]int64, error) {
	return s.ids, s.err
}

// newTestHandler wires a handler with a nil OrgLister — the historic
// shape, exercised by every pre-existing test (org-scoped alerts,
// auth/validation paths, store-failure semantics). Tests that exercise
// the self-monitoring fan-out use newTestHandlerWithOrgs instead.
func newTestHandler(t *testing.T) (http.Handler, *mocks.MockActivityStore) {
	t.Helper()
	ctrl := gomock.NewController(t)
	store := mocks.NewMockActivityStore(ctrl)
	svc := activity.NewService(store)
	return NewHandler(svc, testWebhookToken, nil), store
}

// newTestHandlerWithOrgs wires a handler with a stub OrgLister so tests
// can assert that unscoped self-monitoring alerts fan out to one
// activity row per org (or fall back cleanly on lister errors).
func newTestHandlerWithOrgs(t *testing.T, lister OrgLister) (http.Handler, *mocks.MockActivityStore) {
	t.Helper()
	ctrl := gomock.NewController(t)
	store := mocks.NewMockActivityStore(ctrl)
	svc := activity.NewService(store)
	return NewHandler(svc, testWebhookToken, lister), store
}

// newAuthedRequest builds a POST to the webhook path with the test
// bearer credential attached, mirroring what Grafana's contact point
// emits when `authorization_scheme: Bearer` is provisioned.
func newAuthedRequest(t *testing.T, body []byte) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, Path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testWebhookToken)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// the happy path: a Grafana-shaped firing payload is decoded, mapped to
// an activity event with the system actor + system category, and
// inserted via the activity store.
func TestServeHTTP_FiringPayloadPersistsActivity(t *testing.T) {
	h, store := newTestHandler(t)

	var captured models.Event
	store.EXPECT().
		Insert(gomock.Any(), gomock.AssignableToTypeOf(&models.Event{})).
		DoAndReturn(func(_ context.Context, e *models.Event) error {
			captured = *e
			return nil
		}).
		Times(1)

	req := newAuthedRequest(t, shapedPayload())
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, models.CategorySystem, captured.Category)
	require.Equal(t, "alert.DeviceOffline.firing", captured.Type)
	require.Equal(t, models.ActorSystem, captured.ActorType)
	require.Equal(t, models.ResultFailure, captured.Result)
	require.NotNil(t, captured.OrganizationID)
	require.Equal(t, int64(7), *captured.OrganizationID)
	require.NotNil(t, captured.ScopeType)
	require.Equal(t, "offline", *captured.ScopeType)
	require.NotNil(t, captured.ScopeLabel)
	require.Equal(t, "device-42", *captured.ScopeLabel)
	require.Equal(t, "Device device-42 is offline", captured.Description)

	require.NotNil(t, captured.Metadata)
	require.Equal(t, "firing", captured.Metadata["status"])
	require.Equal(t, "abc123", captured.Metadata["fingerprint"])
}

// a resolved alert flips Result to success, mirroring the way resolved
// rows appear elsewhere in the activity feed.
func TestServeHTTP_ResolvedPayloadMarksSuccess(t *testing.T) {
	h, store := newTestHandler(t)

	resolved := strings.ReplaceAll(string(shapedPayload()), `"status": "firing"`, `"status": "resolved"`)

	var captured models.Event
	store.EXPECT().
		Insert(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, e *models.Event) error {
			captured = *e
			return nil
		}).
		Times(1)

	req := newAuthedRequest(t, []byte(resolved))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, "alert.DeviceOffline.resolved", captured.Type)
	require.Equal(t, models.ResultSuccess, captured.Result)
}

// non-POST methods are rejected and never reach the store. The method
// check must run before the auth check so the Allow header is still
// useful to a Grafana operator probing the endpoint manually.
func TestServeHTTP_NonPostRejected(t *testing.T) {
	h, store := newTestHandler(t)
	store.EXPECT().Insert(gomock.Any(), gomock.Any()).Times(0)

	req := httptest.NewRequest(http.MethodGet, Path, nil)
	req.Header.Set("Authorization", "Bearer "+testWebhookToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	require.Equal(t, http.MethodPost, rec.Header().Get("Allow"))
}

// a POST without an Authorization header is rejected with 401 and
// never reaches the body parser or the store. This is the LAN-attacker
// guard — any unauthenticated client on the api-proxy path can hit the
// receiver, but they cannot forge activity rows without the secret.
func TestServeHTTP_MissingAuthorizationRejected(t *testing.T) {
	h, store := newTestHandler(t)
	store.EXPECT().Insert(gomock.Any(), gomock.Any()).Times(0)

	req := httptest.NewRequest(http.MethodPost, Path, bytes.NewReader(shapedPayload()))
	req.Header.Set("Content-Type", "application/json")
	// no Authorization header set
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

// A wrong credential is rejected even when the Bearer scheme is
// present. The check uses constant-time comparison; this test asserts
// the outcome, not the timing.
func TestServeHTTP_WrongTokenRejected(t *testing.T) {
	h, store := newTestHandler(t)
	store.EXPECT().Insert(gomock.Any(), gomock.Any()).Times(0)

	req := httptest.NewRequest(http.MethodPost, Path, bytes.NewReader(shapedPayload()))
	req.Header.Set("Authorization", "Bearer not-the-secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

// A non-Bearer scheme (Basic, Token, etc.) is rejected even if the
// credential payload happens to match the token. Keeps the contract
// tight: Grafana emits Bearer credentials only when configured that
// way, so anything else is suspicious.
func TestServeHTTP_NonBearerSchemeRejected(t *testing.T) {
	h, store := newTestHandler(t)
	store.EXPECT().Insert(gomock.Any(), gomock.Any()).Times(0)

	req := httptest.NewRequest(http.MethodPost, Path, bytes.NewReader(shapedPayload()))
	req.Header.Set("Authorization", "Token "+testWebhookToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

// A handler built with an empty webhookToken (i.e. unconfigured) must
// refuse every request — empty-token-equals-empty-credential would
// otherwise round-trip a valid match and re-open the very hole the
// auth check closes.
func TestServeHTTP_UnconfiguredHandlerRejectsEverything(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockActivityStore(ctrl)
	store.EXPECT().Insert(gomock.Any(), gomock.Any()).Times(0)
	h := NewHandler(activity.NewService(store), "", nil)

	// Even an "empty Bearer" — what naively-set headers would produce —
	// must be refused.
	req := httptest.NewRequest(http.MethodPost, Path, bytes.NewReader(shapedPayload()))
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

// a malformed JSON body returns 400 and does not reach the store. This
// is the "bad payload" check the integration test guards against.
func TestServeHTTP_InvalidJSONRejected(t *testing.T) {
	h, store := newTestHandler(t)
	store.EXPECT().Insert(gomock.Any(), gomock.Any()).Times(0)

	req := newAuthedRequest(t, []byte("not json"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// an empty batch is ack'd without writing — the request is valid but
// uninteresting, so we return 204 to keep Grafana from retrying.
func TestServeHTTP_EmptyBatchAcked(t *testing.T) {
	h, store := newTestHandler(t)
	store.EXPECT().Insert(gomock.Any(), gomock.Any()).Times(0)

	req := newAuthedRequest(t, []byte(`{"version":"4","status":"firing","alerts":[]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
}

// a store error on one alert in the batch must not prevent the rest of
// the batch from landing — best-effort persistence keeps individual
// row failures isolated. As long as at least one alert lands, the
// handler still acks 204 so Grafana doesn't retry and double-insert
// the rows that did succeed.
func TestServeHTTP_PartialStoreFailureKeepsBatchProgressing(t *testing.T) {
	h, store := newTestHandler(t)

	// Two alerts: the first errors, the second succeeds.
	body := []byte(`{
		"version": "4",
		"status": "firing",
		"alerts": [
			{"status":"firing","labels":{"alertname":"A","organization_id":"1"}},
			{"status":"firing","labels":{"alertname":"B","organization_id":"1"}}
		]
	}`)

	gomock.InOrder(
		store.EXPECT().
			Insert(gomock.Any(), gomock.Any()).
			Return(errors.New("transient db error")),
		store.EXPECT().
			Insert(gomock.Any(), gomock.Any()).
			Return(nil),
	)

	req := newAuthedRequest(t, body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
}

// when every alert in the batch fails to persist — typically the DB
// is down or the activity log is rejecting writes — the handler must
// return 5xx so Grafana retries the delivery. Acking 204 here would
// let a transient outage permanently drop alert history from the
// activity log.
func TestServeHTTP_AllStoreFailuresReturn5xx(t *testing.T) {
	h, store := newTestHandler(t)

	body := []byte(`{
		"version": "4",
		"status": "firing",
		"alerts": [
			{"status":"firing","labels":{"alertname":"A","organization_id":"1"}},
			{"status":"firing","labels":{"alertname":"B","organization_id":"1"}}
		]
	}`)

	store.EXPECT().
		Insert(gomock.Any(), gomock.Any()).
		Return(errors.New("transient db error")).
		Times(2)

	req := newAuthedRequest(t, body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

// the JSON decoder is strict about the envelope shape, but accepts
// unknown keys so a future Grafana version that adds metadata fields
// doesn't break the receiver.
func TestServeHTTP_AcceptsUnknownFields(t *testing.T) {
	h, store := newTestHandler(t)
	store.EXPECT().Insert(gomock.Any(), gomock.Any()).Return(nil)

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
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
}

// selfMonitoringPayload returns the canonical shape Grafana emits when
// the "Metric Ingest Stalled" rule fires in its no-data state — the
// alert carries the rule-defined statics (severity, rule_group,
// component) but no organization_id label, so the receiver has to fan
// the alert out to make it visible in any org-scoped activity feed.
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

// the fan-out happy path: an unscoped self-monitoring alert lands as
// one activity row per active org so each org's scoped activity feed
// surfaces the critical signal.
func TestServeHTTP_SelfMonitoringFansOutToAllOrgs(t *testing.T) {
	h, store := newTestHandlerWithOrgs(t, stubOrgLister{ids: []int64{1, 2, 5}})

	captured := make([]int64, 0, 3)
	store.EXPECT().
		Insert(gomock.Any(), gomock.AssignableToTypeOf(&models.Event{})).
		DoAndReturn(func(_ context.Context, e *models.Event) error {
			require.NotNil(t, e.OrganizationID, "fan-out row must be org-scoped")
			captured = append(captured, *e.OrganizationID)
			require.Equal(t, "alert.Metric Ingest Stalled.firing", e.Type)
			require.Equal(t, models.CategorySystem, e.Category)
			require.Equal(t, models.ActorSystem, e.ActorType)
			require.Equal(t, models.ResultFailure, e.Result)
			return nil
		}).
		Times(3)

	req := newAuthedRequest(t, selfMonitoringPayload())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.ElementsMatch(t, []int64{1, 2, 5}, captured)
}

// fan-out copies the event per org rather than reusing one pointer —
// regression guard for the "every row points at the loop variable" bug
// that would otherwise turn fan-out into "N rows for the last org".
func TestServeHTTP_SelfMonitoringFanOutHasDistinctOrgPointers(t *testing.T) {
	h, store := newTestHandlerWithOrgs(t, stubOrgLister{ids: []int64{10, 20}})

	pointers := make([]*int64, 0, 2)
	store.EXPECT().
		Insert(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, e *models.Event) error {
			pointers = append(pointers, e.OrganizationID)
			return nil
		}).
		Times(2)

	req := newAuthedRequest(t, selfMonitoringPayload())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Len(t, pointers, 2)
	require.NotSame(t, pointers[0], pointers[1], "each fan-out row must own its OrganizationID pointer")
	require.Equal(t, int64(10), *pointers[0])
	require.Equal(t, int64(20), *pointers[1])
}

// when the lister errors, the handler still records the alert (as a
// single unscoped row) so the critical signal isn't dropped entirely.
// Losing fan-out degrades visibility but mustn't lose the event.
func TestServeHTTP_SelfMonitoringFallsBackOnListerError(t *testing.T) {
	h, store := newTestHandlerWithOrgs(t, stubOrgLister{err: errors.New("db down")})

	var captured models.Event
	store.EXPECT().
		Insert(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, e *models.Event) error {
			captured = *e
			return nil
		}).
		Times(1)

	req := newAuthedRequest(t, selfMonitoringPayload())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Nil(t, captured.OrganizationID, "fallback row stays unscoped — fan-out couldn't run")
	require.Equal(t, "alert.Metric Ingest Stalled.firing", captured.Type)
}

// no active orgs → single unscoped row (fresh install, all orgs deleted
// — the alert still has to land somewhere or operators reading the raw
// activity_log table get nothing).
func TestServeHTTP_SelfMonitoringNoActiveOrgsFallsBackToUnscoped(t *testing.T) {
	h, store := newTestHandlerWithOrgs(t, stubOrgLister{ids: nil})

	var captured models.Event
	store.EXPECT().
		Insert(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, e *models.Event) error {
			captured = *e
			return nil
		}).
		Times(1)

	req := newAuthedRequest(t, selfMonitoringPayload())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Nil(t, captured.OrganizationID)
}

// an unscoped alert that ISN'T self-monitoring (no proto-fleet-self
// rule_group label) keeps the historic single-row, unscoped behaviour
// — fan-out is opt-in via rule_group so device/auth-style events
// without an org label don't suddenly multiply across orgs.
func TestServeHTTP_UnscopedNonSelfAlertDoesNotFanOut(t *testing.T) {
	h, store := newTestHandlerWithOrgs(t, stubOrgLister{ids: []int64{1, 2, 3}})

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

	store.EXPECT().
		Insert(gomock.Any(), gomock.Any()).
		Return(nil).
		Times(1)

	req := newAuthedRequest(t, body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
}

// fan-out tolerates partial Insert failures the same way batches do:
// any row that lands keeps the handler from returning 5xx, so Grafana
// doesn't retry the already-written rows and double them.
func TestServeHTTP_SelfMonitoringFanOutToleratesPartialFailure(t *testing.T) {
	h, store := newTestHandlerWithOrgs(t, stubOrgLister{ids: []int64{1, 2, 3}})

	gomock.InOrder(
		store.EXPECT().Insert(gomock.Any(), gomock.Any()).Return(nil),
		store.EXPECT().Insert(gomock.Any(), gomock.Any()).Return(errors.New("transient db error")),
		store.EXPECT().Insert(gomock.Any(), gomock.Any()).Return(nil),
	)

	req := newAuthedRequest(t, selfMonitoringPayload())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
}

// when every fan-out Insert fails (DB down) and there are no other
// alerts in the batch, the handler returns 5xx so Grafana retries —
// same contract as a regular batch with no successes.
func TestServeHTTP_SelfMonitoringFanOutAllFailuresReturn5xx(t *testing.T) {
	h, store := newTestHandlerWithOrgs(t, stubOrgLister{ids: []int64{1, 2}})

	store.EXPECT().
		Insert(gomock.Any(), gomock.Any()).
		Return(errors.New("db down")).
		Times(2)

	req := newAuthedRequest(t, selfMonitoringPayload())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

// a batch carrying more alerts than the per-request cap is rejected
// with 413 and never reaches the store. Grafana's webhook contact
// point should also set settings.maxAlerts to truncate at the
// sender; this server-side cap is the belt that catches a
// misconfigured or compromised sender pushing amplified payloads.
func TestServeHTTP_TooManyAlertsRejected(t *testing.T) {
	h, store := newTestHandler(t)
	store.EXPECT().Insert(gomock.Any(), gomock.Any()).Times(0)

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
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

// a batch sitting right at the per-request alert cap still passes
// through — the limit is "more than maxAlertsPerRequest", not
// "maxAlertsPerRequest or more", so well-tuned senders don't end up
// in 413 territory by hitting the cap exactly.
func TestServeHTTP_AtAlertCapStillAccepted(t *testing.T) {
	h, store := newTestHandler(t)

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

	store.EXPECT().
		Insert(gomock.Any(), gomock.Any()).
		Return(nil).
		Times(maxAlertsPerRequest)

	req := newAuthedRequest(t, body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
}

// a single self-monitoring alert with more active orgs than the
// per-request row cap allows fans out only as many rows as fit in
// the budget. The remaining orgs are intentionally dropped — the
// rule keeps firing on the next Grafana evaluation, so the
// truncated delivery isn't a permanent silencing — but the request
// itself stays bounded.
func TestServeHTTP_SelfMonitoringFanOutTruncatesAtRowCap(t *testing.T) {
	orgIDs := make([]int64, maxRowsPerRequest+5)
	for i := range orgIDs {
		orgIDs[i] = int64(i + 1)
	}
	h, store := newTestHandlerWithOrgs(t, stubOrgLister{ids: orgIDs})

	store.EXPECT().
		Insert(gomock.Any(), gomock.Any()).
		Return(nil).
		Times(maxRowsPerRequest)

	req := newAuthedRequest(t, selfMonitoringPayload())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
}

// the per-request row budget is shared across the whole batch: once
// an early alert exhausts it via fan-out, later alerts in the batch
// are dropped (rather than re-budgeting per alert). Keeps an
// adversarial batch from amplifying one extreme alert into N
// extreme alerts.
func TestServeHTTP_RowBudgetSharedAcrossBatch(t *testing.T) {
	orgIDs := make([]int64, maxRowsPerRequest)
	for i := range orgIDs {
		orgIDs[i] = int64(i + 1)
	}
	h, store := newTestHandlerWithOrgs(t, stubOrgLister{ids: orgIDs})

	// Two self-monitoring alerts: the first exhausts the budget with
	// its fan-out (one row per org), the second must contribute zero
	// rows. Without a shared budget, the second alert would double
	// the request to 2 * maxRowsPerRequest synchronous inserts.
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

	store.EXPECT().
		Insert(gomock.Any(), gomock.Any()).
		Return(nil).
		Times(maxRowsPerRequest)

	req := newAuthedRequest(t, body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
}

// payloads larger than the body cap return 413 and never touch the
// store. Catches a malformed sender trying to wedge the receiver.
func TestServeHTTP_OversizedBodyRejected(t *testing.T) {
	h, store := newTestHandler(t)
	store.EXPECT().Insert(gomock.Any(), gomock.Any()).Times(0)

	// Pad the body well past the 1 MiB cap.
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
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}
