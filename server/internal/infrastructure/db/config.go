package db

import (
	"time"
)

type Config struct {
	Name                     string        `help:"Name of the database" default:"fleet" env:"NAME"`
	Username                 string        `help:"Username to database" default:"root" env:"USERNAME"`
	Password                 string        `help:"Password to database" env:"PASSWORD"`
	Address                  string        `help:"Address of the database, including port" default:"127.0.0.1:3306" env:"ADDRESS"`
	InitialConnectionTimeout time.Duration `help:"Timeout for initial connection" default:"2s" env:"INITIAL_CONNECTION_TIMEOUT"`

	// Connection pool settings
	MaxOpenConns    int           `help:"Maximum number of open database connections" default:"100" env:"MAX_OPEN_CONNS"`
	MaxIdleConns    int           `help:"Maximum number of idle database connections" default:"25" env:"MAX_IDLE_CONNS"`
	ConnMaxLifetime time.Duration `help:"Maximum lifetime of a database connection" default:"5m" env:"CONN_MAX_LIFETIME"`
}
