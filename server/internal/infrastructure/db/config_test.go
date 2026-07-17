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

func TestConfigValidateAcceptsLegacyFields(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Name:     "fleet",
		Username: "fleet",
		Password: "p@ss word",
		Address:  "db.internal:5432",
		SSLMode:  "verify-full",
	}

	require.NoError(t, cfg.Validate())
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

func TestConfigValidateAcceptsMultiHostReadWriteDSN(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ExplicitDSN: "postgres://fleet@fleet-a:5432,fleet-b:5432/fleet?sslmode=disable&target_session_attrs=read-write",
	}

	require.NoError(t, cfg.Validate())
	require.True(t, cfg.UsesExplicitDSN())
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

func TestConfigValidateAcceptsSingleHostExplicitDSNWithTLSFallbacks(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		dsn  string
	}{
		{
			name: "default sslmode prefer",
			dsn:  "postgres://fleet@fleet-a:5432/fleet",
		},
		{
			name: "explicit sslmode prefer",
			dsn:  "postgres://fleet@fleet-a:5432/fleet?sslmode=prefer",
		},
		{
			name: "sslmode allow",
			dsn:  "postgres://fleet@fleet-a:5432/fleet?sslmode=allow",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cfg := Config{
				ExplicitDSN: testCase.dsn,
			}

			require.NoError(t, cfg.Validate())
		})
	}
}

func TestConfigValidateReturnsGenericInvalidDSNError(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ExplicitDSN: "postgres://fleet:secret@tail@%%",
	}

	err := cfg.Validate()

	require.Error(t, err)
	require.EqualError(t, err, "invalid database DSN")
	require.NotContains(t, err.Error(), "secret")
	require.NotContains(t, err.Error(), "tail")
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

func TestConfigValidateAcceptsEnvExpandedMultiHostWithReadWriteTarget(t *testing.T) {
	t.Setenv("PGHOST", "fleet-a,fleet-b")
	t.Setenv("PGPORT", "5432,5432")
	t.Setenv("PGTARGETSESSIONATTRS", "read-write")

	cfg := Config{
		ExplicitDSN: "postgres:///fleet?sslmode=disable",
	}

	require.NoError(t, cfg.Validate())
}

func TestConfigValidateRejectsEnvExpandedMultiHostWithoutReadWriteTarget(t *testing.T) {
	t.Setenv("PGHOST", "fleet-a,fleet-b")
	t.Setenv("PGPORT", "5432,5432")

	cfg := Config{
		ExplicitDSN: "postgres:///fleet?sslmode=disable",
	}

	err := cfg.Validate()

	require.Error(t, err)
	require.Contains(t, err.Error(), "target_session_attrs=read-write")
}
