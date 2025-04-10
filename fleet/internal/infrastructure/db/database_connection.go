package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/btc-mining/miner-firmware/fleet/internal/db/migrations"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

type DBConfig struct {
	Name                     string        `help:"Name of the database" default:"fleet" env:"NAME"`
	Username                 string        `help:"Username to database" default:"root" env:"USERNAME"`
	Password                 string        `help:"Password to database" env:"PASSWORD"`
	Address                  string        `help:"Address of the database, including port" default:"127.0.0.1:3306" env:"ADDRESS"`
	InitialConnectionTimeout time.Duration `help:"Timeout for initial connection" default:"2s" env:"INITIAL_CONNECTION_TIMEOUT"`
}

// ConnectAndMigrate creates a driver for the database, ensures the database is alive, and runs migrations if needed.
func ConnectAndMigrate(config *DBConfig) (*sql.DB, error) {
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

func ConnectToDatabase(config *DBConfig) (*sql.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true", config.Username, config.Password, config.Address, config.Name)

	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("error creating mysql connection: %w", err)
	}

	return conn, nil
}

func verifyDatabaseConnectionEstablished(connection *sql.DB, config *DBConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), config.InitialConnectionTimeout)
	defer cancel()

	err := connection.PingContext(ctx)
	if err != nil {
		return fmt.Errorf("error pinging db: %w", err)
	}

	return nil
}

func MigrateDatabase(connection *sql.DB, config *DBConfig) error {
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
