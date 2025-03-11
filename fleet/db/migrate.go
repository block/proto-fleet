package db

import (
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql" // mysql driver for db/sql
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/btc-mining/miner-firmware/fleet/files"
)

// Migrate the database to the latest version
func Migrate(conn *sql.DB) error {
	fs, err := iofs.New(files.Migrations, "migrations")
	if err != nil {
		return fmt.Errorf("error opening migrations fs: %w", err)
	}
	driver, err := mysql.WithInstance(conn, &mysql.Config{})
	if err != nil {
		return fmt.Errorf("error creating mysql driver: %w", err)
	}
	m, err := migrate.NewWithInstance("migrations", fs, "fleet", driver)
	if err != nil {
		return fmt.Errorf("error creating migrator: %w", err)
	}
	return fmt.Errorf("error running migrations: %w", m.Up())
}
