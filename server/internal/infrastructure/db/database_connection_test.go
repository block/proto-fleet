package db

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnectAndMigrateRejectsSingleConnectionPool(t *testing.T) {
	t.Parallel()

	connection, err := ConnectAndMigrate(&Config{MaxOpenConns: 1})

	require.Nil(t, connection)
	require.EqualError(t, err,
		"DB_MAX_OPEN_CONNS cannot be 1: database migrations require at least two connections")
}
