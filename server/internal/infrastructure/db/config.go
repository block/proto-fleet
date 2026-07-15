package db

import (
	"fmt"
	"net/url"
	"regexp"
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
	encodedPassword := url.QueryEscape(c.Password)
	return "postgres://" + c.Username + ":" + encodedPassword + "@" + c.Address + "/" + c.Name + "?sslmode=" + c.SSLMode
}

func (c *Config) UsesExplicitDSN() bool {
	return strings.TrimSpace(c.ExplicitDSN) != ""
}

func (c *Config) Validate() error {
	dsn := c.DSN()
	if strings.TrimSpace(dsn) == "" {
		return fmt.Errorf("database DSN is empty")
	}
	if _, err := pgconn.ParseConfig(dsn); err != nil {
		return fmt.Errorf("invalid database DSN %q: %s", RedactDSN(dsn), RedactDSN(err.Error()))
	}
	if DSNLooksMultiHost(dsn) && !DSNHasReadWriteTarget(dsn) {
		return fmt.Errorf("multi-host database DSN requires target_session_attrs=read-write")
	}
	return nil
}

func (c *Config) RedactedDSN() string {
	return RedactDSN(c.DSN())
}

func (c *Config) ConnectionTarget() string {
	if c.UsesExplicitDSN() {
		return c.RedactedDSN()
	}
	return c.Address
}

func RedactDSN(dsn string) string {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == "" {
		return trimmed
	}

	if u, err := url.Parse(trimmed); err == nil && isPostgresURL(u) {
		redacted := false
		if u.User != nil {
			if _, hasPassword := u.User.Password(); hasPassword {
				u.User = url.UserPassword(u.User.Username(), "xxxxx")
				redacted = true
			}
		}
		values := u.Query()
		for key := range values {
			if isSensitiveDSNQueryParam(key) {
				values.Set(key, "xxxxx")
				redacted = true
			}
		}
		if redacted {
			u.RawQuery = values.Encode()
			return u.String()
		}
	}

	userInfoPattern := regexp.MustCompile(`(?i)(postgres(?:ql)?://[^:/@\s]+):[^@\s]*@`)
	trimmed = userInfoPattern.ReplaceAllString(trimmed, "${1}:xxxxx@")

	queryPasswordPattern := regexp.MustCompile("(?i)([?&](?:password|sslpassword)=)[^&\\s`'\"]*")
	trimmed = queryPasswordPattern.ReplaceAllString(trimmed, "${1}xxxxx")

	passwordPattern := regexp.MustCompile(`(?i)(^|\s)password=(?:'[^']*'|"[^"]*"|\S+)`)
	return passwordPattern.ReplaceAllString(trimmed, "${1}password=xxxxx")
}

func DSNLooksMultiHost(dsn string) bool {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == "" {
		return false
	}
	if u, err := url.Parse(trimmed); err == nil && isPostgresURL(u) {
		return strings.Contains(u.Host, ",")
	}
	host, ok := keywordDSNValue(trimmed, "host")
	return ok && strings.Contains(host, ",")
}

func DSNHasReadWriteTarget(dsn string) bool {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == "" {
		return false
	}
	if u, err := url.Parse(trimmed); err == nil && isPostgresURL(u) {
		return strings.EqualFold(u.Query().Get("target_session_attrs"), "read-write")
	}
	value, ok := keywordDSNValue(trimmed, "target_session_attrs")
	return ok && strings.EqualFold(value, "read-write")
}

func isPostgresURL(u *url.URL) bool {
	return u.Scheme == "postgres" || u.Scheme == "postgresql"
}

func isSensitiveDSNQueryParam(key string) bool {
	switch strings.ToLower(key) {
	case "password", "sslpassword":
		return true
	default:
		return false
	}
}

func keywordDSNValue(dsn string, key string) (string, bool) {
	pattern := regexp.MustCompile(`(?i)(?:^|\s)` + regexp.QuoteMeta(key) + `=('[^']*'|"[^"]*"|\S+)`)
	matches := pattern.FindStringSubmatch(dsn)
	if len(matches) != 2 {
		return "", false
	}
	value := matches[1]
	if len(value) >= 2 {
		first := value[0]
		last := value[len(value)-1]
		if first == last && (first == '\'' || first == '"') {
			value = value[1 : len(value)-1]
		}
	}
	return value, true
}
