package sqlstores_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

// This opt-in investigation measures the actual SQL consumers against a million
// target rows. It uses the normal isolated database harness; the history index
// is dropped and recreated only in that disposable database. Normal test runs
// do not build the large fixture or rely on machine-specific timing thresholds.
func TestReleaseChannelSuppressionQueryPlans(t *testing.T) {
	output := os.Getenv("ROLLOUT_SUPPRESSION_QUERY_PLANS")
	if testing.Short() || output == "" {
		t.Skip("set ROLLOUT_SUPPRESSION_QUERY_PLANS to an output directory to capture large-history EXPLAIN plans")
	}
	f := newReleaseChannelQueryFixture(t)
	channel := f.channel("current")
	f.exec(`INSERT INTO release_channel_target (channel_id, target_type, target_id) VALUES ($1, 'site', $2)`, channel, f.site)
	for i := range 100 {
		f.device(fmt.Sprintf("history-miner-%02d", i), "Bitmain", "S19", "v1")
	}
	f.exec(`INSERT INTO release_channel_firmware (channel_id, manufacturer, model, firmware_checksum, firmware_version, assignment_generation, assigned_by)
		VALUES ($1, 'Bitmain', 'S19', 'sum', 'v2', 1, 1)`, channel)
	f.exec(`INSERT INTO release_channel (org_id, name, created_by)
		SELECT $1, 'history-' || n, 1 FROM generate_series(1, 10) n`, f.org)
	f.exec(`INSERT INTO firmware_rollout (org_id, channel_id, manufacturer, model, firmware_checksum, firmware_version, assignment_generation, status, created_at)
		SELECT $1, c.id, 'Bitmain', 'S19', 'sum', 'v2', generation, 'completed_with_failures', now() - n * interval '1 minute'
		FROM release_channel c CROSS JOIN generate_series(1, 10) generation CROSS JOIN generate_series(1, 100) n
		WHERE c.id <> $2`, f.org, channel)
	f.exec(`INSERT INTO firmware_rollout (org_id, channel_id, manufacturer, model, firmware_checksum, firmware_version, assignment_generation, status, created_at)
		SELECT $1, $2, 'Bitmain', 'S19', 'sum', 'v2', 1, 'completed_with_failures', now() - n * interval '1 minute'
		FROM generate_series(1, 100) n`, f.org, channel)
	f.exec(`INSERT INTO firmware_rollout_device (rollout_id, device_id, halted_at, halt_reason)
		SELECT r.id, d.id, now(), 'failed'
		FROM firmware_rollout r CROSS JOIN device d`)
	f.exec(`ANALYZE`)
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM firmware_rollout_device`).Scan(&count))
	require.Equal(t, 1010000, count)
	body, err := os.ReadFile("../../../../sqlc/queries/release_channel.sql")
	require.NoError(t, err)
	params := map[string]any{
		"org_id": f.org, "channel_id": channel, "manufacturer": "Bitmain", "model": "S19",
		"firmware_version": "v2", "firmware_checksum": "sum", "assigned_file_ids": "{}",
		"rollout_id": int64(0), "assignment_generation": int64(1),
	}
	require.NoError(t, os.MkdirAll(output, 0700))
	planConn, err := f.db.Conn(t.Context())
	require.NoError(t, err)
	defer func() { require.NoError(t, planConn.Close()) }()
	planExec := func(query string) {
		_, err := planConn.ExecContext(t.Context(), query)
		require.NoError(t, err)
	}
	readPlan := func(query string, args ...any) string {
		rows, err := planConn.QueryContext(t.Context(), "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+query, args...)
		require.NoError(t, err)
		defer func() { require.NoError(t, rows.Close()) }()
		var plan strings.Builder
		for rows.Next() {
			var line string
			require.NoError(t, rows.Scan(&line))
			plan.WriteString(line + "\n")
		}
		require.NoError(t, rows.Err())
		return plan.String()
	}
	capture := func(label string) {
		for _, name := range []string{"ListReleaseChannelMismatchedMembers", "ListReleaseChannelSuppressedMembers", "ListReleaseChannelFirmwareNeedingRollout"} {
			start := strings.Index(string(body), "-- name: "+name+" :many")
			require.NotEqual(t, -1, start)
			query := strings.SplitN(string(body)[start:], "-- name:", 3)[1]
			query = query[strings.Index(query, "\n")+1:]
			var args []any
			query = regexp.MustCompile(`sqlc.arg\('([^']+)'\)`).ReplaceAllStringFunc(query, func(match string) string {
				key := strings.TrimSuffix(strings.TrimPrefix(match, "sqlc.arg('"), "')")
				args = append(args, params[key])
				return fmt.Sprintf("$%d", len(args))
			})
			prepared := strings.HasSuffix(label, "generic")
			if prepared {
				planExec(`SET plan_cache_mode = force_generic_plan`)
				planExec("PREPARE suppression_check AS " + query)
				literals := make([]string, len(args))
				for i, value := range args {
					if text, ok := value.(string); ok {
						literals[i] = "'" + strings.ReplaceAll(text, "'", "''") + "'"
					} else {
						literals[i] = fmt.Sprint(value)
					}
				}
				query = "EXECUTE suppression_check"
				if len(literals) > 0 {
					query += "(" + strings.Join(literals, ", ") + ")"
				}
				args = nil
			}
			plan := readPlan(query, args...)
			if prepared {
				planExec("DEALLOCATE suppression_check")
				planExec(`SET plan_cache_mode = auto`)
			}
			require.NoError(t, os.WriteFile(filepath.Join(output, label+"-"+name+".txt"), []byte(plan), 0600))
			t.Logf("%s %s: %s", label, name, plan[strings.LastIndex(plan, "Execution Time:"):])
		}
	}
	verifySuppression := func() {
		mismatched, err := f.q.ListReleaseChannelMismatchedMembers(t.Context(), sqlc.ListReleaseChannelMismatchedMembersParams{
			OrgID: f.org, ChannelID: channel, Manufacturer: "Bitmain", Model: "S19",
			FirmwareVersion: "v2", FirmwareChecksum: "sum", AssignmentGeneration: 1,
		})
		require.NoError(t, err)
		require.Empty(t, mismatched)
		suppressed, err := f.q.ListReleaseChannelSuppressedMembers(t.Context(), sqlc.ListReleaseChannelSuppressedMembersParams{
			OrgID: f.org, ChannelID: channel, Manufacturer: "Bitmain", Model: "S19", AssignmentGeneration: 1,
		})
		require.NoError(t, err)
		require.Len(t, suppressed, 100)
		needingRollout, err := f.q.ListReleaseChannelFirmwareNeedingRollout(t.Context())
		require.NoError(t, err)
		require.Empty(t, needingRollout)
	}
	f.exec(`DROP INDEX IF EXISTS idx_firmware_rollout_assignment_history`)
	verifySuppression()
	for i := range 3 {
		capture(fmt.Sprintf("before-%d", i+1))
	}
	capture("before-generic")
	f.exec(`CREATE INDEX idx_firmware_rollout_assignment_history ON firmware_rollout
		(channel_id, release_channel_pair_key(manufacturer), release_channel_pair_key(model), assignment_generation, created_at DESC, id DESC)`)
	f.exec(`ANALYZE firmware_rollout`)
	verifySuppression()
	for i := range 3 {
		capture(fmt.Sprintf("indexed-%d", i+1))
	}
	capture("indexed-generic")
}
