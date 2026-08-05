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
		EtcdEndpoints:    []string{"https://10.0.0.1:2379", "https://10.0.0.2:2379"},
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

	hostname := valid
	hostname.EtcdEndpoints = []string{"https://ha-a:2379"}
	require.ErrorContains(t, hostname.Validate(), "must use an IP address")

	tests := []struct {
		name      string
		configure func(*Config)
		wantError string
	}{
		{
			name: "lease duration has sub-millisecond precision",
			configure: func(config *Config) {
				config.LeaseDuration = 1500 * time.Microsecond
				config.RenewInterval = 1200 * time.Microsecond
			},
			wantError: "lease duration must be a positive whole number of milliseconds",
		},
		{
			name: "renew interval is not positive",
			configure: func(config *Config) {
				config.RenewInterval = 0
			},
			wantError: "renew and retry intervals must be positive",
		},
		{
			name: "retry interval is not positive",
			configure: func(config *Config) {
				config.RetryInterval = -time.Second
			},
			wantError: "renew and retry intervals must be positive",
		},
		{
			name: "renew interval reaches lease duration",
			configure: func(config *Config) {
				config.RenewInterval = config.LeaseDuration
			},
			wantError: "renew interval must be less than the lease duration",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Arrange
			config := valid
			test.configure(&config)

			// Act
			err := config.Validate()

			// Assert
			require.ErrorContains(t, err, test.wantError)
		})
	}
}
