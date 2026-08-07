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
		wantQuorum       bool
		wantAuthRequired bool
	}{
		{
			name: "unauthenticated bootstrap quorum",
			statuses: map[string]*clientv3.StatusResponse{
				"a": startupStatus(1, 11, 11),
				"b": startupStatus(1, 12, 11),
			},
			wantQuorum: true,
		},
		{
			name:             "installed cluster requires authenticated client",
			errors:           map[string]error{"a": rpctypes.ErrUserEmpty, "b": rpctypes.ErrPermissionDenied},
			wantAuthRequired: true,
		},
		{
			name: "duplicate member is not quorum",
			statuses: map[string]*clientv3.StatusResponse{
				"a": startupStatus(1, 11, 11),
				"b": startupStatus(1, 11, 11),
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
			quorum, authRequired := startupEtcdQuorum(t.Context(), status, []string{"a", "b"})

			// Assert
			require.Equal(t, test.wantQuorum, quorum)
			require.Equal(t, test.wantAuthRequired, authRequired)
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

func TestRemoveBootstrapCredential(t *testing.T) {
	// Arrange
	path := filepath.Join(t.TempDir(), "etcd-root-password")
	require.NoError(t, os.WriteFile(path, []byte("secret"), 0o600))

	// Act
	err := removeBootstrapCredential(path)
	missingErr := removeBootstrapCredential(path)

	// Assert
	require.NoError(t, err)
	require.NoError(t, missingErr)
	_, statErr := os.Stat(path)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func startupStatus(clusterID, memberID, leaderID uint64) *clientv3.StatusResponse {
	return &clientv3.StatusResponse{
		Header: &etcdserverpb.ResponseHeader{ClusterId: clusterID, MemberId: memberID},
		Leader: leaderID,
	}
}
