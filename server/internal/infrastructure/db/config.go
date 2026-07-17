package db

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type Config struct {
	ExplicitDSN              string        `help:"Full PostgreSQL DSN. Overrides DB address/name/user/password/ssl-mode fields when set." env:"DSN"`
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
	if c.UsesExplicitDSN() {
		return strings.TrimSpace(c.ExplicitDSN)
	}
	dsn := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.Username, c.Password),
		Host:   c.Address,
		Path:   "/" + c.Name,
	}
	values := dsn.Query()
	values.Set("sslmode", c.SSLMode)
	dsn.RawQuery = values.Encode()
	return dsn.String()
}

func (c *Config) UsesExplicitDSN() bool {
	return strings.TrimSpace(c.ExplicitDSN) != ""
}

func (c *Config) Validate() error {
	dsn := c.DSN()
	if strings.TrimSpace(dsn) == "" {
		return fmt.Errorf("database DSN is empty")
	}
	parsedConfig, err := pgconn.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("invalid database DSN")
	}
	if parsedConfigHasHostaddr(parsedConfig) {
		return fmt.Errorf("database DSN hostaddr is not supported; use host")
	}
	if parsedConfigLooksMultiHost(parsedConfig) && !parsedConfigHasReadWriteTarget(parsedConfig) {
		return fmt.Errorf("multi-host database DSN requires target_session_attrs=read-write")
	}
	return nil
}

func (c *Config) ConnectionTarget() string {
	if c.UsesExplicitDSN() {
		return "DB_DSN"
	}
	return c.Address
}

func parsedConfigLooksMultiHost(config *pgconn.Config) bool {
	if config == nil {
		return false
	}
	endpoints := map[string]struct{}{
		fmt.Sprintf("%s:%d", config.Host, config.Port): {},
	}
	for _, fallback := range config.Fallbacks {
		endpoints[fmt.Sprintf("%s:%d", fallback.Host, fallback.Port)] = struct{}{}
	}
	return len(endpoints) > 1
}

func parsedConfigHasReadWriteTarget(config *pgconn.Config) bool {
	if config == nil || config.ValidateConnect == nil {
		return false
	}
	return reflect.ValueOf(config.ValidateConnect).Pointer() ==
		reflect.ValueOf(pgconn.ValidateConnectTargetSessionAttrsReadWrite).Pointer()
}

func parsedConfigHasHostaddr(config *pgconn.Config) bool {
	if config == nil {
		return false
	}
	_, ok := config.RuntimeParams["hostaddr"]
	return ok
}
