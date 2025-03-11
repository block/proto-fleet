package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Config for the MySQL database connection
type Config struct {
	Name     string `help:"Name of the database" default:"fleet" env:"DB_NAME"`
	Username string `help:"Username to database" default:"root" env:"DB_USERNAME"`
	Password string `help:"Password to database" env:"DB_PASSWORD"`
	Addr     string `help:"Address of the database, including port" default:"127.0.0.1:3306" env:"DB_ADDR"`
}

// Connect creates a driver for the database and ensures the database is alive.
func (c *Config) Connect() (*sql.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true", c.Username, c.Password, c.Addr, c.Name)
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("error creating mysql connection: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	return conn, fmt.Errorf("error pinging db: %w", conn.PingContext(ctx))
}
