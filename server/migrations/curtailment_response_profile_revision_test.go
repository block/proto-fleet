package migrations_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/migrations"
)

func TestResponseProfileRevisionMigrationLocksWritersBeforeBackfill(t *testing.T) {
	migrationBytes, err := migrations.Migrations.ReadFile("000144_curtailment_response_profile_revision.up.sql")
	require.NoError(t, err)
	migrationSQL := string(migrationBytes)

	canonicalizationIndex := strings.Index(migrationSQL, "UPDATE curtailment_response_profile\nSET scope_json")
	require.NotEqual(t, -1, canonicalizationIndex)

	for _, lockStatement := range []string{
		"LOCK TABLE curtailment_response_profile IN SHARE MODE;",
		"LOCK TABLE curtailment_automation_rule IN SHARE MODE;",
	} {
		lockIndex := strings.Index(migrationSQL, lockStatement)
		require.NotEqual(t, -1, lockIndex)
		require.Less(t, lockIndex, canonicalizationIndex)
	}
}
