package ha

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/runtimejobs"
)

// Config keeps HA opt-in so existing single-instance deployments remain
// standalone and do not need etcd or Patroni credentials.
type Config struct {
	Enabled          bool          `help:"Enable active/passive Fleet runtime ownership." default:"false" env:"ENABLED"`
	ClusterPath      string        `help:"Patroni DCS cluster path." default:"/service/proto-fleet" env:"CLUSTER_PATH"`
	EtcdEndpoints    []string      `help:"Comma-separated HTTPS etcd endpoints." env:"ETCD_ENDPOINTS" sep:","`
	EtcdUsername     string        `help:"Read-only etcd observer username." default:"fleet-observer" env:"ETCD_USERNAME"`
	EtcdPasswordFile string        `help:"Path to the etcd observer password file." default:"/etc/proto-fleet/ha/fleet-etcd-password" env:"ETCD_PASSWORD_FILE" type:"path"`
	ServiceCAFile    string        `help:"Path to the CA that signs etcd and Patroni service certificates." default:"/etc/proto-fleet/ha/service-ca.crt" env:"SERVICE_CA_FILE" type:"path"`
	LeaseDuration    time.Duration `help:"Fleet active lease duration." default:"10s" env:"LEASE_DURATION"`
	RenewInterval    time.Duration `help:"Fleet active lease renewal interval." default:"3s" env:"RENEW_INTERVAL"`
	RetryInterval    time.Duration `help:"Passive ownership retry interval." default:"1s" env:"RETRY_INTERVAL"`
	DialTimeout      time.Duration `help:"etcd connection timeout." default:"5s" env:"DIAL_TIMEOUT"`
}

// NewConfiguredRuntime creates a standalone runtime unless HA is explicitly
// enabled. In HA mode, cleanup closes the etcd client.
func NewConfiguredRuntime(
	config Config,
	conn *sql.DB,
	group *runtimejobs.Group,
	healthy func() bool,
) (*Runtime, func() error, error) {
	if !config.Enabled {
		runtime, err := NewStandaloneRuntime(group, healthy)
		return runtime, func() error { return nil }, err
	}
	if err := config.Validate(); err != nil {
		return nil, nil, err
	}

	password, err := readRuntimeSecret(config.EtcdPasswordFile)
	if err != nil {
		return nil, nil, fmt.Errorf("read HA etcd password: %w", err)
	}
	tlsConfig, err := loadServiceTLS(config.ServiceCAFile)
	if err != nil {
		return nil, nil, err
	}
	etcd, err := NewEtcdClient(clientv3.Config{
		Endpoints:   config.EtcdEndpoints,
		Username:    config.EtcdUsername,
		Password:    password,
		TLS:         tlsConfig.Clone(),
		DialTimeout: config.DialTimeout,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("create HA etcd client: %w", err)
	}

	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		_ = etcd.Close()
		return nil, nil, errors.New("default HTTP transport is not configurable for HA")
	}
	patroniTransport := transport.Clone()
	patroniTransport.TLSClientConfig = tlsConfig.Clone()
	cleanup := func() error {
		patroniTransport.CloseIdleConnections()
		return etcd.Close()
	}
	patroni := NewPatroniHTTPClient(&http.Client{
		Transport: patroniTransport,
		Timeout:   defaultHAHTTPTimeout,
	})
	queries := db.NewFailoverResettingQuerier(db.NewRetryDB(conn))
	observer, err := NewObserver(config.ClusterPath, etcd, queries, patroni)
	if err != nil {
		_ = cleanup()
		return nil, nil, err
	}
	coordinator, err := NewCoordinator(observer, NewLeaseStore(queries), CoordinatorConfig{
		LeaseDuration: config.LeaseDuration,
		RenewInterval: config.RenewInterval,
		RetryInterval: config.RetryInterval,
	})
	if err != nil {
		_ = cleanup()
		return nil, nil, err
	}
	runtime, err := NewRuntime(coordinator, group, healthy)
	if err != nil {
		_ = cleanup()
		return nil, nil, err
	}
	return runtime, cleanup, nil
}

func (config Config) Validate() error {
	if !config.Enabled {
		return nil
	}
	if config.ClusterPath == "" || len(config.EtcdEndpoints) == 0 || config.EtcdUsername == "" ||
		config.EtcdPasswordFile == "" || config.ServiceCAFile == "" || config.DialTimeout <= 0 {
		return errors.New("enabled HA requires cluster path, etcd endpoints and credentials, service CA, and a positive dial timeout")
	}
	for _, endpoint := range config.EtcdEndpoints {
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return fmt.Errorf("HA etcd endpoint must be an HTTPS URL: %q", endpoint)
		}
	}
	return nil
}

func readRuntimeSecret(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read secret file: %w", err)
	}
	secret := strings.TrimSpace(string(contents))
	if secret == "" {
		return "", errors.New("secret file is empty")
	}
	return secret, nil
}

func loadServiceTLS(path string) (*tls.Config, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read HA service CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(contents) {
		return nil, errors.New("HA service CA contains no certificates")
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    roots,
	}, nil
}
