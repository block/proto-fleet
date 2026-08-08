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
	Healthy(ctx context.Context) error
	AddRole(ctx context.Context, role string) error
	GrantPermission(ctx context.Context, role, prefix string, permission clientv3.PermissionType) error
	AddUser(ctx context.Context, user, password string) error
	GrantRole(ctx context.Context, user, role string) error
	EnableAuth(ctx context.Context) error
}

type etcdAuthClient struct {
	client   *clientv3.Client
	endpoint string
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
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{endpoint},
		DialTimeout: 5 * time.Second,
		TLS: &tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    roots,
		},
	})
	if err != nil {
		return fmt.Errorf("etcd auth bootstrap failed: connect to etcd: %w", err)
	}
	defer client.Close()

	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return bootstrapEtcdAuth(requestCtx, &etcdAuthClient{client: client, endpoint: endpoint}, rootPassword, patroniPassword, fleetPassword)
}

func bootstrapEtcdAuth(ctx context.Context, client authBootstrapClient, rootPassword, patroniPassword, fleetPassword string) error {
	if err := client.Healthy(ctx); err != nil {
		return fmt.Errorf("check local etcd member health: %w", err)
	}

	steps := []struct {
		name string
		run  func() error
	}{
		{"add Patroni role", func() error { return ignoreAlreadyExists(client.AddRole(ctx, "patroni"), rpctypes.ErrRoleAlreadyExist) }},
		{"grant Patroni DCS access", func() error {
			return client.GrantPermission(ctx, "patroni", patroniDCSPath, clientv3.PermissionType(clientv3.PermReadWrite))
		}},
		{"add Patroni user", func() error {
			return ignoreAlreadyExists(client.AddUser(ctx, "patroni", patroniPassword), rpctypes.ErrUserAlreadyExist)
		}},
		{"grant Patroni role", func() error { return client.GrantRole(ctx, "patroni", "patroni") }},
		{"add Fleet observer role", func() error {
			return ignoreAlreadyExists(client.AddRole(ctx, "fleet-observer"), rpctypes.ErrRoleAlreadyExist)
		}},
		{"grant Fleet read access", func() error {
			return client.GrantPermission(ctx, "fleet-observer", patroniDCSPath, clientv3.PermissionType(clientv3.PermRead))
		}},
		{"add Fleet observer user", func() error {
			return ignoreAlreadyExists(client.AddUser(ctx, "fleet-observer", fleetPassword), rpctypes.ErrUserAlreadyExist)
		}},
		{"grant Fleet observer role", func() error { return client.GrantRole(ctx, "fleet-observer", "fleet-observer") }},
		{"add root role", func() error { return ignoreAlreadyExists(client.AddRole(ctx, "root"), rpctypes.ErrRoleAlreadyExist) }},
		{"add root user", func() error {
			return ignoreAlreadyExists(client.AddUser(ctx, "root", rootPassword), rpctypes.ErrUserAlreadyExist)
		}},
		{"grant root role", func() error { return client.GrantRole(ctx, "root", "root") }},
		{"enable authentication", func() error { return client.EnableAuth(ctx) }},
	}
	for _, step := range steps {
		if err := step.run(); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}
	return nil
}

func ignoreAlreadyExists(err, alreadyExists error) error {
	if errors.Is(err, alreadyExists) {
		return nil
	}
	return err
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

func (c *etcdAuthClient) Healthy(ctx context.Context) error {
	_, err := c.client.Status(ctx, c.endpoint)
	if err != nil {
		return fmt.Errorf("get endpoint status: %w", err)
	}
	return nil
}

func (c *etcdAuthClient) AddRole(ctx context.Context, role string) error {
	_, err := c.client.RoleAdd(ctx, role)
	if err != nil {
		return fmt.Errorf("add role: %w", err)
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

func (c *etcdAuthClient) AddUser(ctx context.Context, user, password string) error {
	_, err := c.client.UserAdd(ctx, user, password)
	if err != nil {
		return fmt.Errorf("add user: %w", err)
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
