package deployment

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProbeLocalEtcdListenerAcceptsPreQuorumProcess(t *testing.T) {
	// Arrange
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	// Act
	ready := probeLocalEtcdListener(t.Context(), listener.Addr().String())

	// Assert
	require.True(t, ready)
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
			want:         []string{"--env-file", "node.env", "--file", infrastructureCompose, "--profile", "database", "down"},
		},
		{
			name: "witness",
			want: []string{"--env-file", "node.env", "--file", infrastructureCompose, "down"},
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
