package testutils

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

type Config struct {
	URL          string
	Organization string
	Bucket       string
	Token        string
	WriteTimeout time.Duration
	QueryTimeout time.Duration
}

func SetupInfluxDBContainer(t *testing.T) (testcontainers.Container, Config) {
	ctx := t.Context()

	req := testcontainers.ContainerRequest{
		Image:        "influxdb:3.2-core",
		ExposedPorts: []string{"8181/tcp"},
		Env: map[string]string{
			"INFLUXDB_HTTP_PORT":               "8181",
			"INFLUXDB_ORG":                     "testorg",
			"INFLUXDB3_NODE_IDENTIFIER_PREFIX": "testnode",
			"INFLUXDB3_DISABLE_AUTHZ":          "health", // Only disable auth for health endpoint
			"INFLUXDB3_OBJECT_STORE":           "file",
			"INFLUXDB3_DB_DIR":                 "/var/lib/influxdb3",
		},
		Cmd: []string{"influxdb3", "serve"},
		WaitingFor: wait.ForHTTP("/health").
			WithPort("8181/tcp").
			WithStartupTimeout(60 * time.Second).
			WithPollInterval(50 * time.Millisecond),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	mappedPort, err := container.MappedPort(ctx, "8181")
	require.NoError(t, err)

	host, err := container.Host(ctx)
	require.NoError(t, err)

	baseURL := fmt.Sprintf("http://%s:%s", host, mappedPort.Port())
	t.Logf("InfluxDB v3-core started at %s", baseURL)

	token, err := createInfluxDBToken(ctx, container)
	require.NoError(t, err, "Should be able to create InfluxDB token")

	err = createInfluxDBDatabaseWithToken(ctx, container, "testbucket", token)
	require.NoError(t, err, "Should be able to create InfluxDB database")

	config := Config{
		URL:          baseURL,
		Organization: "testorg",
		Bucket:       "testbucket",
		Token:        token,
		WriteTimeout: 30 * time.Second,
		QueryTimeout: 60 * time.Second,
	}

	return container, config
}

func createInfluxDBToken(ctx context.Context, container testcontainers.Container) (string, error) {
	exitCode, reader, err := container.Exec(ctx, []string{"influxdb3", "create", "token", "--admin"})
	if err != nil {
		return "", fmt.Errorf("failed to execute token creation command: %w", err)
	}

	output, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read token creation output: %w", err)
	}

	outputStr := string(output)

	if exitCode != 0 {
		// If token creation failed due to conflict, database might not require auth in test mode
		// Try creating the database without a token
		if strings.Contains(outputStr, "409") || strings.Contains(outputStr, "already exists") {
			return "", nil // Empty token means no auth required
		}
		return "", fmt.Errorf("token creation command failed with exit code %d: %s", exitCode, outputStr)
	}

	// Extract token from output like "Token: apiv3<token_value>"
	lines := strings.Split(outputStr, "\n")
	for _, line := range lines {
		if strings.Contains(line, "Token:") || strings.Contains(line, "token:") {
			if idx := strings.Index(line, "apiv3"); idx != -1 {
				token := "apiv3" + strings.TrimSpace(line[idx+5:])
				return token, nil
			}
		}
	}

	return "", fmt.Errorf("failed to extract token from output: %s", outputStr)
}

func createInfluxDBDatabase(ctx context.Context, container testcontainers.Container, dbName string) error {
	exitCode, reader, err := container.Exec(ctx, []string{"influxdb3", "create", "database", dbName})
	if err != nil {
		return fmt.Errorf("failed to execute database creation command: %w", err)
	}

	output, _ := io.ReadAll(reader)
	outputStr := string(output)

	if exitCode != 0 {
		if strings.Contains(outputStr, "already exists") || strings.Contains(outputStr, "409") {
			return nil // Database already exists, that's fine
		}
		return fmt.Errorf("database creation command failed with exit code %d: %s", exitCode, outputStr)
	}

	return nil
}

func createInfluxDBDatabaseWithToken(ctx context.Context, container testcontainers.Container, dbName, token string) error {
	var cmd []string
	if token == "" {
		// No token means no auth required (test mode fallback)
		cmd = []string{"influxdb3", "create", "database", dbName}
	} else {
		cmd = []string{"influxdb3", "create", "database", dbName, "--token", token}
	}

	exitCode, reader, err := container.Exec(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to execute database creation command: %w", err)
	}

	output, _ := io.ReadAll(reader)
	outputStr := string(output)

	if exitCode != 0 {
		if strings.Contains(outputStr, "already exists") || strings.Contains(outputStr, "409") {
			return nil // Database already exists, that's fine
		}
		return fmt.Errorf("database creation command failed with exit code %d: %s", exitCode, outputStr)
	}

	return nil
}
