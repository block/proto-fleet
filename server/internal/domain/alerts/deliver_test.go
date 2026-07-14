package alerts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type capturedPost struct {
	auth string
	body []byte
}

// captureServer records every POST body (and Authorization header) it receives.
func captureServer(t *testing.T) (*httptest.Server, *[]capturedPost) {
	t.Helper()
	var mu sync.Mutex
	var got []capturedPost
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, capturedPost{auth: r.Header.Get("Authorization"), body: b})
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

// seedChannel stores an encrypted channel row directly, as CreateChannel would.
func seedChannel(t *testing.T, store *fakeChannelStore, crypto Cipher, orgID int64, kind ChannelKind, url, bearer string) {
	t.Helper()
	enc, err := encodeChannelConfig(crypto, channelConfig{URL: url, Bearer: bearer})
	require.NoError(t, err)
	_, err = store.Insert(context.Background(), ChannelRecord{OrganizationID: orgID, Name: "c", Kind: kind, EncryptedConfig: enc})
	require.NoError(t, err)
}

type fakeDeviceLookup struct {
	ids map[string]DeviceIdentity
}

func (f fakeDeviceLookup) DeviceIdentities(_ context.Context, _ int64, _ []string) (map[string]DeviceIdentity, error) {
	return f.ids, nil
}

func newDeliverer(t *testing.T, store *fakeChannelStore, devices DeviceIdentityLookup) (*Deliverer, Cipher) {
	t.Helper()
	crypto := testCipher(t)
	d := NewDeliverer(store, crypto, devices, DestinationPolicy{AllowPrivateDestinations: true}, "https://fleet.example.com")
	return d, crypto
}

func firingAlert(orgID, dev, name string) Alert {
	return Alert{
		Status:      "firing",
		Labels:      map[string]string{"organization_id": orgID, "device_id": dev, "alertname": name, "severity": "warning"},
		Annotations: map[string]string{"summary": name + " summary"},
	}
}

func TestDeliverSlackChannel(t *testing.T) {
	srv, got := captureServer(t)
	store := newFakeChannelStore()
	d, crypto := newDeliverer(t, store, fakeDeviceLookup{ids: map[string]DeviceIdentity{"dev-a": {Name: "miner-01", MAC: "aa:bb"}}})
	seedChannel(t, store, crypto, 7, ChannelKindSlack, srv.URL, "")

	d.Deliver(context.Background(), []Alert{firingAlert("7", "dev-a", "Device Offline")})

	require.Len(t, *got, 1)
	body := string((*got)[0].body)
	assert.Contains(t, body, "blocks")
	assert.Contains(t, body, "miner-01 (aa:bb)")
	assert.NotContains(t, body, "grafana")
	assert.Empty(t, (*got)[0].auth, "slack webhook posts carry no bearer")
}

func TestDeliverWebhookChannelSendsBearer(t *testing.T) {
	srv, got := captureServer(t)
	store := newFakeChannelStore()
	d, crypto := newDeliverer(t, store, fakeDeviceLookup{})
	seedChannel(t, store, crypto, 7, ChannelKindWebhook, srv.URL, "s3cret")

	d.Deliver(context.Background(), []Alert{firingAlert("7", "dev-a", "Device Offline")})

	require.Len(t, *got, 1)
	assert.Equal(t, "Bearer s3cret", (*got)[0].auth)
	var payload map[string]any
	require.NoError(t, json.Unmarshal((*got)[0].body, &payload))
	assert.EqualValues(t, 7, payload["organization_id"])
}

func TestDeliverExcludesInternalScopeAndOrglessAlerts(t *testing.T) {
	srv, got := captureServer(t)
	store := newFakeChannelStore()
	d, crypto := newDeliverer(t, store, fakeDeviceLookup{})
	seedChannel(t, store, crypto, 7, ChannelKindSlack, srv.URL, "")

	internal := Alert{Status: "firing", Labels: map[string]string{"organization_id": "7", "proto_fleet_scope": "internal", "alertname": "Metric Ingest Stalled"}}
	orgless := Alert{Status: "firing", Labels: map[string]string{"alertname": "No Org"}}
	// Grafana evaluation-failure alerts (marked by datasource_uid) inherit user
	// rules' static org label but stay operator-only.
	datasourceErr := Alert{Status: "firing", Labels: map[string]string{"organization_id": "7", "alertname": "DatasourceError", "datasource_uid": "protofleet-timescaledb"}}
	datasourceNoData := Alert{Status: "firing", Labels: map[string]string{"organization_id": "7", "alertname": "DatasourceNoData", "datasource_uid": "protofleet-timescaledb"}}
	d.Deliver(context.Background(), []Alert{internal, orgless, datasourceErr, datasourceNoData})

	assert.Empty(t, *got, "internal-scope, org-less, and evaluation-failure alerts must never reach an org channel")
}

// A real rule that merely shares Grafana's synthetic alertname (no
// datasource_uid label) must still be delivered.
func TestDeliverKeepsAlertsNamedLikeSyntheticOnes(t *testing.T) {
	srv, got := captureServer(t)
	store := newFakeChannelStore()
	d, crypto := newDeliverer(t, store, fakeDeviceLookup{})
	seedChannel(t, store, crypto, 7, ChannelKindSlack, srv.URL, "")

	d.Deliver(context.Background(), []Alert{
		{Status: "firing", Labels: map[string]string{"organization_id": "7", "alertname": "DatasourceError"}},
	})

	assert.Len(t, *got, 1, "an alert without the datasource_uid marker is not synthetic and must deliver")
}

func TestDeliverFansOutPerOrg(t *testing.T) {
	srvA, gotA := captureServer(t)
	srvB, gotB := captureServer(t)
	store := newFakeChannelStore()
	d, crypto := newDeliverer(t, store, fakeDeviceLookup{})
	seedChannel(t, store, crypto, 7, ChannelKindSlack, srvA.URL, "")
	seedChannel(t, store, crypto, 8, ChannelKindSlack, srvB.URL, "")

	d.Deliver(context.Background(), []Alert{
		firingAlert("7", "dev-a", "A"),
		firingAlert("8", "dev-b", "B"),
	})

	assert.Len(t, *gotA, 1)
	assert.Len(t, *gotB, 1)
	assert.Contains(t, string((*gotA)[0].body), "*A*")
	assert.Contains(t, string((*gotB)[0].body), "*B*")
}

func TestDeliverSkipsPrivateDestinationUnderPolicy(t *testing.T) {
	store := newFakeChannelStore()
	crypto := testCipher(t)
	// Policy disallows private destinations: a loopback URL must be refused at send time.
	d := NewDeliverer(store, crypto, fakeDeviceLookup{}, DestinationPolicy{}, "")
	seedChannel(t, store, crypto, 7, ChannelKindSlack, "http://127.0.0.1:1/hook", "")

	// No panic, no send; the SSRF check rejects it and the error is logged internally.
	d.Deliver(context.Background(), []Alert{firingAlert("7", "dev-a", "X")})
}

func TestDelivererDoesNotFollowRedirects(t *testing.T) {
	// Redirects are never followed: a 3xx must not forward the secret channel URL (Referer/Authorization)
	// to the redirect target, whether public or internal. The client returns the 3xx response instead.
	d := NewDeliverer(newFakeChannelStore(), testCipher(t), fakeDeviceLookup{}, DestinationPolicy{}, "")
	require.NotNil(t, d.httpClient.CheckRedirect)

	for _, target := range []string{"http://8.8.8.8/hook", "http://127.0.0.1/x", "http://169.254.169.254/latest"} {
		req := httptest.NewRequest(http.MethodPost, target, nil)
		require.ErrorIsf(t, d.httpClient.CheckRedirect(req, nil), http.ErrUseLastResponse,
			"redirect to %s must not be followed", target)
	}
}

func TestDelivererDisablesProxy(t *testing.T) {
	// A proxy would resolve+connect the destination itself, bypassing the pinned dial.
	d := NewDeliverer(newFakeChannelStore(), testCipher(t), fakeDeviceLookup{}, DestinationPolicy{}, "")
	tr, ok := d.httpClient.Transport.(*http.Transport)
	require.True(t, ok)
	assert.Nil(t, tr.Proxy, "egress client must not use an env proxy")
}

func TestDelivererDialRejectsPrivateAtConnect(t *testing.T) {
	// The pinned-IP dialer must refuse internal addresses even if reached via DNS rebind or redirect.
	d := NewDeliverer(newFakeChannelStore(), testCipher(t), fakeDeviceLookup{}, DestinationPolicy{}, "")
	tr, ok := d.httpClient.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, tr.DialContext)

	for _, addr := range []string{"127.0.0.1:80", "169.254.169.254:80", "10.0.0.5:443"} {
		_, err := tr.DialContext(context.Background(), "tcp", addr)
		require.Errorf(t, err, "dial to %s must be refused", addr)
	}
}

func TestSendTestReturnsAcceptedOnSuccess(t *testing.T) {
	srv, got := captureServer(t)
	d, _ := newDeliverer(t, newFakeChannelStore(), fakeDeviceLookup{})

	ok, msg, err := d.SendTest(context.Background(), ChannelKindSlack, srv.URL, "")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Empty(t, msg)
	require.Len(t, *got, 1)
	assert.Contains(t, string((*got)[0].body), "test")
}

func TestSendTestReportsFailureWithoutError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	d, _ := newDeliverer(t, newFakeChannelStore(), fakeDeviceLookup{})

	ok, msg, err := d.SendTest(context.Background(), ChannelKindWebhook, srv.URL, "tok")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.NotEmpty(t, msg, "a rejected test surfaces a message, not a Go error")
}
