// seedtelemetry generates realistic simulated telemetry data for testing
// the Proto Fleet dashboard charts. It inserts data into the device_metrics
// hypertable and refreshes all continuous aggregates.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"

	"github.com/alecthomas/kong"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const dbDriverName = "pgx"

func main() {
	cfg := &config{}
	kong.Parse(cfg, kong.Name("seedtelemetry"))
	if err := cfg.validate(); err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	if cfg.CleanUp {
		if err := runCleanup(*cfg); err != nil {
			log.Fatalf("Error: %v", err)
		}
		return
	}

	end := time.Now().UTC().Truncate(time.Minute)
	start := end.Add(-time.Duration(cfg.Days) * 24 * time.Hour)
	pointsPerDevice := int(end.Sub(start) / cfg.Interval)
	totalRows := pointsPerDevice * cfg.Devices

	log.Printf("Seed Telemetry Generator")
	log.Printf("  devices:    %d", cfg.Devices)
	log.Printf("  time range: %s → %s (%d days)", start.Format(time.RFC3339), end.Format(time.RFC3339), cfg.Days)
	log.Printf("  interval:   %s", cfg.Interval)
	log.Printf("  points/dev: %d", pointsPerDevice)
	log.Printf("  total rows: %d", totalRows)
	log.Printf("  batch size: %d", cfg.BatchSize)
	log.Printf("  outliers:   %t", cfg.Outliers)

	if cfg.DryRun {
		log.Printf("Dry run — no data inserted.")
		return
	}

	profiles := buildProfiles(cfg.Devices)

	log.Printf("Generating %d data points...", totalRows)
	genStart := time.Now()
	metrics := generateMetrics(profiles, start, end, cfg.Interval, cfg.Outliers)
	log.Printf("  generated %d points in %s", len(metrics), time.Since(genStart).Round(time.Millisecond))

	if err := seedAndRefresh(*cfg, metrics, start, end, genStart); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func openDB(cfg config) (*sql.DB, error) {
	db, err := sql.Open(dbDriverName, cfg.dsn())
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}
	return db, nil
}

// A dedicated connection owns the exclusive session lock across raw batches and
// Timescale's committing refreshes. Closing this DB also releases the lock if an
// error prevents explicit unlock; it cannot leak into the application's pool.
func withSeedMaintenance(db *sql.DB, work func(context.Context, *sql.Conn, *sqlc.Queries) error) (result error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquiring telemetry maintenance connection: %w", err)
	}
	defer conn.Close()
	q := sqlc.New(conn)
	lockCtx, lockCancel := context.WithTimeout(ctx, 30*time.Second)
	err = q.LockTelemetrySeedMaintenance(lockCtx)
	lockCancel()
	if err != nil {
		return fmt.Errorf("waiting for telemetry maintenance: %w", err)
	}
	defer func() {
		unlockCtx, unlockCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer unlockCancel()
		if err := q.UnlockTelemetrySeedMaintenance(unlockCtx); err != nil && result == nil {
			result = fmt.Errorf("releasing telemetry maintenance: %w", err)
		}
	}()
	return work(ctx, conn, q)
}

func runCleanup(cfg config) error {
	db, err := openDB(cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	if cfg.DryRun {
		start, end, found, err := findSeedDataBounds(context.Background(), db)
		if err != nil {
			return err
		}
		log.Printf("Cleanup dry run: seed data present=%t, range %s to %s", found, start, end)
		return nil
	}
	return withSeedMaintenance(db, func(ctx context.Context, conn *sql.Conn, q *sqlc.Queries) error {
		start, end, found, err := findSeedDataBounds(ctx, conn)
		if err != nil {
			return err
		}
		if !found {
			log.Printf("Cleanup mode: no synthetic rows found for prefix %q", seedDevicePrefix)
			return nil
		}
		safeStart, err := q.PrepareTelemetrySeed(ctx, start)
		if err != nil {
			return err
		}
		if start.Before(safeStart) {
			return fmt.Errorf("seed cleanup begins at %s before complete retained coverage at %s; existing rows were preserved", start, safeStart)
		}
		log.Printf("Cleanup mode: seed window %s to %s", start, end)
		deleted, err := clearSeedData(ctx, conn)
		if err != nil {
			return err
		}
		log.Printf("  deleted %d rows; recovering dependent summaries", deleted)
		return recoverSeedSummaries(ctx, q)
	})
}

func seedAndRefresh(cfg config, metrics []deviceMetric, start, end, genStart time.Time) error {
	db, err := openDB(cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	return withSeedMaintenance(db, func(ctx context.Context, conn *sql.Conn, q *sqlc.Queries) error {
		oldStart, _, found, err := findSeedDataBounds(ctx, conn)
		if err != nil {
			return err
		}
		importStart := start
		if found && oldStart.Before(importStart) {
			importStart = oldStart
		}
		safeStart, err := q.PrepareTelemetrySeed(ctx, importStart)
		if err != nil {
			return err
		}
		// Never refresh a partially expired UTC day. A new, proven-empty source can
		// extend its initial boundary; established sources use their retained range.
		eligible := make([]deviceMetric, 0, len(metrics))
		for _, metric := range metrics {
			if !metric.Time.Before(safeStart) {
				eligible = append(eligible, metric)
			}
		}
		if len(eligible) == 0 {
			return fmt.Errorf("requested seed window %s to %s precedes complete retained coverage at %s; existing rows were preserved", start, end, safeStart)
		}
		if found && oldStart.Before(safeStart) {
			return fmt.Errorf("existing seed rows precede complete retained coverage at %s; existing rows were preserved", safeStart)
		}
		if len(eligible) != len(metrics) {
			log.Printf("  using complete retained coverage from %s; omitted %d older samples", safeStart, len(metrics)-len(eligible))
		}
		log.Printf("Clearing existing seed data...")
		//nolint:forbidigo // Keep the connection owning the import lock; a pooled transaction would block behind it.
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("beginning seed replacement: %w", err)
		}
		defer tx.Rollback()
		deleted, err := clearSeedData(ctx, tx)
		if err != nil {
			return fmt.Errorf("clearing seed data: %w", err)
		}
		log.Printf("  deleted %d rows", deleted)
		inserted, err := insertBatches(ctx, tx, eligible, cfg.BatchSize)
		if err != nil {
			return fmt.Errorf("inserting data: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing seed replacement: %w", err)
		}
		log.Printf("  inserted %d rows; recovering dependent summaries", inserted)
		if err := recoverSeedSummaries(ctx, q); err != nil {
			return err
		}
		log.Printf("Done. Total time: %s", time.Since(genStart).Round(time.Millisecond))
		return nil
	})
}

func buildProfiles(count int) []deviceProfile {
	profiles := make([]deviceProfile, count)
	for i := range count {
		identifier := fmt.Sprintf("%s%03d", seedDevicePrefix, i+1)
		profiles[i] = newDeviceProfile(identifier, i)
	}
	return profiles
}
