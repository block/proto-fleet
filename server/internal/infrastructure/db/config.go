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

var sensitiveDSNKeys = []string{"password", "sslpassword"}

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
			if isSensitiveDSNKey(key) {
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

	return redactSensitiveKeywordDSNValues(trimmed)
}

func DSNLooksMultiHost(dsn string) bool {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == "" {
		return false
	}
	if u, err := url.Parse(trimmed); err == nil && isPostgresURL(u) {
		return strings.Contains(u.Host, ",") || urlQueryHasMultiHost(u.Query(), "host")
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

func isSensitiveDSNKey(key string) bool {
	for _, sensitiveKey := range sensitiveDSNKeys {
		if strings.EqualFold(key, sensitiveKey) {
			return true
		}
	}
	return false
}

func urlQueryHasMultiHost(values url.Values, key string) bool {
	hosts := values[key]
	if len(hosts) > 1 {
		return true
	}
	for _, host := range hosts {
		if strings.Contains(host, ",") {
			return true
		}
	}
	return false
}

func redactSensitiveKeywordDSNValues(dsn string) string {
	redacted := dsn
	for _, key := range sensitiveDSNKeys {
		redacted = redactKeywordDSNValue(redacted, key)
	}
	return redacted
}

func redactKeywordDSNValue(dsn string, key string) string {
	var redacted strings.Builder
	last := 0
	scanKeywordDSNAssignments(dsn, func(assignment keywordDSNAssignment) bool {
		if !strings.EqualFold(assignment.key, key) {
			return true
		}
		redacted.WriteString(dsn[last:assignment.valueStart])
		redacted.WriteString("xxxxx")
		last = assignment.valueEnd
		return true
	})
	if last == 0 {
		return dsn
	}
	redacted.WriteString(dsn[last:])
	return redacted.String()
}

type keywordDSNAssignment struct {
	key        string
	valueStart int
	valueEnd   int
}

func scanKeywordDSNAssignments(dsn string, visit func(keywordDSNAssignment) bool) {
	for i := 0; i < len(dsn); {
		i = skipDSNSpace(dsn, i)
		keyStart := i
		for i < len(dsn) && !isDSNSpace(dsn[i]) && dsn[i] != '=' {
			i++
		}
		if keyStart == i {
			i++
			continue
		}

		keyEnd := i
		i = skipDSNSpace(dsn, i)
		if i >= len(dsn) || dsn[i] != '=' {
			continue
		}
		i++
		i = skipDSNSpace(dsn, i)

		valueStart := i
		valueEnd := keywordDSNValueEnd(dsn, valueStart)
		if !visit(keywordDSNAssignment{
			key:        dsn[keyStart:keyEnd],
			valueStart: valueStart,
			valueEnd:   valueEnd,
		}) {
			return
		}
		i = valueEnd
	}
}

func skipDSNSpace(dsn string, offset int) int {
	for offset < len(dsn) && isDSNSpace(dsn[offset]) {
		offset++
	}
	return offset
}

func keywordDSNValueEnd(dsn string, start int) int {
	if start >= len(dsn) {
		return start
	}
	if dsn[start] == '\'' || dsn[start] == '"' {
		quote := dsn[start]
		for i := start + 1; i < len(dsn); i++ {
			if dsn[i] == '\\' && i+1 < len(dsn) {
				i++
				continue
			}
			if dsn[i] == quote {
				return i + 1
			}
		}
		return len(dsn)
	}
	for i := start; i < len(dsn); i++ {
		if dsn[i] == '\\' && i+1 < len(dsn) {
			i++
			continue
		}
		if isDSNSpace(dsn[i]) {
			return i
		}
	}
	return len(dsn)
}

func isDSNSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}

func keywordDSNValue(dsn string, key string) (string, bool) {
	var value string
	found := false
	scanKeywordDSNAssignments(dsn, func(assignment keywordDSNAssignment) bool {
		if !strings.EqualFold(assignment.key, key) {
			return true
		}
		value = keywordDSNValueText(dsn[assignment.valueStart:assignment.valueEnd])
		found = true
		return false
	})
	return value, found
}

func keywordDSNValueText(value string) string {
	if len(value) >= 2 {
		first := value[0]
		last := value[len(value)-1]
		if first == last && (first == '\'' || first == '"') {
			value = value[1 : len(value)-1]
		}
	}
	return unescapeKeywordDSNValue(value)
}

func unescapeKeywordDSNValue(value string) string {
	if !strings.Contains(value, `\`) {
		return value
	}
	var out strings.Builder
	out.Grow(len(value))
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' && i+1 < len(value) {
			i++
		}
		out.WriteByte(value[i])
	}
	return out.String()
}
