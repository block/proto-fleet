package monitoring_test

import (
	"database/sql"
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/stretchr/testify/require"
)

func TestNodeConnectivitySummary(t *testing.T) {
	db := dbtest.GetTestDB(t)
	ctx := t.Context()
	// Keep now() fixed for the heartbeat, admission, and clock-skew boundaries.
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
INSERT INTO organization(id, org_id, name) VALUES (1, 'monitor-test', 'Monitor test'), (2, 'other-org', 'Other org');
INSERT INTO "user"(id, user_id, username, password_hash) VALUES (1, 'monitor-test-user', 'monitor-test-user', '');
INSERT INTO fleet_node(id, org_id, name, identity_pubkey, encryption_pubkey, enrollment_status, created_at)
SELECT id, org_id, name, decode(lpad(to_hex(id), 64, '0'), 'hex'),
       decode(lpad(to_hex(id + 100), 64, '0'), 'hex'), 'CONFIRMED', now() - creation_age
FROM (VALUES
    (1, 1, 'recent-boundary', interval '1 hour'),
    (2, 1, 'overdue', interval '1 hour'),
    (3, 1, 'no-heartbeat', interval '1 hour'),
    (4, 1, 'too-far-ahead', interval '1 hour'),
    (5, 1, 'in-grace', interval '4 minutes 59 seconds'),
    (6, 1, 'excluded', interval '1 hour'),
    (7, 1, 'revoked', interval '1 hour'),
    (8, 1, 'deleted', interval '1 hour'),
    (9, 1, 're-enrolled', interval '1 hour'),
    (10, 1, 'cancelled-enrollment', interval '1 hour'),
    (11, 1, 'grace-boundary', interval '5 minutes'),
    (12, 2, 'allowed-clock-skew', interval '1 hour')
) AS nodes(id, org_id, name, creation_age);
UPDATE fleet_node SET enrollment_status = 'REVOKED' WHERE id = 7;
UPDATE fleet_node SET deleted_at = now() WHERE id = 8;
INSERT INTO fleet_node_heartbeat(fleet_node_id, org_id, last_seen_at) VALUES
    (1, 1, now() - interval '2 minutes'),
    (2, 1, now() - interval '3 minutes'),
    (4, 1, now() + interval '6 seconds'),
    (9, 1, now() - interval '10 minutes'),
    (10, 1, now() - interval '1 minute'),
    (12, 2, now() + interval '5 seconds');
INSERT INTO pending_enrollment(code_hash, org_id, created_by, fleet_node_id, status, created_at, expires_at, consumed_at) VALUES
    ('first-enrollment', 1, 1, 9, 'CONFIRMED', now() - interval '1 hour', now(), now() - interval '30 minutes'),
    ('re-enrollment', 1, 1, 9, 'CONFIRMED', now() - interval '15 minutes', now(), now() - interval '6 minutes'),
    ('cancelled-enrollment', 1, 1, 10, 'CANCELLED', now() - interval '15 minutes', now(), now() - interval '1 second');
INSERT INTO fleet_node_monitoring_exclusion(fleet_node_id, reason, created_at, expires_at) VALUES
    (6, 'planned work', now(), now() + interval '7 days'),
    (11, 'scheduled work', now() + interval '1 hour', now() + interval '2 hours');`)
	require.NoError(t, err)

	type summary struct {
		expected, recent, overdue, unknown, excluded, grace int64
		oldestAgeSeconds                                    float64
	}
	read := func(orgID int64) summary {
		t.Helper()
		var s summary
		err := tx.QueryRowContext(ctx, `
SELECT expected_count, recent_count, overdue_count, unknown_count, excluded_count, grace_count, oldest_heartbeat_age_seconds
FROM fleet_node_connectivity_summary WHERE org_id = $1`, orgID).
			Scan(&s.expected, &s.recent, &s.overdue, &s.unknown, &s.excluded, &s.grace, &s.oldestAgeSeconds)
		require.NoError(t, err)
		require.Equal(t, s.expected, s.recent+s.overdue+s.unknown)
		return s
	}
	want := summary{expected: 7, recent: 2, overdue: 1, unknown: 4, excluded: 1, grace: 1, oldestAgeSeconds: 180}
	require.Equal(t, want, read(1))
	require.Equal(t, summary{expected: 1, recent: 1}, read(2))

	_, err = tx.ExecContext(ctx, `UPDATE fleet_node_heartbeat SET last_seen_at = now() + interval '6 seconds' WHERE fleet_node_id = 12`)
	require.NoError(t, err)
	require.Equal(t, summary{expected: 1, unknown: 1}, read(2))

	_, err = tx.ExecContext(ctx, `UPDATE organization SET deleted_at = now() WHERE id = 2`)
	require.NoError(t, err)
	var orgID int64
	require.ErrorIs(t, tx.QueryRowContext(ctx, `SELECT org_id FROM fleet_node_connectivity_summary WHERE org_id = 2`).Scan(&orgID), sql.ErrNoRows)

	// Legacy snapshot changes must never manufacture a current heartbeat.
	_, err = tx.ExecContext(ctx, `UPDATE fleet_node SET last_seen_at = now() WHERE id = 3`)
	require.NoError(t, err)
	require.Equal(t, want, read(1))
	_, err = tx.ExecContext(ctx, `
UPDATE fleet_node_monitoring_exclusion SET created_at = now() - interval '2 hours', expires_at = now()
WHERE fleet_node_id = 6`)
	require.NoError(t, err)
	require.Equal(t, summary{expected: 8, recent: 2, overdue: 1, unknown: 5, grace: 1, oldestAgeSeconds: 180}, read(1))
	_, err = tx.ExecContext(ctx, `UPDATE fleet_node SET enrollment_status = 'REVOKED' WHERE org_id = 1`)
	require.NoError(t, err)
	require.Equal(t, summary{}, read(1))
}

func TestNodeMonitoringExclusionExpiry(t *testing.T) {
	db := dbtest.GetTestDB(t)
	ctx := t.Context()
	_, err := db.ExecContext(ctx, `
INSERT INTO organization(id, org_id, name) VALUES (1, 'exclusion-test', 'Exclusion test');
INSERT INTO fleet_node(id, org_id, name, identity_pubkey, encryption_pubkey, enrollment_status)
VALUES (1, 1, 'node', decode(repeat('01', 32), 'hex'), decode(repeat('02', 32), 'hex'), 'CONFIRMED');`)
	require.NoError(t, err)

	for _, interval := range []string{"0 seconds", "7 days 1 second", "infinity"} {
		t.Run(interval, func(t *testing.T) {
			_, err := db.ExecContext(t.Context(), `
INSERT INTO fleet_node_monitoring_exclusion(fleet_node_id, reason, expires_at)
VALUES (1, 'planned work', CASE WHEN $1 = 'infinity' THEN 'infinity'::timestamptz ELSE now() + $1::interval END)`, interval)
			require.ErrorContains(t, err, "ck_fleet_node_monitoring_exclusion_expiry")
		})
	}
}
