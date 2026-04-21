package db

import (
	"net/url"
	"time"
)

type Config struct {
	Name                     string        `help:"Name of the database" default:"fleet" env:"NAME"`
	Username                 string        `help:"Username to database" default:"fleet" env:"USERNAME"`
	Password                 string        `help:"Password to database" env:"PASSWORD"`
	Address                  string        `help:"Address of the database, including port" default:"127.0.0.1:5432" env:"ADDRESS"`
	SSLMode                  string        `help:"PostgreSQL SSL mode" default:"disable" env:"SSL_MODE"`
	InitialConnectionTimeout time.Duration `help:"Timeout for initial connection" default:"2s" env:"INITIAL_CONNECTION_TIMEOUT"`

	// Connection pool settings
	MaxOpenConns    int           `help:"Maximum number of open database connections" default:"250" env:"MAX_OPEN_CONNS"`
	MaxIdleConns    int           `help:"Maximum number of idle database connections" default:"50" env:"MAX_IDLE_CONNS"`
	ConnMaxLifetime time.Duration `help:"Maximum lifetime of a database connection" default:"5m" env:"CONN_MAX_LIFETIME"`
}

// DSN returns the PostgreSQL connection string.
func (c *Config) DSN() string {
	encodedPassword := url.QueryEscape(c.Password)
	return "postgres://" + c.Username + ":" + encodedPassword + "@" + c.Address + "/" + c.Name + "?sslmode=" + c.SSLMode
}
