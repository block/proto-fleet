package db

import (
	"strings"
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
		"postgres://fleet:p%40ss+word@db.internal:5432/fleet?sslmode=verify-full",
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

	explicit := "postgres://ha:secret@fleet-a:5432/fleet?sslmode=disable"
	cfg := Config{
		ExplicitDSN: explicit,
		Name:        "ignored",
		Username:    "ignored",
		Password:    "ignored",
		Address:     "ignored:5432",
		SSLMode:     "verify-full",
	}

	require.Equal(t, explicit, cfg.DSN())
	require.Equal(t, "postgres://ha:xxxxx@fleet-a:5432/fleet?sslmode=disable", cfg.RedactedDSN())
	require.Equal(t, cfg.RedactedDSN(), cfg.ConnectionTarget())
}

func TestConfigValidateAcceptsMultiHostReadWriteDSN(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ExplicitDSN: "postgres://fleet:secret@fleet-a:5432,fleet-b:5432/fleet?sslmode=disable&target_session_attrs=read-write",
	}

	require.NoError(t, cfg.Validate())
	require.True(t, cfg.UsesExplicitDSN())
	require.True(t, DSNLooksMultiHost(cfg.DSN()))
	require.True(t, DSNHasReadWriteTarget(cfg.DSN()))
}

func TestConfigValidateRejectsMultiHostDSNWithoutReadWriteTarget(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ExplicitDSN: "postgres://fleet:secret@fleet-a:5432,fleet-b:5432/fleet?sslmode=disable",
	}

	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "target_session_attrs=read-write")
}

func TestConfigValidateRejectsURLQueryMultiHostDSNWithoutReadWriteTarget(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ExplicitDSN: "postgres:///fleet?host=fleet-a,fleet-b&port=5432,5432&sslmode=disable",
	}

	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "target_session_attrs=read-write")
	require.True(t, DSNLooksMultiHost(cfg.DSN()))
}

func TestConfigValidateAcceptsURLQueryMultiHostReadWriteDSN(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ExplicitDSN: "postgres:///fleet?host=fleet-a,fleet-b&port=5432,5432&sslmode=disable&target_session_attrs=read-write",
	}

	require.NoError(t, cfg.Validate())
	require.True(t, DSNLooksMultiHost(cfg.DSN()))
	require.True(t, DSNHasReadWriteTarget(cfg.DSN()))
}

func TestRedactDSNURLPassword(t *testing.T) {
	t.Parallel()

	dsn := "postgres://fleet:super-secret@fleet-a:5432/fleet?sslmode=disable&target_session_attrs=read-write"

	redacted := RedactDSN(dsn)

	require.Equal(t, "postgres://fleet:xxxxx@fleet-a:5432/fleet?sslmode=disable&target_session_attrs=read-write", redacted)
	require.NotContains(t, redacted, "super-secret")
}

func TestRedactDSNURLQueryPassword(t *testing.T) {
	t.Parallel()

	dsn := "postgres://fleet@fleet-a:5432/fleet?password=super-secret&sslmode=disable&sslpassword=cert-secret"

	redacted := RedactDSN(dsn)

	require.Contains(t, redacted, "password=xxxxx")
	require.Contains(t, redacted, "sslpassword=xxxxx")
	require.NotContains(t, redacted, "super-secret")
	require.NotContains(t, redacted, "cert-secret")
}

func TestConfigValidateRedactsURLQueryPassword(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ExplicitDSN: "postgres://fleet@fleet-a:5432/fleet?password=super-secret&sslmode=invalid",
	}

	err := cfg.Validate()

	require.Error(t, err)
	require.NotContains(t, err.Error(), "super-secret")
	require.Contains(t, err.Error(), "password=xxxxx")
}

func TestRedactDSNMalformedURLPassword(t *testing.T) {
	t.Parallel()

	dsn := "postgres://fleet:super-secret@%%"

	redacted := RedactDSN(dsn)

	require.Equal(t, "postgres://fleet:xxxxx@%%", redacted)
	require.NotContains(t, redacted, "super-secret")
}

func TestRedactDSNKeywordPassword(t *testing.T) {
	t.Parallel()

	dsn := "host=fleet-a,fleet-b port=5432,5432 user=fleet password='super secret' dbname=fleet target_session_attrs=read-write"

	redacted := RedactDSN(dsn)

	require.Contains(t, redacted, "password=xxxxx")
	require.NotContains(t, redacted, "super secret")
}

func TestRedactDSNKeywordSSLPassword(t *testing.T) {
	t.Parallel()

	dsn := "host=fleet-a user=fleet password=super-secret sslpassword='cert secret' dbname=fleet sslmode=verify-full"

	redacted := RedactDSN(dsn)

	require.Contains(t, redacted, "password=xxxxx")
	require.Contains(t, redacted, "sslpassword=xxxxx")
	require.NotContains(t, redacted, "super-secret")
	require.NotContains(t, redacted, "cert secret")
}

func TestRedactDSNDoubleQuotedKeywordPassword(t *testing.T) {
	t.Parallel()

	dsn := `host=fleet-a,fleet-b port=5432,5432 user=fleet password="super secret" dbname=fleet target_session_attrs=read-write`

	redacted := RedactDSN(dsn)

	require.Contains(t, redacted, "password=xxxxx")
	require.NotContains(t, redacted, "super secret")
}

func TestRedactDSNEscapedQuotedKeywordPassword(t *testing.T) {
	t.Parallel()

	dsn := `host=fleet-a user=fleet password='abc\'def' dbname=fleet`

	redacted := RedactDSN(dsn)

	require.Contains(t, redacted, "password=xxxxx")
	require.NotContains(t, redacted, "abc")
	require.NotContains(t, redacted, "def")
}

func TestRedactDSNEscapedUnquotedKeywordPassword(t *testing.T) {
	t.Parallel()

	dsn := `host=fleet-a user=fleet password=abc\ def dbname=fleet`

	redacted := RedactDSN(dsn)

	require.Contains(t, redacted, "password=xxxxx")
	require.NotContains(t, redacted, "abc")
	require.NotContains(t, redacted, "def")
}

func TestConfigValidateRedactsEscapedKeywordPassword(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ExplicitDSN: `host=fleet-a user=fleet password='abc\'def' dbname=fleet sslmode=invalid`,
	}

	err := cfg.Validate()

	require.Error(t, err)
	require.NotContains(t, err.Error(), "abc")
	require.NotContains(t, err.Error(), "def")
	require.Contains(t, err.Error(), "password=xxxxx")
}

func TestConfigValidateRedactsKeywordSSLPassword(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ExplicitDSN: `host=fleet-a user=fleet sslpassword='cert secret' dbname=fleet sslmode=invalid`,
	}

	err := cfg.Validate()

	require.Error(t, err)
	require.NotContains(t, err.Error(), "cert secret")
	require.Contains(t, err.Error(), "sslpassword=xxxxx")
}

func TestDSNHelpersSupportKeywordDSN(t *testing.T) {
	t.Parallel()

	dsn := "host=fleet-a,fleet-b port=5432,5432 user=fleet password=secret dbname=fleet target_session_attrs=read-write"

	require.True(t, DSNLooksMultiHost(dsn))
	require.True(t, DSNHasReadWriteTarget(dsn))
	require.False(t, strings.Contains(RedactDSN(dsn), "secret"))
}
