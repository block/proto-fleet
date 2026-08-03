package ha

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/runtimejobs"
	"github.com/stretchr/testify/require"
)

func TestNewConfiguredRuntimeUsesStandaloneModeWithoutReadingHAFiles(t *testing.T) {
	group, err := runtimejobs.NewGroup(nil)
	require.NoError(t, err)

	runtime, cleanup, err := NewConfiguredRuntime(Config{
		EtcdPasswordFile: filepath.Join(t.TempDir(), "missing-password"),
		ServiceCAFile:    filepath.Join(t.TempDir(), "missing-ca"),
	}, nil, group, alwaysHealthy)
	require.NoError(t, err)
	require.NotNil(t, runtime)
	require.Nil(t, runtime.owner)
	require.NoError(t, cleanup())
}

func TestHAConfigValidation(t *testing.T) {
	valid := Config{
		Enabled:          true,
		ClusterPath:      "/service/proto-fleet",
		EtcdEndpoints:    []string{"https://ha-a:2379", "https://ha-b:2379"},
		EtcdUsername:     "fleet-observer",
		EtcdPasswordFile: "/run/secrets/etcd-password",
		ServiceCAFile:    "/etc/proto-fleet/ha/service-ca.crt",
		LeaseDuration:    10 * time.Second,
		RenewInterval:    3 * time.Second,
		RetryInterval:    time.Second,
		DialTimeout:      5 * time.Second,
	}
	require.NoError(t, valid.Validate())

	require.Error(t, (Config{Enabled: true}).Validate())

	insecure := valid
	insecure.EtcdEndpoints = []string{"http://ha-a:2379"}
	require.ErrorContains(t, insecure.Validate(), "must be an HTTPS URL")
}
