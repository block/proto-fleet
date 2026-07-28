package ha

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

func TestEtcdHTTPClientSnapshotUsesLinearizablePrefixRead(t *testing.T) {
	const clusterPath = "/service/fleet"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v3/kv/range", r.URL.Path)
		var body struct {
			Key          string `json:"key"`
			RangeEnd     string `json:"range_end"`
			Serializable bool   `json:"serializable"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.False(t, body.Serializable)
		require.Equal(t, clusterPath+"/", decodeBase64(t, body.Key))
		require.NotEmpty(t, decodeBase64(t, body.RangeEnd))

		writeJSON(t, w, etcdSnapshotResponse("cluster-a", "patroni-a", 41, 998))
	}))
	t.Cleanup(server.Close)

	client := NewEtcdHTTPClient(server.URL, server.Client())
	snapshot, err := client.Snapshot(t.Context(), clusterPath)
	require.NoError(t, err)
	require.Equal(t, "cluster-a", snapshot.ClusterID)
	require.Equal(t, int64(41), snapshot.WriterGeneration)
	require.Equal(t, int64(998), snapshot.LeaderLeaseID)
}

func TestHAHTTPClientsUseBoundedDefaultTimeout(t *testing.T) {
	require.Greater(t, NewEtcdHTTPClient("http://etcd", nil).client.Timeout, time.Duration(0))
	require.Greater(t, NewPatroniHTTPClient(nil).client.Timeout, time.Duration(0))
}

func TestPatroniHTTPClientRejectsUnsupportedScheme(t *testing.T) {
	_, err := NewPatroniHTTPClient(nil).PrimaryIdentity(
		t.Context(),
		"file:///var/run/patroni",
		"127.0.0.1",
	)
	require.ErrorContains(t, err, "scheme must be http or https")
}

func TestPatroniHTTPClientRejectsRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(server.Close)

	_, err := NewPatroniHTTPClient(server.Client()).PrimaryIdentity(
		t.Context(),
		server.URL,
		"127.0.0.1",
	)
	require.ErrorContains(t, err, "redirects are not allowed")
}

func TestPatroniHTTPClientDialsValidatedPostgresAddress(t *testing.T) {
	var expectedHost string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/primary", r.URL.Path)
		require.Equal(t, expectedHost, r.Host)
		writeJSON(t, w, map[string]any{"role": "primary", "timeline": 7})
	}))
	t.Cleanup(server.Close)

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	_, port, err := net.SplitHostPort(serverURL.Host)
	require.NoError(t, err)
	expectedHost = net.JoinHostPort("patroni.invalid", port)

	identity, err := NewPatroniHTTPClient(server.Client()).PrimaryIdentity(
		t.Context(),
		"http://"+expectedHost,
		serverURL.Hostname(),
	)
	require.NoError(t, err)
	require.Equal(t, PatroniIdentity{Role: "primary", Timeline: 7}, identity)
}

func TestObserverAcceptsStableBoundWriter(t *testing.T) {
	observer := validObserver(
		[]DCSSnapshot{
			validDCSSnapshot(),
			validDCSSnapshot(),
		},
	)

	observation, err := observer.Observe(t.Context())
	require.NoError(t, err)
	require.Equal(t, "cluster-a", observation.DCSClusterID)
	require.Equal(t, int64(41), observation.WriterGeneration)
	require.Equal(t, "patroni-a", observation.LeaderName)
	require.WithinDuration(
		t,
		time.Now().Add(10*time.Second),
		observation.DCSProofDeadline,
		50*time.Millisecond,
	)
}

func TestObserverValidatesAuthoritiesInOrder(t *testing.T) {
	var calls []string
	var patroniServerAddress string
	observer := validObserver([]DCSSnapshot{validDCSSnapshot(), validDCSSnapshot()})
	dcs, ok := observer.dcs.(*fakeDCSReader)
	require.True(t, ok)
	dcs.calls = &calls
	observer.postgres = fakePostgresReader{
		identity: sqlc.ConnectedPostgresIdentity{
			ServerAddress: "172.30.0.12",
			ServerPort:    5432,
			Timeline:      7,
		},
		calls: &calls,
	}
	observer.resolve = func(context.Context, string) ([]string, error) {
		calls = append(calls, "resolve")
		return []string{"172.30.0.12"}, nil
	}
	observer.patroni = fakePatroniReader{
		identity:      PatroniIdentity{Role: "primary", Timeline: 7},
		calls:         &calls,
		serverAddress: &patroniServerAddress,
	}

	_, err := observer.Observe(t.Context())
	require.NoError(t, err)
	require.Equal(t, "172.30.0.12", patroniServerAddress)
	require.Equal(
		t,
		[]string{
			"dcs-snapshot",
			"postgres",
			"resolve",
			"patroni",
			"dcs-snapshot",
			"dcs-lease",
		},
		calls,
	)
}

func TestObserverRunsActionInsideDCSValidationBracket(t *testing.T) {
	var calls []string
	observer := validObserver([]DCSSnapshot{validDCSSnapshot(), validDCSSnapshot()})
	dcs, ok := observer.dcs.(*fakeDCSReader)
	require.True(t, ok)
	dcs.calls = &calls
	observer.postgres = fakePostgresReader{
		identity: sqlc.ConnectedPostgresIdentity{
			ServerAddress: "172.30.0.12",
			ServerPort:    5432,
			Timeline:      7,
		},
		calls: &calls,
	}
	observer.resolve = func(context.Context, string) ([]string, error) {
		calls = append(calls, "resolve")
		return []string{"172.30.0.12"}, nil
	}
	observer.patroni = fakePatroniReader{
		identity: PatroniIdentity{Role: "primary", Timeline: 7},
		calls:    &calls,
	}

	_, err := observer.ObserveAndRun(
		t.Context(),
		func(context.Context, WriterObservation) error {
			calls = append(calls, "action")
			return nil
		},
	)
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{
			"dcs-snapshot",
			"postgres",
			"resolve",
			"patroni",
			"action",
			"dcs-snapshot",
			"dcs-lease",
		},
		calls,
	)
}

func TestObserverRejectsPatroniAPIServerMismatch(t *testing.T) {
	observer := validObserver([]DCSSnapshot{validDCSSnapshot()})
	observer.resolve = func(_ context.Context, host string) ([]string, error) {
		if host == "patroni-a" {
			return []string{"172.30.0.12"}, nil
		}
		return []string{"172.30.0.99"}, nil
	}
	snapshotReader, ok := observer.dcs.(*fakeDCSReader)
	require.True(t, ok)
	snapshotReader.snapshots[0].Member.APIURL = "http://patroni-api-elsewhere:8008"

	_, err := observer.Observe(t.Context())
	require.ErrorIs(t, err, ErrWritableServerMismatch)
}

func TestObserverRejectsLeaderLeaseChangeInsideValidationBracket(t *testing.T) {
	second := validDCSSnapshot()
	second.LeaderLeaseID++
	observer := validObserver([]DCSSnapshot{validDCSSnapshot(), second})

	_, err := observer.Observe(t.Context())
	require.ErrorIs(t, err, ErrWriterChanged)
}

func TestObserverRejectsChangedLeaderTerm(t *testing.T) {
	second := validDCSSnapshot()
	second.LeaderName = "patroni-b"
	second.Member = DCSMember{
		Name:    "patroni-b",
		APIURL:  "http://patroni-b:8008",
		ConnURL: "postgres://patroni-b:5432/postgres",
	}
	observer := validObserver([]DCSSnapshot{validDCSSnapshot(), second})

	_, err := observer.Observe(t.Context())
	require.ErrorIs(t, err, ErrWriterChanged)
}

func TestObserverRejectsDCSClusterIdentityChange(t *testing.T) {
	second := validDCSSnapshot()
	second.ClusterID = "cluster-b"
	observer := validObserver([]DCSSnapshot{validDCSSnapshot(), second})

	_, err := observer.Observe(t.Context())
	require.ErrorIs(t, err, ErrDCSClusterIdentityMismatch)
}

func TestObserverRejectsMissingDCSIdentityData(t *testing.T) {
	tests := map[string]func(*DCSSnapshot){
		"cluster identity": func(snapshot *DCSSnapshot) { snapshot.ClusterID = "" },
		"leader":           func(snapshot *DCSSnapshot) { snapshot.LeaderName = "" },
		"member":           func(snapshot *DCSSnapshot) { snapshot.Member = DCSMember{} },
		"generation":       func(snapshot *DCSSnapshot) { snapshot.WriterGeneration = 0 },
		"lease":            func(snapshot *DCSSnapshot) { snapshot.LeaderLeaseID = 0 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			snapshot := validDCSSnapshot()
			mutate(&snapshot)
			observer := validObserver([]DCSSnapshot{snapshot})

			_, err := observer.Observe(t.Context())
			require.Error(t, err)
		})
	}
}

func TestObserverRejectsWritableServerMismatch(t *testing.T) {
	observer := validObserver([]DCSSnapshot{validDCSSnapshot()})
	observer.resolve = func(context.Context, string) ([]string, error) {
		return []string{"172.30.0.99"}, nil
	}

	_, err := observer.Observe(t.Context())
	require.ErrorIs(t, err, ErrWritableServerMismatch)
}

func TestObserverRejectsPostgresReplica(t *testing.T) {
	observer := validObserver([]DCSSnapshot{validDCSSnapshot()})
	observer.postgres = fakePostgresReader{identity: sqlc.ConnectedPostgresIdentity{
		ServerAddress: "172.30.0.12",
		ServerPort:    5432,
		InRecovery:    true,
		Timeline:      7,
	}}

	_, err := observer.Observe(t.Context())
	require.ErrorIs(t, err, ErrWritableServerMismatch)
}

func TestObserverRejectsTimelineMismatch(t *testing.T) {
	observer := validObserver([]DCSSnapshot{validDCSSnapshot()})
	observer.patroni = fakePatroniReader{identity: PatroniIdentity{
		Role:     "primary",
		Timeline: 8,
	}}

	_, err := observer.Observe(t.Context())
	require.ErrorIs(t, err, ErrTimelineMismatch)
}

func TestObserverRejectsNonPrimaryPatroniMember(t *testing.T) {
	observer := validObserver([]DCSSnapshot{validDCSSnapshot()})
	observer.patroni = fakePatroniReader{identity: PatroniIdentity{
		Role:     "replica",
		Timeline: 7,
	}}

	_, err := observer.Observe(t.Context())
	require.ErrorIs(t, err, ErrWritableServerMismatch)
}

func TestObserverRejectsExpiredLeaderLease(t *testing.T) {
	observer := validObserver([]DCSSnapshot{validDCSSnapshot(), validDCSSnapshot()})
	dcs, ok := observer.dcs.(*fakeDCSReader)
	require.True(t, ok)
	dcs.leaseTTL = 0

	_, err := observer.Observe(t.Context())
	require.ErrorIs(t, err, ErrLeaderLeaseExpired)
}

func validObserver(snapshots []DCSSnapshot) *Observer {
	return &Observer{
		clusterPath: "/service/fleet",
		dcs: &fakeDCSReader{
			snapshots: snapshots,
			leaseTTL:  10 * time.Second,
		},
		postgres: fakePostgresReader{identity: sqlc.ConnectedPostgresIdentity{
			ServerAddress: "172.30.0.12",
			ServerPort:    5432,
			Timeline:      7,
		}},
		patroni: fakePatroniReader{identity: PatroniIdentity{
			Role:     "primary",
			Timeline: 7,
		}},
		resolve: func(context.Context, string) ([]string, error) {
			return []string{"172.30.0.12"}, nil
		},
	}
}

func validDCSSnapshot() DCSSnapshot {
	return DCSSnapshot{
		ClusterID:        "cluster-a",
		Revision:         50,
		LeaderName:       "patroni-a",
		WriterGeneration: 41,
		LeaderLeaseID:    998,
		Member: DCSMember{
			Name:    "patroni-a",
			APIURL:  "http://patroni-a:8008",
			ConnURL: "postgres://patroni-a:5432/postgres",
		},
	}
}

type fakeDCSReader struct {
	snapshots []DCSSnapshot
	leaseTTL  time.Duration
	calls     *[]string
}

func (f *fakeDCSReader) Snapshot(context.Context, string) (DCSSnapshot, error) {
	if f.calls != nil {
		*f.calls = append(*f.calls, "dcs-snapshot")
	}
	if len(f.snapshots) == 0 {
		return DCSSnapshot{}, errors.New("no snapshot")
	}
	snapshot := f.snapshots[0]
	f.snapshots = f.snapshots[1:]
	return snapshot, nil
}

func (f *fakeDCSReader) LeaseTTL(context.Context, int64) (time.Duration, error) {
	if f.calls != nil {
		*f.calls = append(*f.calls, "dcs-lease")
	}
	return f.leaseTTL, nil
}

type fakePostgresReader struct {
	identity sqlc.ConnectedPostgresIdentity
	err      error
	calls    *[]string
}

func (f fakePostgresReader) GetConnectedPostgresIdentity(
	context.Context,
) (sqlc.ConnectedPostgresIdentity, error) {
	if f.calls != nil {
		*f.calls = append(*f.calls, "postgres")
	}
	return f.identity, f.err
}

type fakePatroniReader struct {
	identity      PatroniIdentity
	err           error
	calls         *[]string
	serverAddress *string
}

func (f fakePatroniReader) PrimaryIdentity(
	_ context.Context,
	_ string,
	serverAddress string,
) (PatroniIdentity, error) {
	if f.calls != nil {
		*f.calls = append(*f.calls, "patroni")
	}
	if f.serverAddress != nil {
		*f.serverAddress = serverAddress
	}
	return f.identity, f.err
}

func decodeBase64(t *testing.T, value string) string {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString(value)
	require.NoError(t, err)
	return string(decoded)
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func etcdSnapshotResponse(clusterID, leader string, generation, leaseID int64) map[string]any {
	encoded := func(value string) string {
		return base64.StdEncoding.EncodeToString([]byte(value))
	}
	clusterPath := "/service/fleet"
	return map[string]any{
		"header": map[string]string{
			"cluster_id": clusterID,
			"revision":   "50",
		},
		"kvs": []map[string]string{
			{
				"key":             encoded(clusterPath + "/leader"),
				"value":           encoded(leader),
				"create_revision": strconv.FormatInt(generation, 10),
				"mod_revision":    "47",
				"lease":           strconv.FormatInt(leaseID, 10),
			},
			{
				"key": encoded(clusterPath + "/members/" + leader),
				"value": encoded(`{"api_url":"http://` + leader + `:8008",` +
					`"conn_url":"postgres://` + leader + `:5432/postgres"}`),
				"create_revision": "13",
				"mod_revision":    "48",
			},
		},
	}
}
