package deployment

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.etcd.io/etcd/api/v3/etcdserverpb"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func TestStartupEtcdQuorumReportsQuorumAndAuthentication(t *testing.T) {
	for _, test := range []struct {
		name             string
		statuses         map[string]*clientv3.StatusResponse
		errors           map[string]error
		requiredEndpoint string
		wantQuorum       bool
		wantAuthRequired bool
		wantLocalReady   bool
	}{
		{
			name: "unauthenticated bootstrap quorum",
			statuses: map[string]*clientv3.StatusResponse{
				"a": startupStatus(11, 11),
				"b": startupStatus(12, 11),
			},
			wantQuorum: true, wantLocalReady: true,
		},
		{
			name:             "installed cluster requires authenticated client",
			errors:           map[string]error{"a": rpctypes.ErrUserEmpty, "b": rpctypes.ErrPermissionDenied},
			wantAuthRequired: true, wantLocalReady: true,
		},
		{
			name: "duplicate member is not quorum",
			statuses: map[string]*clientv3.StatusResponse{
				"a": startupStatus(11, 11),
				"b": startupStatus(11, 11),
			},
			wantLocalReady: true,
		},
		{
			name:             "healthy peers do not replace local member",
			requiredEndpoint: "local",
			statuses: map[string]*clientv3.StatusResponse{
				"a": startupStatus(11, 11),
				"b": startupStatus(12, 11),
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Arrange
			status := func(_ context.Context, endpoint string) (*clientv3.StatusResponse, error) {
				if err := test.errors[endpoint]; err != nil {
					return nil, err
				}
				return test.statuses[endpoint], nil
			}

			// Act
			requiredEndpoint := test.requiredEndpoint
			if requiredEndpoint == "" {
				requiredEndpoint = "a"
			}
			quorum, authRequired, localReady := startupEtcdQuorum(t.Context(), status, []string{"a", "b", "local"}, requiredEndpoint)

			// Assert
			require.Equal(t, test.wantQuorum, quorum)
			require.Equal(t, test.wantAuthRequired, authRequired)
			require.Equal(t, test.wantLocalReady, localReady)
		})
	}
}

func TestInfrastructureDownArgsSelectsDatabaseProfile(t *testing.T) {
	for _, test := range []struct {
		name         string
		databaseNode bool
		want         []string
	}{
		{
			name:         "database host",
			databaseNode: true,
			want:         []string{"--env-file", "node.env", "--file", filepath.Join(installRoot, "ha", "compose.yaml"), "--profile", "database", "down"},
		},
		{
			name: "witness",
			want: []string{"--env-file", "node.env", "--file", filepath.Join(installRoot, "ha", "compose.yaml"), "down"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Act
			args := infrastructureDownArgs("node.env", test.databaseNode)

			// Assert
			require.Equal(t, test.want, args)
		})
	}
}

func TestShouldBootstrapEtcdAuth(t *testing.T) {
	for _, test := range []struct {
		name             string
		authEnabled      bool
		credentialExists bool
		wantBootstrap    bool
		wantError        string
	}{
		{name: "restart after bootstrap", authEnabled: true},
		{name: "initial bootstrap", credentialExists: true, wantBootstrap: true},
		{name: "missing bootstrap credential", wantError: "root password is missing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Arrange
			path := filepath.Join(t.TempDir(), "etcd-root-password")
			if test.credentialExists {
				require.NoError(t, os.WriteFile(path, []byte("secret"), 0o600))
			}

			// Act
			bootstrap, err := shouldBootstrapEtcdAuth("ha-a", test.authEnabled, path)

			// Assert
			require.Equal(t, test.wantBootstrap, bootstrap)
			if test.wantError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, test.wantError)
			}
		})
	}
}

func startupStatus(memberID, leaderID uint64) *clientv3.StatusResponse {
	return &clientv3.StatusResponse{
		Header: &etcdserverpb.ResponseHeader{MemberId: memberID},
		Leader: leaderID,
	}
}
