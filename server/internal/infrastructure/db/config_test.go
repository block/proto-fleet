package db

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigDSNUsesLegacyFieldsByDefault(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Name:     "fleet",
		Username: "fleet",
		Password: "p@ss word",
		Address:  "db.internal:5432",
		SSLMode:  "verify-full",
	}

	require.Equal(t,
		"postgres://fleet:p%40ss%20word@db.internal:5432/fleet?sslmode=verify-full",
		cfg.DSN(),
	)
	require.Equal(t, "db.internal:5432", cfg.ConnectionTarget())
}

func TestConfigExplicitDSNOverridesLegacyFields(t *testing.T) {
	t.Parallel()

	explicit := "postgres://ha@fleet-a:5432/fleet?sslmode=disable"
	cfg := Config{
		ExplicitDSN: explicit,
		Name:        "ignored",
		Username:    "ignored",
		Password:    "ignored",
		Address:     "ignored:5432",
		SSLMode:     "verify-full",
	}

	require.Equal(t, explicit, cfg.DSN())
	require.Equal(t, "DB_DSN", cfg.ConnectionTarget())
}

func TestConfigValidateRejectsSingleConnectionPool(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Address:      "127.0.0.1:5432",
		Name:         "fleet",
		MaxOpenConns: 1,
	}

	require.EqualError(t, cfg.Validate(),
		"DB_MAX_OPEN_CONNS cannot be 1: database migrations require at least two connections")
}

func TestConfigValidateAcceptsMultiHostReadWriteDSN(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ExplicitDSN: "postgres://fleet@fleet-a:5432,fleet-b:5432/fleet?sslmode=disable&target_session_attrs=read-write",
	}

	require.NoError(t, cfg.Validate())
}

func TestConfigValidateRejectsMultiHostDSNWithoutReadWriteTarget(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ExplicitDSN: "postgres://fleet@fleet-a:5432,fleet-b:5432/fleet?sslmode=disable",
	}

	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "target_session_attrs=read-write")
}

func TestConfigValidateAcceptsKeywordMultiHostReadWriteDSN(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ExplicitDSN: "host=fleet-a,fleet-b port=5432,5432 user=fleet dbname=fleet target_session_attrs=read-write",
	}

	require.NoError(t, cfg.Validate())
}

func TestConfigValidateRejectsKeywordMultiHostWithoutReadWriteTarget(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ExplicitDSN: "host=fleet-a,fleet-b port=5432,5432 user=fleet dbname=fleet sslmode=disable",
	}

	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "target_session_attrs=read-write")
}

func TestConfigValidateReturnsGenericInvalidDSNError(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ExplicitDSN: "postgres://fleet:secret@tail@%%",
	}

	err := cfg.Validate()

	require.Error(t, err)
	require.EqualError(t, err, "invalid database DSN")
}

func TestConfigValidateRejectsHostaddr(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		dsn  string
	}{
		{
			name: "URL query",
			dsn:  "postgres:///fleet?hostaddr=10.0.0.11&port=5432&sslmode=disable",
		},
		{
			name: "keyword",
			dsn:  "hostaddr=10.0.0.11 port=5432 user=fleet dbname=fleet sslmode=disable",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cfg := Config{ExplicitDSN: testCase.dsn}

			err := cfg.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), "hostaddr is not supported")
		})
	}
}

func TestConfigValidateRejectsPGHostaddrEnvironment(t *testing.T) {
	t.Setenv("PGHOSTADDR", "10.0.0.11")

	cfg := Config{
		ExplicitDSN: "postgres://fleet@fleet-a:5432/fleet?sslmode=disable",
	}

	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "hostaddr is not supported")
}

func TestConfigValidateHAAcceptsSecureMultiHostWriterDSN(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ExplicitDSN: "postgres://fleet@fleet-a:5432,fleet-b:5432/fleet?sslmode=verify-full&sslrootcert=system&target_session_attrs=read-write",
	}

	require.NoError(t, cfg.ValidateHA())
}

func TestConfigValidateHARejectsUnsupportedDatabaseTargets(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		config    Config
		wantError string
	}{
		{
			name: "legacy fields",
			config: Config{
				Address: "fleet-a:5432",
			},
			wantError: "explicit multi-host DB_DSN",
		},
		{
			name: "single host",
			config: Config{
				ExplicitDSN: "postgres://fleet@fleet-a:5432/fleet?sslmode=verify-full&sslrootcert=system&target_session_attrs=read-write",
			},
			wantError: "explicit multi-host DB_DSN",
		},
		{
			name: "no writer selection",
			config: Config{
				ExplicitDSN: "postgres://fleet@fleet-a:5432,fleet-b:5432/fleet?sslmode=verify-full&sslrootcert=system",
			},
			wantError: "target_session_attrs=read-write",
		},
		{
			name: "plaintext",
			config: Config{
				ExplicitDSN: "postgres://fleet@fleet-a:5432,fleet-b:5432/fleet?sslmode=disable&target_session_attrs=read-write",
			},
			wantError: "sslmode=verify-full and sslrootcert",
		},
		{
			name: "no trust root",
			config: Config{
				ExplicitDSN: "postgres://fleet@fleet-a:5432,fleet-b:5432/fleet?sslmode=verify-full&target_session_attrs=read-write",
			},
			wantError: "sslmode=verify-full and sslrootcert",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := testCase.config.ValidateHA()

			require.ErrorContains(t, err, testCase.wantError)
		})
	}
}
