package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"

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
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true&allowNativePasswords=true", config.Username, config.Password, config.Address, config.Name)

	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error creating mysql connection: %v", err)
	}

	return conn, nil
}

func MigrateDatabase(connection *sql.DB, config *Config) error {
	slog.Info("Migrating database", slog.String("addr", config.Address), slog.String("db", config.Name))

	fs, err := iofs.New(migrations.Migrations, ".")
	if err != nil {
		return fleeterror.NewInternalErrorf("error opening migrations fs: %v", err)
	}

	driver, err := mysql.WithInstance(connection, &mysql.Config{})
	if err != nil {
		return fleeterror.NewInternalErrorf("error creating mysql driver: %v", err)
	}

	m, err := migrate.NewWithInstance("migrations", fs, "fleet", driver)
	if err != nil {
		return fleeterror.NewInternalErrorf("error creating migrator: %v", err)
	}

	err = m.Up()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fleeterror.NewInternalErrorf("error running migrations: %v", err)
	}

	return nil
}

func verifyDatabaseConnectionEstablished(connection *sql.DB, config *Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), config.InitialConnectionTimeout)
	defer cancel()

	err := connection.PingContext(ctx)
	if err != nil {
		return fleeterror.NewInternalErrorf("error pinging db: %v", err)
	}

	return nil
}
