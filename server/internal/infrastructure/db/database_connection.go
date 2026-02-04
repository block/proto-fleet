package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver

	"github.com/btc-mining/proto-fleet/server/migrations"
)

// ConnectToDatabase establishes a connection to the database using the provided config.
// Returns a sql.DB connection with configured connection pooling settings.
func ConnectToDatabase(config *Config) (*sql.DB, error) {
	conn, err := sql.Open("pgx", config.DSN())
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	conn.SetMaxOpenConns(config.MaxOpenConns)
	conn.SetMaxIdleConns(config.MaxIdleConns)
	conn.SetConnMaxLifetime(config.ConnMaxLifetime)

	return conn, nil
}

// verifyConnectionEstablished waits for the database connection to be established.
func verifyConnectionEstablished(conn *sql.DB, config *Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), config.InitialConnectionTimeout)
	defer cancel()

	if err := conn.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	return nil
}

// ConnectAndMigrate connects to the database and runs all pending migrations.
// Returns the database connection ready for use.
func ConnectAndMigrate(config *Config) (*sql.DB, error) {
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

	err = verifyConnectionEstablished(connection, config)
	if err != nil {
		return nil, fmt.Errorf("failed to verify database connection: %w", err)
	}

	slog.Info("connected to database", "address", config.Address, "database", config.Name)

	err = runMigrations(connection, config)
	if err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	success = true
	return connection, nil
}

// runMigrations runs all pending database migrations.
func runMigrations(conn *sql.DB, config *Config) error {
	fs, err := iofs.New(migrations.Migrations, ".")
	if err != nil {
		return fmt.Errorf("failed to create migration source: %w", err)
	}

	driver, err := postgres.WithInstance(conn, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("failed to create migration driver: %w", err)
	}

	m, err := migrate.NewWithInstance("migrations", fs, config.Name, driver)
	if err != nil {
		return fmt.Errorf("failed to create migrate instance: %w", err)
	}

	start := time.Now()
	err = m.Up()
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
