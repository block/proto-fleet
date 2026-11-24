package deployment

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"

	"github.com/block/proto-fleet/server/internal/ha"
)

// Both API pools can overlap during handover. Keep 200 slots for other clients.
const haDatabaseConnectionCapacity = 1200

func checkDatabaseConnectionCapacity(ctx context.Context, envPath string) error {
	config, err := loadNodeConfig(envPath)
	if err != nil {
		return err
	}
	tlsConfig, err := ha.LoadServiceTLS(filepath.Join(config.SecretsDir, "service-ca.crt"))
	if err != nil {
		return err
	}
	password, err := readPassword(filepath.Join(config.SecretsDir, "patroni-rest-password"))
	if err != nil {
		return err
	}
	client, cleanup := newProbeHTTPClient(tlsConfig, nil)
	defer cleanup()
	for _, address := range []string{config.DatabaseAIP, config.DatabaseBIP} {
		if err := checkPatroniConnectionCapacity(ctx, client, "https://"+address+":8008", password); err != nil {
			return fmt.Errorf("HA database connection capacity at %s: %w; complete the PostgreSQL worker and connection capacity procedure on both database hosts before updating Fleet", address, err)
		}
	}
	return nil
}

func checkPatroniConnectionCapacity(ctx context.Context, client *http.Client, baseURL, password string) error {
	var config struct {
		PostgreSQL struct {
			Parameters struct {
				MaxConnections int `json:"max_connections"`
			} `json:"parameters"`
		} `json:"postgresql"`
	}
	if err := readPatroniCapacity(ctx, client, baseURL+"/config", password, &config); err != nil {
		return err
	}
	if limit := config.PostgreSQL.Parameters.MaxConnections; limit < haDatabaseConnectionCapacity {
		return fmt.Errorf("max_connections is %d; at least %d required", limit, haDatabaseConnectionCapacity)
	}
	var status struct {
		State          string `json:"state"`
		PendingRestart bool   `json:"pending_restart"`
	}
	if err := readPatroniCapacity(ctx, client, baseURL+"/patroni", password, &status); err != nil {
		return err
	}
	if status.State != "running" || status.PendingRestart {
		return fmt.Errorf("database state is %q, pending_restart=%t; running databases with no pending restart required", status.State, status.PendingRestart)
	}
	return nil
}

func readPatroniCapacity(ctx context.Context, client *http.Client, endpoint, password string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create Patroni capacity request: %w", err)
	}
	request.SetBasicAuth("patroni-admin", password)
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("read Patroni capacity: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("read Patroni capacity: HTTP %d", response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(target); err != nil {
		return fmt.Errorf("decode Patroni capacity: %w", err)
	}
	return nil
}
