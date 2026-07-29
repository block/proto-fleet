package ha

import (
	"context"
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

func TestPatroniHTTPClientUsesBoundedDefaultTimeout(t *testing.T) {
	client, err := NewPatroniHTTPClient(nil)
	require.NoError(t, err)
	require.Greater(t, client.client.Timeout, time.Duration(0))
}

func TestPatroniHTTPClientRejectsInvalidPostgresAddress(t *testing.T) {
	client, err := NewPatroniHTTPClient(nil)
	require.NoError(t, err)
	_, err = client.PrimaryIdentity(t.Context(), "postgres.internal")
	require.ErrorContains(t, err, "not an IP")
}

func TestPatroniHTTPClientRejectsRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(server.Close)

	_, err := patroniClientForServer(t, server).PrimaryIdentity(
		t.Context(),
		serverAddress(t, server),
	)
	require.ErrorContains(t, err, "redirects are not allowed")
}

func TestPatroniHTTPClientCallsPrimaryOnPostgresServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/primary", r.URL.Path)
		writeJSON(t, w, map[string]any{"role": "primary", "timeline": 7})
	}))
	t.Cleanup(server.Close)

	identity, err := patroniClientForServer(t, server).PrimaryIdentity(
		t.Context(),
		serverAddress(t, server),
	)
	require.NoError(t, err)
	require.Equal(t, PatroniIdentity{Role: "primary", Timeline: 7}, identity)
}

func TestPatroniHTTPClientReusesConnections(t *testing.T) {
	remoteAddresses := make(map[string]struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteAddresses[r.RemoteAddr] = struct{}{}
		writeJSON(t, w, map[string]any{"role": "primary", "timeline": 7})
	}))
	t.Cleanup(server.Close)
	client := patroniClientForServer(t, server)

	_, err := client.PrimaryIdentity(t.Context(), serverAddress(t, server))
	require.NoError(t, err)
	_, err = client.PrimaryIdentity(t.Context(), serverAddress(t, server))
	require.NoError(t, err)
	require.Len(t, remoteAddresses, 1)
}

func TestObserverAcceptsPostgresWriterConfirmedByPatroni(t *testing.T) {
	observer, _, _ := validObserver(1)

	observation, err := observer.Observe(t.Context())
	require.NoError(t, err)
	require.Equal(t, "7668091434102423594", observation.PostgresSystemIdentifier)
	require.Equal(t, int64(7), observation.WriterGeneration)
	require.Equal(t, "172.30.0.12", observation.ServerAddress)
	require.Equal(t, int32(5432), observation.ServerPort)
}

func TestObserverBracketsActionWithWriterValidation(t *testing.T) {
	var calls []string
	var patroniServerAddresses []string
	observer, postgres, patroni := validObserver(2)
	postgres.calls = &calls
	patroni.calls = &calls
	patroni.serverAddresses = &patroniServerAddresses

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
		[]string{"postgres", "patroni", "action", "postgres", "patroni"},
		calls,
	)
	require.Equal(t, []string{"172.30.0.12", "172.30.0.12"}, patroniServerAddresses)
}

func TestObserverRejectsWriterChangeAcrossAction(t *testing.T) {
	tests := map[string]struct {
		mutatePostgres func(*sqlc.ConnectedPostgresIdentity)
		mutatePatroni  func(*PatroniIdentity)
	}{
		"system identifier": {
			mutatePostgres: func(identity *sqlc.ConnectedPostgresIdentity) {
				identity.SystemIdentifier = "7668091434102423595"
			},
		},
		"server address": {
			mutatePostgres: func(identity *sqlc.ConnectedPostgresIdentity) {
				identity.ServerAddress = "172.30.0.13"
			},
		},
		"server port": {
			mutatePostgres: func(identity *sqlc.ConnectedPostgresIdentity) {
				identity.ServerPort = 5433
			},
		},
		"timeline": {
			mutatePostgres: func(identity *sqlc.ConnectedPostgresIdentity) {
				identity.Timeline = 8
			},
			mutatePatroni: func(identity *PatroniIdentity) {
				identity.Timeline = 8
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			firstPostgres := validPostgresIdentity()
			secondPostgres := firstPostgres
			test.mutatePostgres(&secondPostgres)
			firstPatroni := validPatroniIdentity()
			secondPatroni := firstPatroni
			if test.mutatePatroni != nil {
				test.mutatePatroni(&secondPatroni)
			}
			observer := &Observer{
				postgres: &fakePostgresReader{
					identities: []sqlc.ConnectedPostgresIdentity{
						firstPostgres,
						secondPostgres,
					},
				},
				patroni: &fakePatroniReader{
					identities: []PatroniIdentity{firstPatroni, secondPatroni},
				},
			}

			_, err := observer.ObserveAndRun(
				t.Context(),
				func(context.Context, WriterObservation) error { return nil },
			)
			require.ErrorIs(t, err, ErrWriterChanged)
		})
	}
}

func TestObserverRejectsInvalidPostgresIdentity(t *testing.T) {
	tests := map[string]func(*sqlc.ConnectedPostgresIdentity){
		"system identifier": func(identity *sqlc.ConnectedPostgresIdentity) {
			identity.SystemIdentifier = ""
		},
		"server address": func(identity *sqlc.ConnectedPostgresIdentity) {
			identity.ServerAddress = ""
		},
		"server port": func(identity *sqlc.ConnectedPostgresIdentity) {
			identity.ServerPort = 0
		},
		"timeline": func(identity *sqlc.ConnectedPostgresIdentity) {
			identity.Timeline = 0
		},
		"replica": func(identity *sqlc.ConnectedPostgresIdentity) {
			identity.InRecovery = true
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			identity := validPostgresIdentity()
			mutate(&identity)
			observer := &Observer{
				postgres: &fakePostgresReader{
					identities: []sqlc.ConnectedPostgresIdentity{identity},
				},
				patroni: &fakePatroniReader{
					identities: []PatroniIdentity{validPatroniIdentity()},
				},
			}

			_, err := observer.Observe(t.Context())
			require.ErrorIs(t, err, ErrWritableServerMismatch)
		})
	}
}

func TestObserverRejectsTimelineMismatch(t *testing.T) {
	observer, _, patroni := validObserver(1)
	patroni.identities[0].Timeline = 8

	_, err := observer.Observe(t.Context())
	require.ErrorIs(t, err, ErrTimelineMismatch)
}

func TestObserverRejectsNonPrimaryPatroniMember(t *testing.T) {
	observer, _, patroni := validObserver(1)
	patroni.identities[0].Role = "replica"

	_, err := observer.Observe(t.Context())
	require.ErrorIs(t, err, ErrWritableServerMismatch)
}

func TestObserverStopsWhenActionFails(t *testing.T) {
	actionErr := errors.New("lease write failed")
	observer, postgres, patroni := validObserver(2)

	_, err := observer.ObserveAndRun(
		t.Context(),
		func(context.Context, WriterObservation) error { return actionErr },
	)
	require.ErrorIs(t, err, actionErr)
	require.Len(t, postgres.identities, 1)
	require.Len(t, patroni.identities, 1)
}

func validObserver(
	observations int,
) (*Observer, *fakePostgresReader, *fakePatroniReader) {
	postgresIdentities := make([]sqlc.ConnectedPostgresIdentity, observations)
	patroniIdentities := make([]PatroniIdentity, observations)
	for index := range observations {
		postgresIdentities[index] = validPostgresIdentity()
		patroniIdentities[index] = validPatroniIdentity()
	}
	postgres := &fakePostgresReader{identities: postgresIdentities}
	patroni := &fakePatroniReader{identities: patroniIdentities}
	return &Observer{postgres: postgres, patroni: patroni}, postgres, patroni
}

func validPostgresIdentity() sqlc.ConnectedPostgresIdentity {
	return sqlc.ConnectedPostgresIdentity{
		SystemIdentifier: "7668091434102423594",
		ServerAddress:    "172.30.0.12",
		ServerPort:       5432,
		Timeline:         7,
	}
}

func validPatroniIdentity() PatroniIdentity {
	return PatroniIdentity{Role: "primary", Timeline: 7}
}

type fakePostgresReader struct {
	identities []sqlc.ConnectedPostgresIdentity
	err        error
	calls      *[]string
}

func (f *fakePostgresReader) GetConnectedPostgresIdentity(
	context.Context,
) (sqlc.ConnectedPostgresIdentity, error) {
	if f.calls != nil {
		*f.calls = append(*f.calls, "postgres")
	}
	if f.err != nil {
		return sqlc.ConnectedPostgresIdentity{}, f.err
	}
	if len(f.identities) == 0 {
		return sqlc.ConnectedPostgresIdentity{}, errors.New("no PostgreSQL identity")
	}
	identity := f.identities[0]
	f.identities = f.identities[1:]
	return identity, nil
}

type fakePatroniReader struct {
	identities      []PatroniIdentity
	err             error
	calls           *[]string
	serverAddresses *[]string
}

func (f *fakePatroniReader) PrimaryIdentity(
	_ context.Context,
	serverAddress string,
) (PatroniIdentity, error) {
	if f.calls != nil {
		*f.calls = append(*f.calls, "patroni")
	}
	if f.serverAddresses != nil {
		*f.serverAddresses = append(*f.serverAddresses, serverAddress)
	}
	if f.err != nil {
		return PatroniIdentity{}, f.err
	}
	if len(f.identities) == 0 {
		return PatroniIdentity{}, errors.New("no Patroni identity")
	}
	identity := f.identities[0]
	f.identities = f.identities[1:]
	return identity, nil
}

func patroniClientForServer(t *testing.T, server *httptest.Server) *PatroniHTTPClient {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)
	client, err := NewPatroniHTTPClient(server.Client())
	require.NoError(t, err)
	client.port = port
	return client
}

func serverAddress(t *testing.T, server *httptest.Server) string {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)
	address := net.ParseIP(parsed.Hostname())
	require.NotNil(t, address)
	return address.String()
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(value))
}
