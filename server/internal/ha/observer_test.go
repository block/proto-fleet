package ha

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.etcd.io/etcd/api/v3/etcdserverpb"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

func TestEtcdClientSnapshotUsesLinearizablePrefixRead(t *testing.T) {
	const clusterPath = "/service/fleet"
	api := &fakeEtcdAPI{
		getResponse: etcdSnapshotResponse(101, "patroni-a", 41, 998),
	}
	client := &EtcdClient{client: api, requestTimeout: time.Second}
	snapshot, err := client.Snapshot(t.Context(), clusterPath)
	require.NoError(t, err)
	require.Equal(t, clusterPath+"/", api.getKey)
	require.NotEmpty(t, api.getRangeEnd)
	require.Equal(t, "101", snapshot.ClusterID)
	require.Equal(t, int64(41), snapshot.WriterGeneration)
	require.Equal(t, int64(998), snapshot.LeaderLeaseID)
}

func TestNewEtcdClientKeepsRequestTimeoutIndependentFromDialTimeout(t *testing.T) {
	client, err := NewEtcdClient(clientv3.Config{
		Endpoints:   []string{"http://127.0.0.1:2379"},
		DialTimeout: time.Minute,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	require.Equal(t, defaultHAHTTPTimeout, client.requestTimeout)
}

func TestPatroniHTTPClientUsesBoundedDefaultTimeout(t *testing.T) {
	require.Greater(t, NewPatroniHTTPClient(nil).client.Timeout, time.Duration(0))
}

func TestEtcdClientReadsLeaderLeaseTTL(t *testing.T) {
	api := &fakeEtcdAPI{
		leaseResponse: &clientv3.LeaseTimeToLiveResponse{
			ID:  998,
			TTL: 18,
		},
	}

	ttl, err := (&EtcdClient{
		client:         api,
		requestTimeout: time.Second,
	}).LeaseTTL(t.Context(), 998)
	require.NoError(t, err)
	require.Equal(t, 18*time.Second, ttl)
}

func TestEtcdClientBoundsSnapshotRequest(t *testing.T) {
	client := &EtcdClient{
		client:         &fakeEtcdAPI{blockGet: true},
		requestTimeout: time.Millisecond,
	}

	_, err := client.Snapshot(t.Context(), "/service/fleet")
	require.ErrorIs(t, err, context.DeadlineExceeded)
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
		writeJSON(t, w, map[string]any{
			"role":     "primary",
			"timeline": 7,
		})
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
	require.Equal(t, PatroniIdentity{
		Role:     "primary",
		Timeline: 7,
	}, identity)
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
		identity: PatroniIdentity{
			Role:     "primary",
			Timeline: 7,
		},
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
		identity: PatroniIdentity{
			Role:     "primary",
			Timeline: 7,
		},
		calls: &calls,
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

func TestObserverRedactsDCSURLSecretsFromEndpointMismatchErrors(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*DCSMember)
		safeURL    string
		sensitive  []string
		mismatched string
	}{
		{
			name: "PostgreSQL connection URL",
			mutate: func(member *DCSMember) {
				member.ConnURL = "postgres://db-user:db-password@patroni-db:5432/private-db?sslpassword=query-secret#conn-fragment"
			},
			safeURL:    "postgres://patroni-db:5432",
			sensitive:  []string{"db-user", "db-password", "private-db", "query-secret", "conn-fragment"},
			mismatched: "patroni-db",
		},
		{
			name: "Patroni API URL",
			mutate: func(member *DCSMember) {
				member.APIURL = "http://api-user:api-password@patroni-api:8008/private-api?token=query-secret#api-fragment"
			},
			safeURL:    "http://patroni-api:8008",
			sensitive:  []string{"api-user", "api-password", "private-api", "query-secret", "api-fragment"},
			mismatched: "patroni-api",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := validDCSSnapshot()
			test.mutate(&snapshot.Member)
			observer := validObserver([]DCSSnapshot{snapshot})
			observer.resolve = func(_ context.Context, host string) ([]string, error) {
				if host == test.mismatched {
					return []string{"172.30.0.99"}, nil
				}
				return []string{"172.30.0.12"}, nil
			}

			_, err := observer.Observe(t.Context())

			require.ErrorIs(t, err, ErrWritableServerMismatch)
			require.ErrorContains(t, err, test.safeURL)
			for _, sensitive := range test.sensitive {
				require.NotContains(t, err.Error(), sensitive)
			}
		})
	}
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
	observer := validObserver([]DCSSnapshot{validDCSSnapshot(), validDCSSnapshot()})
	observer.patroni = fakePatroniReader{identity: PatroniIdentity{
		Role:     "primary",
		Timeline: 8,
	}}

	observed, err := observer.Observe(t.Context())
	require.ErrorIs(t, err, ErrTimelineMismatch)
	require.Equal(t, "cluster-a", observed.DCSClusterID)
	require.EqualValues(t, 41, observed.WriterGeneration)
	require.Zero(t, observed.DCSProofDeadline)
}

func TestObserverRejectsTimelineMismatchWhenDCSWriterChanges(t *testing.T) {
	second := validDCSSnapshot()
	second.WriterGeneration++
	observer := validObserver([]DCSSnapshot{validDCSSnapshot(), second})
	observer.patroni = fakePatroniReader{identity: PatroniIdentity{
		Role:     "primary",
		Timeline: 8,
	}}

	_, err := observer.Observe(t.Context())
	require.ErrorIs(t, err, ErrWriterChanged)
}

func TestObserverRejectsExpiredLeaderLeaseBeforeTimelineMismatch(t *testing.T) {
	observer := validObserver([]DCSSnapshot{validDCSSnapshot(), validDCSSnapshot()})
	dcs, ok := observer.dcs.(*fakeDCSReader)
	require.True(t, ok)
	dcs.leaseTTL = 0
	observer.patroni = fakePatroniReader{identity: PatroniIdentity{
		Role:     "primary",
		Timeline: 8,
	}}

	observed, err := observer.Observe(t.Context())
	require.ErrorIs(t, err, ErrLeaderLeaseExpired)
	require.Equal(t, WriterObservation{}, observed)
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

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

type fakeEtcdAPI struct {
	getKey        string
	getRangeEnd   []byte
	getResponse   *clientv3.GetResponse
	leaseResponse *clientv3.LeaseTimeToLiveResponse
	blockGet      bool
}

func (*fakeEtcdAPI) Close() error {
	return nil
}

func (f *fakeEtcdAPI) Get(
	ctx context.Context,
	key string,
	opts ...clientv3.OpOption,
) (*clientv3.GetResponse, error) {
	op := clientv3.OpGet(key, opts...)
	f.getKey = key
	f.getRangeEnd = op.RangeBytes()
	if f.blockGet {
		<-ctx.Done()
		return nil, fmt.Errorf("blocked etcd request: %w", ctx.Err())
	}
	return f.getResponse, nil
}

func (f *fakeEtcdAPI) TimeToLive(
	context.Context,
	clientv3.LeaseID,
	...clientv3.LeaseOption,
) (*clientv3.LeaseTimeToLiveResponse, error) {
	return f.leaseResponse, nil
}

func etcdSnapshotResponse(
	clusterID uint64,
	leader string,
	generation int64,
	leaseID int64,
) *clientv3.GetResponse {
	clusterPath := "/service/fleet"
	return &clientv3.GetResponse{
		Header: &etcdserverpb.ResponseHeader{
			ClusterId: clusterID,
			Revision:  50,
		},
		Kvs: []*mvccpb.KeyValue{
			{
				Key:            []byte(clusterPath + "/leader"),
				Value:          []byte(leader),
				CreateRevision: generation,
				ModRevision:    47,
				Lease:          leaseID,
			},
			{
				Key: []byte(clusterPath + "/members/" + leader),
				Value: []byte(`{"api_url":"http://` + leader + `:8008",` +
					`"conn_url":"postgres://` + leader + `:5432/postgres"}`),
				CreateRevision: 13,
				ModRevision:    48,
			},
		},
	}
}
