package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/block/proto-fleet/server/migrations"
)

const curtailmentAuthorizationEnvelopeBridgePath = "bridges/000143_legacy_curtailment_authorization_envelopes.sql"

// runMigrationCompatibilityBridges prepares legacy schemas for immutable
// migrations whose original deployment preconditions turned out not to hold in
// the field. A bridge must produce the exact final schema owned by its migration
// and advance schema_migrations atomically in the same transaction.
func runMigrationCompatibilityBridges(ctx context.Context, conn *sql.DB) error {
	exists, err := schemaMigrationsTableExists(ctx, conn)
	if err != nil {
		return err
	}
	if !exists {
		// Fresh database: golang-migrate owns the complete bootstrap.
		return nil
	}

	return bridgeCurtailmentAuthorizationEnvelopes(ctx, conn)
}

func schemaMigrationsTableExists(ctx context.Context, conn *sql.DB) (bool, error) {
	var exists bool
	if err := conn.QueryRowContext(ctx,
		"SELECT to_regclass('schema_migrations') IS NOT NULL").Scan(&exists); err != nil {
		return false, fmt.Errorf("inspect schema_migrations table: %w", err)
	}
	return exists, nil
}

// bridgeCurtailmentAuthorizationEnvelopes supports two upgrade starting points:
//
//   - v0.2.10-rc.15: schema version 142, clean, with pre-contract rows;
//   - v0.3.0-rc.1 after its guard fired: schema version 143, dirty, with neither
//     authorization_envelope_jsonb column present.
//
// Existing executable scopes are preserved. Their envelope is intentionally
// organization-wide so the new runtime requires live organization-wide
// permission before reuse instead of reconstructing historical grants from
// today's topology.
func bridgeCurtailmentAuthorizationEnvelopes(ctx context.Context, conn *sql.DB) error {
	// nolint:forbidigo // Migration DDL and schema_migrations locking must share one raw SQL transaction.
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin migration 143 compatibility bridge: %w", err)
	}
	defer tx.Rollback()

	var version int
	var dirty bool
	if err := tx.QueryRowContext(ctx,
		"SELECT version, dirty FROM schema_migrations FOR UPDATE").Scan(&version, &dirty); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// postgres.WithInstance creates schema_migrations before a fresh
			// database has a version row. Ordered migrations own that bootstrap.
			return nil
		}
		return fmt.Errorf("read migration version for compatibility bridge: %w", err)
	}

	if !(version == 142 && !dirty) && !(version == 143 && dirty) {
		return nil
	}

	var responseProfiles int64
	var events int64
	if err := tx.QueryRowContext(ctx, `
		SELECT
			(SELECT count(*) FROM curtailment_response_profile),
			(SELECT count(*) FROM curtailment_event)
	`).Scan(&responseProfiles, &events); err != nil {
		return fmt.Errorf("count pre-contract curtailment rows: %w", err)
	}

	var envelopeColumnCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name IN ('curtailment_response_profile', 'curtailment_event')
		  AND column_name = 'authorization_envelope_jsonb'
	`).Scan(&envelopeColumnCount); err != nil {
		return fmt.Errorf("inspect migration 143 envelope columns: %w", err)
	}

	if envelopeColumnCount != 0 {
		return fmt.Errorf(
			"migration 143 compatibility bridge found %d of 2 envelope columns at version %d dirty=%t; "+
				"refusing to guess at a partially applied schema",
			envelopeColumnCount, version, dirty,
		)
	}

	if responseProfiles == 0 && events == 0 {
		if version == 143 && dirty {
			// RC.1 marked the version dirty before its zero-row guard ran. With no
			// rows and no columns, resetting to 142 lets the immutable migration run
			// normally.
			if _, err := tx.ExecContext(ctx, `
				UPDATE schema_migrations
				SET version = 142, dirty = false
				WHERE version = 143 AND dirty
			`); err != nil {
				return fmt.Errorf("reset empty dirty migration 143: %w", err)
			}
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("commit empty migration 143 reset: %w", err)
			}
			slog.Warn("reset dirty migration 143 after confirming no curtailment rows or schema changes")
		}
		return nil
	}

	bridgeSQL, err := fs.ReadFile(migrations.Bridges, curtailmentAuthorizationEnvelopeBridgePath)
	if err != nil {
		return fmt.Errorf("read migration 143 compatibility bridge: %w", err)
	}
	if _, err := tx.ExecContext(ctx, string(bridgeSQL)); err != nil {
		return fmt.Errorf("apply migration 143 compatibility bridge: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration 143 compatibility bridge: %w", err)
	}

	slog.Warn("preserved pre-contract curtailment rows with conservative authorization envelopes",
		"response_profiles", responseProfiles,
		"events", events,
	)
	return nil
}
