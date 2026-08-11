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

	clientv3 "go.etcd.io/etcd/client/v3"
)

const patroniDCSPath = "/service/proto-fleet/"

type authBootstrapClient interface {
	Healthy(ctx context.Context) error
	AuthEnabled(ctx context.Context) (bool, error)
	ResetAuth(ctx context.Context) error
	AddRole(ctx context.Context, role string) error
	GrantPermission(ctx context.Context, role, prefix string, permission clientv3.PermissionType) error
	AddUser(ctx context.Context, user, password string) error
	GrantRole(ctx context.Context, user, role string) error
	EnableAuth(ctx context.Context) error
	VerifyPolicy(ctx context.Context, rootPassword, patroniPassword, fleetPassword string) error
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
		if err := client.VerifyPolicy(ctx, rootPassword, patroniPassword, fleetPassword); err != nil {
			return fmt.Errorf("verify existing authentication policy: %w", err)
		}
		return nil
	}
	if err := client.Healthy(ctx); err != nil {
		return fmt.Errorf("check local etcd member health: %w", err)
	}
	if err := client.ResetAuth(ctx); err != nil {
		return fmt.Errorf("reset incomplete authentication policy: %w", err)
	}

	steps := []struct {
		name string
		run  func() error
	}{
		{"add Patroni role", func() error { return client.AddRole(ctx, "patroni") }},
		{"grant Patroni DCS access", func() error {
			return client.GrantPermission(ctx, "patroni", patroniDCSPath, clientv3.PermissionType(clientv3.PermReadWrite))
		}},
		{"add Patroni user", func() error { return client.AddUser(ctx, "patroni", patroniPassword) }},
		{"grant Patroni role", func() error { return client.GrantRole(ctx, "patroni", "patroni") }},
		{"add Fleet observer role", func() error { return client.AddRole(ctx, "fleet-observer") }},
		{"grant Fleet read access", func() error {
			return client.GrantPermission(ctx, "fleet-observer", patroniDCSPath, clientv3.PermissionType(clientv3.PermRead))
		}},
		{"add Fleet observer user", func() error { return client.AddUser(ctx, "fleet-observer", fleetPassword) }},
		{"grant Fleet observer role", func() error { return client.GrantRole(ctx, "fleet-observer", "fleet-observer") }},
		{"add root role", func() error { return client.AddRole(ctx, "root") }},
		{"add root user", func() error { return client.AddUser(ctx, "root", rootPassword) }},
		{"grant root role", func() error { return client.GrantRole(ctx, "root", "root") }},
		{"enable authentication", func() error { return client.EnableAuth(ctx) }},
		{"verify authentication policy", func() error {
			return client.VerifyPolicy(ctx, rootPassword, patroniPassword, fleetPassword)
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

func (c *etcdAuthClient) Healthy(ctx context.Context) error {
	_, err := c.client.Status(ctx, c.endpoint)
	if err != nil {
		return fmt.Errorf("get endpoint status: %w", err)
	}
	return nil
}

func (c *etcdAuthClient) AuthEnabled(ctx context.Context) (bool, error) {
	status, err := c.client.AuthStatus(ctx)
	if err != nil {
		return false, fmt.Errorf("get authentication status: %w", err)
	}
	return status.Enabled, nil
}

func (c *etcdAuthClient) ResetAuth(ctx context.Context) error {
	users, err := c.client.UserList(ctx)
	if err != nil {
		return fmt.Errorf("list users: %w", err)
	}
	roles, err := c.client.RoleList(ctx)
	if err != nil {
		return fmt.Errorf("list roles: %w", err)
	}
	for _, user := range users.Users {
		if !bootstrapAuthPrincipal(user) {
			return fmt.Errorf("refusing to replace unexpected user %q", user)
		}
	}
	for _, role := range roles.Roles {
		if !bootstrapAuthPrincipal(role) {
			return fmt.Errorf("refusing to replace unexpected role %q", role)
		}
	}
	for _, user := range users.Users {
		if _, err := c.client.UserDelete(ctx, user); err != nil {
			return fmt.Errorf("delete user %q: %w", user, err)
		}
	}
	for _, role := range roles.Roles {
		if _, err := c.client.RoleDelete(ctx, role); err != nil {
			return fmt.Errorf("delete role %q: %w", role, err)
		}
	}
	return nil
}

func bootstrapAuthPrincipal(name string) bool {
	switch name {
	case "root", "patroni", "fleet-observer":
		return true
	default:
		return false
	}
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

func (c *etcdAuthClient) VerifyPolicy(ctx context.Context, rootPassword, patroniPassword, fleetPassword string) error {
	root, err := c.authenticatedClient("root", rootPassword)
	if err != nil {
		return err
	}
	defer root.Close()

	users, err := root.UserList(ctx)
	if err != nil {
		return fmt.Errorf("list users as root: %w", err)
	}
	if !sameNames(users.Users, "fleet-observer", "patroni", "root") {
		return fmt.Errorf("unexpected users: %v", users.Users)
	}
	roles, err := root.RoleList(ctx)
	if err != nil {
		return fmt.Errorf("list roles as root: %w", err)
	}
	if !sameNames(roles.Roles, "fleet-observer", "patroni", "root") {
		return fmt.Errorf("unexpected roles: %v", roles.Roles)
	}
	for _, expected := range []struct {
		user       string
		password   string
		permission clientv3.PermissionType
	}{
		{"root", rootPassword, clientv3.PermissionType(clientv3.PermReadWrite)},
		{"patroni", patroniPassword, clientv3.PermissionType(clientv3.PermReadWrite)},
		{"fleet-observer", fleetPassword, clientv3.PermissionType(clientv3.PermRead)},
	} {
		credential, err := c.authenticatedClient(expected.user, expected.password)
		if err != nil {
			return err
		}
		if _, err := credential.Authenticate(ctx, expected.user, expected.password); err != nil {
			credential.Close()
			return fmt.Errorf("authenticate as %s: %w", expected.user, err)
		}
		credential.Close()

		user, err := root.UserGet(ctx, expected.user)
		if err != nil {
			return fmt.Errorf("get user %s: %w", expected.user, err)
		}
		if !sameNames(user.Roles, expected.user) {
			return fmt.Errorf("user %s has unexpected roles: %v", expected.user, user.Roles)
		}
		role, err := root.RoleGet(ctx, expected.user)
		if err != nil {
			return fmt.Errorf("get role %s: %w", expected.user, err)
		}
		if expected.user == "root" {
			if len(role.Perm) != 0 {
				return errors.New("root role has unexpected explicit permissions")
			}
			continue
		}
		if len(role.Perm) != 1 || clientv3.PermissionType(role.Perm[0].PermType) != expected.permission || string(role.Perm[0].Key) != patroniDCSPath || string(role.Perm[0].RangeEnd) != clientv3.GetPrefixRangeEnd(patroniDCSPath) {
			return fmt.Errorf("role %s does not have the expected DCS permission", expected.user)
		}
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

func sameNames(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	wanted := make(map[string]struct{}, len(want))
	for _, name := range want {
		wanted[name] = struct{}{}
	}
	for _, name := range got {
		if _, ok := wanted[name]; !ok {
			return false
		}
		delete(wanted, name)
	}
	return len(wanted) == 0
}
