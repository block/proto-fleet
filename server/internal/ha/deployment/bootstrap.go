package deployment

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
)

const patroniDCSPath = "/service/proto-fleet/"

type authBootstrapClient interface {
	AuthEnabled(ctx context.Context) (bool, error)
	EnsureRole(ctx context.Context, role string) error
	GrantPermission(ctx context.Context, role, prefix string, permission clientv3.PermissionType) error
	EnsureUser(ctx context.Context, user, password string) error
	GrantRole(ctx context.Context, user, role string) error
	EnableAuth(ctx context.Context) error
	VerifyAccess(ctx context.Context, rootPassword, patroniPassword, fleetPassword string) error
}

type etcdAuthClient struct {
	client    *clientv3.Client
	endpoint  string
	tlsConfig *tls.Config
}

// BootstrapEtcdAuth creates the complete least-privilege role set, then enables auth.
func BootstrapEtcdAuth(ctx context.Context, envPath, rootPasswordFile string) error {
	config, err := loadNodeConfig(envPath)
	if err != nil {
		return err
	}
	if config.NodeIP == "" || config.SecretsDir == "" {
		return errors.New("etcd auth bootstrap failed: HA_NODE_IP and HA_SECRETS_DIR are required")
	}

	rootPassword, err := readPassword(rootPasswordFile)
	if err != nil {
		return fmt.Errorf("etcd auth bootstrap failed: read etcd root password: %w", err)
	}
	fleetPassword, err := readPassword(filepath.Join(config.SecretsDir, fleetEtcdPasswordFile))
	if err != nil {
		return fmt.Errorf("etcd auth bootstrap failed: read Fleet etcd password: %w", err)
	}
	patroniPassword, err := readPassword(filepath.Join(config.SecretsDir, patroniEtcdPasswordFile))
	if err != nil {
		return fmt.Errorf("etcd auth bootstrap failed: read Patroni etcd password: %w", err)
	}

	caPEM, err := os.ReadFile(filepath.Join(config.SecretsDir, "service-ca.crt"))
	if err != nil {
		return fmt.Errorf("etcd auth bootstrap failed: read service CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return errors.New("etcd auth bootstrap failed: service CA is not a PEM certificate")
	}
	endpoint := "https://" + config.NodeIP + ":2379"
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
	}
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{endpoint},
		DialTimeout: 5 * time.Second,
		TLS:         tlsConfig,
	})
	if err != nil {
		return fmt.Errorf("etcd auth bootstrap failed: connect to etcd: %w", err)
	}
	defer client.Close()

	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return bootstrapEtcdAuth(requestCtx, &etcdAuthClient{client: client, endpoint: endpoint, tlsConfig: tlsConfig}, rootPassword, patroniPassword, fleetPassword)
}

func bootstrapEtcdAuth(ctx context.Context, client authBootstrapClient, rootPassword, patroniPassword, fleetPassword string) error {
	authEnabled, err := client.AuthEnabled(ctx)
	if err != nil {
		return fmt.Errorf("check authentication status: %w", err)
	}
	if authEnabled {
		if err := client.VerifyAccess(ctx, rootPassword, patroniPassword, fleetPassword); err != nil {
			return fmt.Errorf("verify existing authentication access: %w", err)
		}
		return nil
	}

	steps := []struct {
		name string
		run  func() error
	}{
		{"ensure Patroni role", func() error { return client.EnsureRole(ctx, "patroni") }},
		{"grant Patroni DCS access", func() error {
			return client.GrantPermission(ctx, "patroni", patroniDCSPath, clientv3.PermissionType(clientv3.PermReadWrite))
		}},
		{"ensure Patroni user", func() error { return client.EnsureUser(ctx, "patroni", patroniPassword) }},
		{"grant Patroni role", func() error { return client.GrantRole(ctx, "patroni", "patroni") }},
		{"ensure Fleet observer role", func() error { return client.EnsureRole(ctx, "fleet-observer") }},
		{"grant Fleet read access", func() error {
			return client.GrantPermission(ctx, "fleet-observer", patroniDCSPath, clientv3.PermissionType(clientv3.PermRead))
		}},
		{"ensure Fleet observer user", func() error { return client.EnsureUser(ctx, "fleet-observer", fleetPassword) }},
		{"grant Fleet observer role", func() error { return client.GrantRole(ctx, "fleet-observer", "fleet-observer") }},
		{"ensure root role", func() error { return client.EnsureRole(ctx, "root") }},
		{"ensure root user", func() error { return client.EnsureUser(ctx, "root", rootPassword) }},
		{"grant root role", func() error { return client.GrantRole(ctx, "root", "root") }},
		{"enable authentication", func() error { return client.EnableAuth(ctx) }},
		{"verify authentication access", func() error {
			return client.VerifyAccess(ctx, rootPassword, patroniPassword, fleetPassword)
		}},
	}
	for _, step := range steps {
		if err := step.run(); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}
	return nil
}

func readPassword(path string) (string, error) {
	info, err := secureFileInfo(path, 0o600)
	if err != nil {
		return "", err
	}
	if err := requireCurrentOwner(info, filepath.Base(path)); err != nil {
		return "", err
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read password file: %w", err)
	}
	password := strings.TrimSpace(string(contents))
	if password == "" {
		return "", errors.New("password must not be empty")
	}
	return password, nil
}

func (c *etcdAuthClient) AuthEnabled(ctx context.Context) (bool, error) {
	status, err := c.client.AuthStatus(ctx)
	if err != nil {
		return false, fmt.Errorf("get authentication status: %w", err)
	}
	return status.Enabled, nil
}

func (c *etcdAuthClient) EnsureRole(ctx context.Context, role string) error {
	_, err := c.client.RoleAdd(ctx, role)
	if errors.Is(err, rpctypes.ErrRoleAlreadyExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("ensure role: %w", err)
	}
	return nil
}

func (c *etcdAuthClient) GrantPermission(ctx context.Context, role, prefix string, permission clientv3.PermissionType) error {
	_, err := c.client.RoleGrantPermission(ctx, role, prefix, clientv3.GetPrefixRangeEnd(prefix), permission)
	if err != nil {
		return fmt.Errorf("grant permission: %w", err)
	}
	return nil
}

func (c *etcdAuthClient) EnsureUser(ctx context.Context, user, password string) error {
	_, err := c.client.UserAdd(ctx, user, password)
	if errors.Is(err, rpctypes.ErrUserAlreadyExist) {
		_, err = c.client.UserChangePassword(ctx, user, password)
	}
	if err != nil {
		return fmt.Errorf("ensure user: %w", err)
	}
	return nil
}

func (c *etcdAuthClient) GrantRole(ctx context.Context, user, role string) error {
	_, err := c.client.UserGrantRole(ctx, user, role)
	if err != nil {
		return fmt.Errorf("grant role: %w", err)
	}
	return nil
}

func (c *etcdAuthClient) EnableAuth(ctx context.Context) error {
	_, err := c.client.AuthEnable(ctx)
	if err != nil {
		return fmt.Errorf("enable auth: %w", err)
	}
	return nil
}

func (c *etcdAuthClient) VerifyAccess(ctx context.Context, rootPassword, patroniPassword, fleetPassword string) error {
	root, err := c.authenticatedClient("root", rootPassword)
	if err != nil {
		return err
	}
	defer root.Close()

	if _, err := root.Authenticate(ctx, "root", rootPassword); err != nil {
		return fmt.Errorf("authenticate as root: %w", err)
	}
	patroni, err := c.authenticatedClient("patroni", patroniPassword)
	if err != nil {
		return err
	}
	defer patroni.Close()
	probeKey := patroniDCSPath + "auth-smoke-test"
	if _, err := patroni.Put(ctx, probeKey, "ok"); err != nil {
		return fmt.Errorf("write DCS as patroni: %w", err)
	}
	if _, err := patroni.Delete(ctx, probeKey); err != nil {
		return fmt.Errorf("remove Patroni DCS smoke test: %w", err)
	}

	fleet, err := c.authenticatedClient("fleet-observer", fleetPassword)
	if err != nil {
		return err
	}
	defer fleet.Close()
	if _, err := fleet.Get(ctx, patroniDCSPath, clientv3.WithPrefix(), clientv3.WithLimit(1)); err != nil {
		return fmt.Errorf("read DCS as Fleet observer: %w", err)
	}
	if _, err := fleet.Put(ctx, probeKey, "denied"); !errors.Is(err, rpctypes.ErrPermissionDenied) {
		return errors.New("Fleet observer unexpectedly has DCS write access")
	}
	return nil
}

func (c *etcdAuthClient) authenticatedClient(user, password string) (*clientv3.Client, error) {
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{c.endpoint},
		DialTimeout: 5 * time.Second,
		TLS:         c.tlsConfig.Clone(),
		Username:    user,
		Password:    password,
	})
	if err != nil {
		return nil, fmt.Errorf("connect as %s: %w", user, err)
	}
	return client, nil
}
