package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/btc-mining/proto-fleet/server/migrations"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// ConnectAndMigrate creates a driver for the database, ensures the database is alive, and runs migrations if needed.
func ConnectAndMigrate(config *Config) (*sql.DB, error) {
	connection, err := ConnectToDatabase(config)
	if err != nil {
		return nil, err
	}

	err = verifyDatabaseConnectionEstablished(connection, config)
	if err != nil {
		return nil, err
	}

	err = MigrateDatabase(connection, config)
	if err != nil {
		return nil, err
	}

	return connection, nil
}

func ConnectToDatabase(config *Config) (*sql.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true", config.Username, config.Password, config.Address, config.Name)

	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("error creating mysql connection: %w", err)
	}

	return conn, nil
}

func MigrateDatabase(connection *sql.DB, config *Config) error {
	slog.Info("Migrating database", slog.String("addr", config.Address), slog.String("db", config.Name))

	fs, err := iofs.New(migrations.Migrations, ".")
	if err != nil {
		return fmt.Errorf("error opening migrations fs: %w", err)
	}

	driver, err := mysql.WithInstance(connection, &mysql.Config{})
	if err != nil {
		return fmt.Errorf("error creating mysql driver: %w", err)
	}

	m, err := migrate.NewWithInstance("migrations", fs, "fleet", driver)
	if err != nil {
		return fmt.Errorf("error creating migrator: %w", err)
	}

	err = m.Up()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("error running migrations: %w", err)
	}

	return nil
}

func verifyDatabaseConnectionEstablished(connection *sql.DB, config *Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), config.InitialConnectionTimeout)
	defer cancel()

	err := connection.PingContext(ctx)
	if err != nil {
		return fmt.Errorf("error pinging db: %w", err)
	}

	return nil
}
