package ha

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
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
	EndpointIP       string        `help:"Stable endpoint IPv4 address owned by keepalived." env:"ENDPOINT_IP"`
}

// NewConfiguredRuntime creates a standalone runtime unless HA is explicitly
// enabled. In HA mode, cleanup closes the prepared queries and etcd client.
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
	if group == nil {
		return nil, nil, errors.New("HA runtime requires a runtime job group")
	}
	if healthy == nil {
		return nil, nil, errors.New("HA runtime requires a critical health check")
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
	patroni := NewPatroniHTTPClient(&http.Client{
		Transport: patroniTransport,
		Timeout:   defaultHAHTTPTimeout,
	})
	queries, err := db.NewPreparedQuerier(context.Background(), conn)
	if err != nil {
		patroniTransport.CloseIdleConnections()
		_ = etcd.Close()
		return nil, nil, fmt.Errorf("prepare HA database queries: %w", err)
	}
	cleanup := func() error {
		patroniTransport.CloseIdleConnections()
		queriesErr := queries.Close()
		if queriesErr != nil {
			queriesErr = fmt.Errorf("close HA prepared queries: %w", queriesErr)
		}
		etcdErr := etcd.Close()
		if etcdErr != nil {
			etcdErr = fmt.Errorf("close HA etcd client: %w", etcdErr)
		}
		return errors.Join(queriesErr, etcdErr)
	}
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
	endpointHealthy := newEndpointHealth(netip.MustParseAddr(config.EndpointIP), EndpointHeartbeatFile, endpointHeartbeatTimeout)
	runtime := newRuntime(coordinator, group, healthy, RuntimeConfig{
		EndpointHealthy: endpointHealthy,
	})
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
	if config.LeaseDuration <= 0 || config.LeaseDuration%time.Millisecond != 0 {
		return errors.New("HA lease duration must be a positive whole number of milliseconds")
	}
	if config.RenewInterval <= 0 || config.RetryInterval <= 0 {
		return errors.New("HA renew and retry intervals must be positive")
	}
	if config.RenewInterval >= config.LeaseDuration {
		return errors.New("HA renew interval must be less than the lease duration")
	}
	endpointIP, err := netip.ParseAddr(config.EndpointIP)
	if err != nil || !endpointIP.Is4() || !endpointIP.IsGlobalUnicast() || endpointIP.As4()[0] == 0 {
		return errors.New("HA endpoint IP must be a routable literal IPv4 address")
	}
	for _, endpoint := range config.EtcdEndpoints {
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return fmt.Errorf("HA etcd endpoint must be an HTTPS URL: %q", endpoint)
		}
		if net.ParseIP(parsed.Hostname()) == nil {
			return fmt.Errorf("HA etcd endpoint must use an IP address: %q", endpoint)
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
