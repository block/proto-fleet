//go:build ha_fixture

package ha

import (
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbinfra "github.com/block/proto-fleet/server/internal/infrastructure/db"
)

func TestWriterObserverFixture(t *testing.T) {
	if os.Getenv("HA_WRITER_FIXTURE") != "1" {
		t.Skip("set HA_WRITER_FIXTURE=1 inside the writer-generation fixture")
	}

	config := &dbinfra.Config{
		ExplicitDSN:              requireEnv(t, "HA_WRITER_DB_DSN"),
		InitialConnectionTimeout: 3 * time.Second,
		MaxOpenConns:             4,
		MaxIdleConns:             2,
		ConnMaxLifetime:          time.Minute,
	}
	conn, err := dbinfra.ConnectAndMigrate(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })

	queries, err := dbinfra.NewPreparedQuerier(t.Context(), conn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, queries.Close()) })

	httpClient := &http.Client{Timeout: 3 * time.Second}
	observer, err := NewObserver(
		requireEnv(t, "HA_WRITER_CLUSTER_PATH"),
		NewEtcdHTTPClient(requireEnv(t, "HA_WRITER_DCS_ENDPOINT"), httpClient),
		queries,
		NewPatroniHTTPClient(httpClient),
	)
	require.NoError(t, err)

	first, err := observer.Observe(t.Context())
	require.NoError(t, err)
	second, err := observer.Observe(t.Context())
	require.NoError(t, err)

	require.Equal(t, first.DCSClusterID, second.DCSClusterID)
	require.Equal(t, first.LeaderName, second.LeaderName)
	require.Equal(t, first.WriterGeneration, second.WriterGeneration)
	require.Equal(t, first.ServerAddress, second.ServerAddress)
	require.Equal(t, first.Timeline, second.Timeline)

	if minimum := os.Getenv("HA_WRITER_MIN_GENERATION"); minimum != "" {
		value, parseErr := strconv.ParseInt(minimum, 10, 64)
		require.NoError(t, parseErr)
		require.Greater(t, second.WriterGeneration, value)
	}

	t.Logf(
		"HA_FIXTURE_OBSERVATION leader=%s writer_generation=%d cluster_id=%s timeline=%d",
		second.LeaderName,
		second.WriterGeneration,
		second.DCSClusterID,
		second.Timeline,
	)
}

func requireEnv(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	require.NotEmpty(t, value, "missing %s", name)
	return value
}
