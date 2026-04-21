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

func runCleanup(cfg config) error {
	db, err := openDB(cfg)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx := context.Background()
	start, end, found, err := findSeedDataBounds(ctx, db)
	if err != nil {
		return err
	}
	if !found {
		log.Printf("Cleanup mode: no synthetic rows found for prefix %q", seedDevicePrefix)
		return nil
	}

	refreshStart, refreshEnd := cleanupRefreshWindow(start, end)

	log.Printf("Cleanup mode")
	log.Printf("  seed window: %s → %s", start.Format(time.RFC3339), end.Format(time.RFC3339))
	log.Printf("  refresh:     %s → %s", refreshStart.Format(time.RFC3339), refreshEnd.Format(time.RFC3339))

	if cfg.DryRun {
		log.Printf("Dry run — no data deleted.")
		return nil
	}

	deleted, err := clearSeedData(ctx, db)
	if err != nil {
		return fmt.Errorf("clearing seed data: %w", err)
	}
	log.Printf("  deleted %d rows", deleted)

	if deleted == 0 {
		return nil
	}

	log.Printf("Refreshing continuous aggregates...")
	if err := refreshAggregates(ctx, db, refreshStart, refreshEnd); err != nil {
		return fmt.Errorf("refreshing aggregates: %w", err)
	}
	log.Printf("Cleanup complete.")
	return nil
}

func seedAndRefresh(cfg config, metrics []deviceMetric, start, end, genStart time.Time) error {
	db, err := openDB(cfg)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx := context.Background()

	log.Printf("Clearing existing seed data...")
	deleted, err := clearSeedData(ctx, db)
	if err != nil {
		return fmt.Errorf("clearing seed data: %w", err)
	}
	log.Printf("  deleted %d rows", deleted)

	log.Printf("Inserting %d rows...", len(metrics))
	insertStart := time.Now()
	inserted, err := insertBatches(ctx, db, metrics, cfg.BatchSize)
	if err != nil {
		return fmt.Errorf("inserting data: %w", err)
	}
	log.Printf("  inserted %d rows in %s", inserted, time.Since(insertStart).Round(time.Millisecond))

	log.Printf("Refreshing continuous aggregates...")
	refreshStart := time.Now()
	if err := refreshAggregates(ctx, db, start, end); err != nil {
		return fmt.Errorf("refreshing aggregates: %w", err)
	}
	log.Printf("  refreshed in %s", time.Since(refreshStart).Round(time.Millisecond))

	log.Printf("Done. Total time: %s", time.Since(genStart).Round(time.Millisecond))
	return nil
}

func buildProfiles(count int) []deviceProfile {
	profiles := make([]deviceProfile, count)
	for i := range count {
		identifier := fmt.Sprintf("%s%03d", seedDevicePrefix, i+1)
		profiles[i] = newDeviceProfile(identifier, i)
	}
	return profiles
}
