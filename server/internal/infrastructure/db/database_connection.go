package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratedatabase "github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	migratesource "github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver

	"github.com/block/proto-fleet/server/migrations"
)

const (
	connectionRetryMaxAttempts       = 10
	connectionRetryInitialBackoff    = 500 * time.Millisecond
	connectionRetryMaxBackoff        = 5 * time.Second
	connectionRetryBackoffMultiplier = 2.0
)

// ConnectToDatabase establishes a connection to the database using the provided config.
// Returns a sql.DB connection with configured connection pooling settings.
func ConnectToDatabase(config *Config) (*sql.DB, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	conn, err := sql.Open("pgx", config.DSN())
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection for %s: %w", config.ConnectionTarget(), err)
	}

	conn.SetMaxOpenConns(config.MaxOpenConns)
	conn.SetMaxIdleConns(config.MaxIdleConns)
	conn.SetConnMaxLifetime(config.ConnMaxLifetime)
	RegisterIdleConnectionPoolReset(conn, config.MaxIdleConns)

	return conn, nil
}

// verifyConnectionEstablished retries pinging the database with exponential backoff
// until a connection is established or max attempts are exhausted. This handles the
// case where the application starts before the database is fully ready.
func verifyConnectionEstablished(ctx context.Context, conn *sql.DB, config *Config) error {
	var lastErr error
	backoff := connectionRetryInitialBackoff

	for attempt := 1; attempt <= connectionRetryMaxAttempts; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, config.InitialConnectionTimeout)
		lastErr = conn.PingContext(pingCtx)
		cancel()

		if lastErr == nil {
			return nil
		}

		if attempt == connectionRetryMaxAttempts {
			break
		}

		slog.Warn("database not ready, retrying",
			"attempt", attempt,
			"max_attempts", connectionRetryMaxAttempts,
			"retry_in", backoff,
			"error", lastErr)

		select {
		case <-ctx.Done():
			return fmt.Errorf("cancelled while waiting for database: %w", ctx.Err())
		case <-time.After(backoff):
		}

		backoff = time.Duration(float64(backoff) * connectionRetryBackoffMultiplier)
		if backoff > connectionRetryMaxBackoff {
			backoff = connectionRetryMaxBackoff
		}
	}

	return fmt.Errorf("failed to ping database after %d attempts: %w", connectionRetryMaxAttempts, lastErr)
}

// ConnectAndMigrate connects to the database and runs all pending migrations.
// Returns the database connection ready for use.
func ConnectAndMigrate(config *Config) (*sql.DB, error) {
	if config.MaxOpenConns == 1 {
		return nil, fmt.Errorf("DB_MAX_OPEN_CONNS cannot be 1: database migrations require at least two connections")
	}

	connection, err := ConnectToDatabase(config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Ensure connection is closed on any error to prevent resource leaks
	success := false
	defer func() {
		if !success {
			if closeErr := connection.Close(); closeErr != nil {
				slog.Warn("failed to close database connection after error", "error", closeErr)
			}
		}
	}()

	err = verifyConnectionEstablished(context.Background(), connection, config)
	if err != nil {
		return nil, fmt.Errorf("failed to verify database connection: %w", err)
	}

	slog.Info("connected to database", "target", config.ConnectionTarget(), "database", config.Name)

	err = runCurrentMigrations(context.Background(), connection, migrations.Current)
	if err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	success = true
	return connection, nil
}

// migrationCheckpoints derives provenance from the immutable migration filenames,
// not a second schema-version history. Public releases end at a shared version;
// internal releases must end at a private version (a no-op checkpoint is enough).
func migrationCheckpoints(files fs.FS) (map[int]bool, int, error) {
	paths, err := fs.Glob(files, "current/*.up.sql")
	if err != nil || len(paths) == 0 {
		return nil, 0, fmt.Errorf("current migration source is missing: %v", err)
	}
	checkpoints := make(map[int]bool, len(paths))
	latest, internal := 0, false
	for _, path := range paths {
		parts := strings.SplitN(strings.TrimPrefix(path, "current/"), "_", 3)
		version, err := strconv.Atoi(parts[0])
		if err != nil || len(parts) != 3 || version < 1000 || (parts[1] != "shared" && parts[1] != "internal") {
			return nil, 0, fmt.Errorf("invalid current migration: %s", path)
		}
		if _, duplicate := checkpoints[version]; duplicate {
			return nil, 0, fmt.Errorf("duplicate current migration: %d", version)
		}
		private := parts[1] == "internal"
		checkpoints[version], internal = private, internal || private
		latest = max(latest, version)
	}
	if internal && !checkpoints[latest] {
		return nil, 0, fmt.Errorf("internal releases must end at a private checkpoint")
	}
	if !internal {
		for version := range checkpoints {
			checkpoints[version] = true
		}
	}
	return checkpoints, latest, nil
}

// Internal shared steps remain dirty until a private checkpoint completes. A
// crash between files must never leave a clean shared version that the public
// application could mistake for its own database. All SQL, locking and version
// writes still belong to golang-migrate's stock PostgreSQL driver.
type checkpointDriver struct {
	migratedatabase.Driver
	checkpoints map[int]bool
}

func (d checkpointDriver) SetVersion(version int, dirty bool) error {
	if err := d.Driver.SetVersion(version, dirty || !d.checkpoints[version]); err != nil {
		return fmt.Errorf("record migration checkpoint: %w", err)
	}
	return nil
}

func runCurrentMigrations(ctx context.Context, pool *sql.DB, files fs.FS) error {
	checkpoints, latest, err := migrationCheckpoints(files)
	if err != nil {
		return err
	}
	return withBaselineLock(ctx, pool, func(conn *sql.Conn) error {
		if _, err := checkBaselineStartup(ctx, conn, files); err != nil {
			return err
		}
		source, err := iofs.New(files, "current")
		if err != nil {
			return fmt.Errorf("open current migrations: %w", err)
		}
		driver, err := postgres.WithConnection(ctx, conn, &postgres.Config{})
		if err != nil {
			return fmt.Errorf("initialize PostgreSQL migrator: %w", err)
		}
		m, err := migrate.NewWithInstance("current", source, "", checkpointDriver{driver, checkpoints})
		if err != nil {
			return fmt.Errorf("initialize current migrations: %w", err)
		}
		if err = runMigrations(m); err != nil {
			return err
		}
		status, err := checkBaselineStartup(ctx, conn, files)
		if err == nil && status.Version != latest {
			return fmt.Errorf("migration stopped before release checkpoint %d", latest)
		}
		return err
	})
}

// runMigrationsWithCompatibilityBridges runs immutable migrations around the
// exact schema versions where a legacy compatibility bridge must intervene.
func runMigrationsWithCompatibilityBridges(conn *sql.DB, config *Config) error {
	ctx := context.Background()
	m, driver, err := newMigrator(conn, config)
	if err != nil {
		return err
	}
	through142, err := newMigratorThrough(config.Name, driver, 142)
	if err != nil {
		return err
	}
	// Do not call either migrator's Close method: postgres.WithInstance treats
	// the supplied *sql.DB as owned and would close the application connection
	// returned by this function.

	// First recover an RC.1 database already left at version 143/dirty. The
	// golang-migrate lock prevents an older binary from racing the bridge and
	// overwriting its clean version marker.
	if err := runMigrationCompatibilityBridgesWithLock(ctx, conn, driver); err != nil {
		return fmt.Errorf("failed to run migration compatibility bridges: %w", err)
	}

	// v0.2.9 is version 130. Stop at 142 so legacy curtailment rows can be
	// prepared before immutable migration 143 evaluates its zero-row guard.
	if err := runMigrationsThroughVersion(through142, 142); err != nil {
		return err
	}
	if err := runMigrationCompatibilityBridgesWithLock(ctx, conn, driver); err != nil {
		return fmt.Errorf("failed to run migration compatibility bridges: %w", err)
	}

	return runMigrations(m)
}

// runMigrationCompatibilityBridgesWithLock serializes bridge SQL with every
// ordinary golang-migrate runner using the driver's database advisory lock.
func runMigrationCompatibilityBridgesWithLock(
	ctx context.Context,
	conn *sql.DB,
	driver migratedatabase.Driver,
) (err error) {
	if err := driver.Lock(); err != nil {
		return fmt.Errorf("acquire migration lock for compatibility bridge: %w", err)
	}
	defer func() {
		if unlockErr := driver.Unlock(); unlockErr != nil {
			unlockErr = fmt.Errorf("release migration lock for compatibility bridge: %w", unlockErr)
			if err == nil {
				err = unlockErr
			} else {
				err = errors.Join(err, unlockErr)
			}
		}
	}()

	return runMigrationCompatibilityBridges(ctx, conn)
}

// runMigrationsThroughVersion applies only upward migrations exposed by a
// source capped at target. Unlike Migrate(target), Up cannot downgrade a
// database another runner has already advanced beyond the boundary.
func runMigrationsThroughVersion(m *migrate.Migrate, target uint) error {
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to run migrations through version %d: %w", target, err)
	}
	return nil
}

// runMigrations runs all pending database migrations.
func runMigrations(m *migrate.Migrate) error {
	start := time.Now()
	err := m.Up()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	version, dirty, _ := m.Version()
	slog.Info("migrations completed",
		"duration", time.Since(start),
		"version", version,
		"dirty", dirty)

	return nil
}

type migrationSourceThrough struct {
	migratesource.Driver
	target uint
}

func (s *migrationSourceThrough) Next(version uint) (uint, error) {
	next, err := s.Driver.Next(version)
	if err != nil {
		return 0, fmt.Errorf("find migration after version %d: %w", version, err)
	}
	if next > s.target {
		return 0, os.ErrNotExist
	}
	return next, nil
}

func (s *migrationSourceThrough) ReadUp(version uint) (io.ReadCloser, string, error) {
	if version > s.target {
		return nil, "", os.ErrNotExist
	}
	r, identifier, err := s.Driver.ReadUp(version)
	if err != nil {
		return nil, "", fmt.Errorf("read up migration %d: %w", version, err)
	}
	return r, identifier, nil
}

func newMigrator(conn *sql.DB, config *Config) (*migrate.Migrate, migratedatabase.Driver, error) {
	fs, err := iofs.New(migrations.Migrations, ".")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create migration source: %w", err)
	}

	driver, err := postgres.WithInstance(conn, &postgres.Config{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create migration driver: %w", err)
	}

	m, err := migrate.NewWithInstance("migrations", fs, config.Name, driver)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create migrate instance: %w", err)
	}
	return m, driver, nil
}

func newMigratorThrough(
	databaseName string,
	driver migratedatabase.Driver,
	target uint,
) (*migrate.Migrate, error) {
	source, err := iofs.New(migrations.Migrations, ".")
	if err != nil {
		return nil, fmt.Errorf("failed to create bounded migration source: %w", err)
	}

	bounded := &migrationSourceThrough{Driver: source, target: target}
	m, err := migrate.NewWithInstance("bounded-migrations", bounded, databaseName, driver)
	if err != nil {
		return nil, fmt.Errorf("failed to create bounded migrate instance: %w", err)
	}
	return m, nil
}
