package deployment

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProbeLocalEtcdTLSRejectsUntrustedServer(t *testing.T) {
	// Arrange
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	trustedRoots := x509.NewCertPool()
	trustedRoots.AddCert(server.Certificate())
	serverName := server.Certificate().DNSNames[0]

	for _, test := range []struct {
		name    string
		roots   *x509.CertPool
		wantErr bool
	}{
		{name: "trusted", roots: trustedRoots},
		{name: "untrusted", roots: x509.NewCertPool(), wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Act
			ready, err := probeLocalEtcdTLS(t.Context(), server.Listener.Addr().String(), &tls.Config{RootCAs: test.roots, ServerName: serverName, MinVersion: tls.VersionTLS12})

			// Assert
			require.Equal(t, !test.wantErr, ready)
			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
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
