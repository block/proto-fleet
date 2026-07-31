package ha

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/block/proto-fleet/server/generated/sqlc"
	infradb "github.com/block/proto-fleet/server/internal/infrastructure/db"
)

// TestProductionHAProfile is an opt-in three-host qualification check. It uses
// the same Observer and SQL identity query as Fleet; the deployment runbook
// executes it once from each database host after the profile is healthy.
func TestProductionHAProfile(t *testing.T) {
	if os.Getenv("PROTO_FLEET_HA_PROFILE_TEST") != "1" {
		t.Skip("set PROTO_FLEET_HA_PROFILE_TEST=1 on a database host")
	}

	dsn := requiredEnv(t, "HA_PROFILE_DB_DSN")
	expectedWriters, err := haProfileDatabaseEndpoints(dsn)
	require.NoError(t, err)
	checkoutSHA := checkoutSourceIdentity(t)
	deploymentSHA, err := deploymentCommit(
		requiredEnv(t, "HA_PROFILE_DEPLOYMENT_VERSION_FILE"),
	)
	require.NoError(t, err)
	serviceCA := requiredEnv(t, "HA_PROFILE_SERVICE_CA")
	etcdPassword := readSecret(t, requiredEnv(t, "HA_PROFILE_ETCD_PASSWORD_FILE"))
	endpoints := strings.Split(requiredEnv(t, "HA_PROFILE_ETCD_ENDPOINTS"), ",")

	serverTLS := loadServerTLS(t, serviceCA)
	etcd, err := NewEtcdClient(clientv3.Config{
		Endpoints:   endpoints,
		Username:    "fleet-observer",
		Password:    etcdPassword,
		TLS:         serverTLS.Clone(),
		DialTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, etcd.Close()) })

	dbConfig := &infradb.Config{
		ExplicitDSN:              dsn,
		Name:                     "fleet",
		InitialConnectionTimeout: 5 * time.Second,
		MaxOpenConns:             5,
		MaxIdleConns:             2,
		ConnMaxLifetime:          time.Minute,
	}
	ctx := t.Context()
	var db *sql.DB
	if os.Getenv("HA_PROFILE_MIGRATE") == "1" {
		db, err = infradb.ConnectAndMigrate(dbConfig)
	} else {
		db, err = infradb.ConnectToDatabase(dbConfig)
	}
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	connection, err := db.Conn(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })

	queries, err := sqlc.Prepare(ctx, connection)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, queries.Close()) })

	databaseIdentity, err := queries.GetHAProfileDatabaseIdentity(ctx)
	require.NoError(t, err)
	require.Equal(t, "fleet", databaseIdentity.DatabaseUser)
	require.Equal(t, "fleet", databaseIdentity.DatabaseName)
	require.False(t, databaseIdentity.IsSuperuser)
	imageSHA := strings.TrimSpace(databaseIdentity.SourceCommit)
	require.NoError(t, validateHAProfileSourceIdentity(
		checkoutSHA,
		deploymentSHA,
		imageSHA,
	))

	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	require.True(t, ok)
	patroniTransport := defaultTransport.Clone()
	patroniTransport.TLSClientConfig = serverTLS.Clone()
	patroni := NewPatroniHTTPClient(&http.Client{
		Transport: patroniTransport,
		Timeout:   5 * time.Second,
	})

	observer, err := NewObserver(
		"/service/proto-fleet",
		etcd,
		queries,
		patroni,
	)
	require.NoError(t, err)
	observation, err := observer.Observe(ctx)
	require.NoError(t, err)

	require.True(t, slices.Contains(expectedWriters, observation.ServerAddress))
	require.Equal(t, int32(5432), observation.ServerPort)

	evidence, err := json.Marshal(map[string]any{
		"dcs_cluster_id":    observation.DCSClusterID,
		"deployment_sha":    deploymentSHA,
		"fleet_host":        requiredEnv(t, "HA_PROFILE_FLEET_HOST"),
		"image_sha":         imageSHA,
		"leader":            observation.LeaderName,
		"server_address":    observation.ServerAddress,
		"server_port":       observation.ServerPort,
		"standby_ready":     true,
		"timeline":          observation.Timeline,
		"writer_generation": observation.WriterGeneration,
	})
	require.NoError(t, err)
	t.Logf("HA_PROFILE_EVIDENCE %s", evidence)
}

func TestHAProfileDatabaseEndpoints(t *testing.T) {
	const validDSN = "postgresql://fleet:secret@10.40.0.11:5432,10.40.0.12:5432/fleet" +
		"?target_session_attrs=read-write&sslmode=verify-full" +
		"&sslrootcert=/ca.crt"

	endpoints, err := haProfileDatabaseEndpoints(validDSN)
	require.NoError(t, err)
	require.Equal(t, []string{"10.40.0.11", "10.40.0.12"}, endpoints)

	tests := map[string]string{
		"wrong role": "postgresql://postgres:secret@10.40.0.11:5432,10.40.0.12:5432/fleet" +
			"?target_session_attrs=read-write&sslmode=verify-full" +
			"&sslrootcert=/ca.crt",
		"wrong database": "postgresql://fleet:secret@10.40.0.11:5432,10.40.0.12:5432/postgres" +
			"?target_session_attrs=read-write&sslmode=verify-full" +
			"&sslrootcert=/ca.crt",
		"one host": "postgresql://fleet:secret@10.40.0.11:5432/fleet" +
			"?target_session_attrs=read-write",
		"duplicate host": "postgresql://fleet:secret@10.40.0.11:5432,10.40.0.11:5432/fleet" +
			"?target_session_attrs=read-write",
		"hostname": "postgresql://fleet:secret@db-a:5432,10.40.0.12:5432/fleet" +
			"?target_session_attrs=read-write",
		"wrong target": "postgresql://fleet:secret@10.40.0.11:5432,10.40.0.12:5432/fleet" +
			"?target_session_attrs=any",
		"weak TLS": "postgresql://fleet:secret@10.40.0.11:5432,10.40.0.12:5432/fleet" +
			"?target_session_attrs=read-write&sslmode=require" +
			"&sslrootcert=/ca.crt",
		"missing CA": "postgresql://fleet:secret@10.40.0.11:5432,10.40.0.12:5432/fleet" +
			"?target_session_attrs=read-write&sslmode=verify-full",
	}
	for name, dsn := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := haProfileDatabaseEndpoints(dsn)
			require.Error(t, err)
		})
	}
}

func TestValidateHAProfileSourceIdentity(t *testing.T) {
	require.NoError(t, validateHAProfileSourceIdentity("abc", "abc", "abc"))
	require.ErrorContains(
		t,
		validateHAProfileSourceIdentity("abc", "older-bundle", "abc"),
		"deployment bundle",
	)
	require.ErrorContains(
		t,
		validateHAProfileSourceIdentity("abc", "abc", "older-image"),
		"database image",
	)
}

func haProfileDatabaseEndpoints(dsn string) ([]string, error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse HA profile database DSN: %w", err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return nil, fmt.Errorf("HA profile database DSN must use postgresql")
	}
	if parsed.User == nil || parsed.User.Username() != "fleet" {
		return nil, fmt.Errorf("HA profile database DSN must use the fleet role")
	}
	if parsed.Path != "/fleet" {
		return nil, fmt.Errorf("HA profile database DSN must use the fleet database")
	}

	query := parsed.Query()
	requiredValues := map[string]string{
		"sslmode":              "verify-full",
		"target_session_attrs": "read-write",
	}
	for key, expected := range requiredValues {
		values := query[key]
		if len(values) != 1 || values[0] != expected {
			return nil, fmt.Errorf(
				"HA profile database DSN requires %s=%s",
				key,
				expected,
			)
		}
	}
	for _, key := range []string{"sslrootcert"} {
		values := query[key]
		if len(values) != 1 || strings.TrimSpace(values[0]) == "" {
			return nil, fmt.Errorf("HA profile database DSN requires %s", key)
		}
	}

	hosts := strings.Split(parsed.Host, ",")
	if len(hosts) != 2 {
		return nil, fmt.Errorf("HA profile database DSN requires exactly two hosts")
	}

	endpoints := make([]string, 0, len(hosts))
	unique := make(map[string]struct{}, len(hosts))
	for _, endpoint := range hosts {
		host, port, err := net.SplitHostPort(endpoint)
		if err != nil || port != "5432" {
			return nil, fmt.Errorf(
				"HA profile database endpoint %q must use port 5432",
				endpoint,
			)
		}
		ip := net.ParseIP(host)
		if ip == nil || ip.To4() == nil {
			return nil, fmt.Errorf(
				"HA profile database endpoint %q must use a literal IPv4 address",
				endpoint,
			)
		}
		address := ip.String()
		if _, exists := unique[address]; exists {
			return nil, fmt.Errorf("HA profile database endpoints must be unique")
		}
		unique[address] = struct{}{}
		endpoints = append(endpoints, address)
	}
	return endpoints, nil
}

func checkoutSourceIdentity(t *testing.T) string {
	t.Helper()
	require.Empty(
		t,
		gitOutput(t, "status", "--porcelain"),
		"qualification checkout must be clean",
	)
	return gitOutput(t, "rev-parse", "HEAD")
}

func gitOutput(t *testing.T, args ...string) string {
	t.Helper()
	output, err := exec.Command("git", args...).CombinedOutput()
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), output)
	return strings.TrimSpace(string(output))
}

func deploymentCommit(versionFile string) (string, error) {
	contents, err := os.ReadFile(versionFile)
	if err != nil {
		return "", fmt.Errorf("read deployment version: %w", err)
	}
	for line := range strings.SplitSeq(string(contents), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(key) == "commit" {
			commit := strings.TrimSpace(value)
			if commit == "" {
				break
			}
			return commit, nil
		}
	}
	return "", fmt.Errorf("deployment version file does not contain a commit")
}

func validateHAProfileSourceIdentity(
	checkoutSHA string,
	deploymentSHA string,
	imageSHA string,
) error {
	if deploymentSHA != checkoutSHA {
		return fmt.Errorf(
			"deployment bundle source %q does not match checkout %q",
			deploymentSHA,
			checkoutSHA,
		)
	}
	if imageSHA != checkoutSHA {
		return fmt.Errorf(
			"database image source %q does not match checkout %q",
			imageSHA,
			checkoutSHA,
		)
	}
	return nil
}

func requiredEnv(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	require.NotEmpty(t, value, "%s is required", name)
	return value
}

func readSecret(t *testing.T, path string) string {
	t.Helper()
	value, err := os.ReadFile(path)
	require.NoError(t, err)
	return strings.TrimSpace(string(value))
}

func loadServerTLS(t *testing.T, caPath string) *tls.Config {
	t.Helper()
	caPEM, err := os.ReadFile(caPath)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(caPEM))
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    roots,
	}
}
