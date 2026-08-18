package deployment

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/block/proto-fleet/server/internal/ha"
)

// StartInstalledServices is the role-aware systemd entrypoint for an installed HA node.
func StartInstalledServices(ctx context.Context, envPath, rootPasswordFile string) error {
	config, err := loadNodeConfig(envPath)
	if err != nil {
		return err
	}
	if err := RunCompose(ctx, []string{"--env-file", envPath, "--file", infrastructureCompose, "up", "-d", "--no-build", "etcd"}); err != nil {
		return fmt.Errorf("start etcd: %w", err)
	}
	if err := waitForLocalEtcd(ctx, config); err != nil {
		return err
	}

	authEnabled, err := waitForEtcdAuthProbe(ctx, config)
	if err != nil {
		return err
	}
	bootstrapAuth, err := shouldBootstrapEtcdAuth(config.NodeName, authEnabled, rootPasswordFile)
	if err != nil {
		return err
	}
	if bootstrapAuth {
		if err := BootstrapEtcdAuth(ctx, envPath, rootPasswordFile); err != nil {
			return err
		}
		authEnabled = true
	}
	if !authEnabled && config.NodeName == "ha-a" {
		return errors.New("etcd authentication is not initialized; reimage this dedicated host and rerun the guided install")
	}
	for !authEnabled {
		select {
		case <-ctx.Done():
			return fmt.Errorf("stopped waiting for ha-a to enable etcd authentication: %w", ctx.Err())
		case <-time.After(2 * time.Second):
		}
		authEnabled, err = waitForEtcdAuthProbe(ctx, config)
		if err != nil {
			return err
		}
	}
	if config.NodeName == "ha-a" && rootPasswordFile != "" {
		if err := removeBootstrapCredential(rootPasswordFile); err != nil {
			return err
		}
	}
	if !config.isDatabaseNode() {
		return nil
	}
	if err := RunCompose(ctx, []string{"--env-file", envPath, "--file", infrastructureCompose, "--profile", "database", "up", "-d", "--no-build", "--pull", "never", "patroni"}); err != nil {
		return fmt.Errorf("start Patroni: %w", err)
	}
	if err := RunCompose(ctx, fleetApplicationComposeArgs("up", "-d", "--no-build", "--pull", "never", "--wait", "--wait-timeout", "60")); err != nil {
		return fmt.Errorf("start Fleet: %w", err)
	}
	return nil
}

func shouldBootstrapEtcdAuth(nodeName string, authEnabled bool, rootPasswordFile string) (bool, error) {
	if nodeName != "ha-a" || rootPasswordFile == "" {
		return false, nil
	}
	if _, err := os.Stat(rootPasswordFile); err == nil {
		return true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("inspect installed etcd root password: %w", err)
	}
	if authEnabled {
		return false, nil
	}
	return false, errors.New("etcd authentication is disabled but the installed root password is missing; reimage this dedicated host and rerun the guided install")
}

func removeBootstrapCredential(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove installed etcd root password: %w", err)
	}
	return nil
}

func waitForLocalEtcd(ctx context.Context, config NodeConfig) error {
	// The peer listener opens before quorum, unlike the client TLS endpoint.
	address := net.JoinHostPort(config.NodeIP, "2380")
	deadline := time.NewTimer(time.Minute)
	defer deadline.Stop()
	for {
		if probeLocalEtcdListener(ctx, address) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("stopped waiting for local etcd member: %w", ctx.Err())
		case <-deadline.C:
			return errors.New("local etcd member did not start within one minute")
		case <-time.After(2 * time.Second):
		}
	}
}

func probeLocalEtcdListener(ctx context.Context, address string) bool {
	connection, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

func etcdAuthRequired(err error) bool {
	return errors.Is(err, rpctypes.ErrPermissionDenied) || errors.Is(err, rpctypes.ErrAuthFailed) || errors.Is(err, rpctypes.ErrUserEmpty)
}

func etcdAuthEnabled(ctx context.Context, config NodeConfig) (bool, error) {
	client, err := startupEtcdClient(config)
	if err != nil {
		return false, err
	}
	defer client.Close()
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err = client.Get(probeCtx, patroniDCSPath, clientv3.WithPrefix(), clientv3.WithLimit(1))
	switch {
	case err == nil:
		return false, nil
	case etcdAuthRequired(err):
		return true, nil
	default:
		return false, fmt.Errorf("check etcd authentication: %w", err)
	}
}

func waitForEtcdAuthProbe(ctx context.Context, config NodeConfig) (bool, error) {
	for {
		enabled, err := etcdAuthEnabled(ctx, config)
		if err == nil {
			return enabled, nil
		}
		select {
		case <-ctx.Done():
			return false, fmt.Errorf("stopped checking etcd authentication: %w", ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}

// StopInstalledServices stops only the services owned by this HA profile.
func StopInstalledServices(ctx context.Context, envPath string) error {
	config, err := loadNodeConfig(envPath)
	if err != nil {
		return err
	}
	var stopFleetErr error
	if config.isDatabaseNode() {
		if err := RunCompose(ctx, fleetComposeArgs("down")); err != nil {
			stopFleetErr = fmt.Errorf("stop Fleet: %w", err)
		}
	}
	stopInfrastructureErr := RunCompose(ctx, infrastructureDownArgs(envPath, config.isDatabaseNode()))
	if stopInfrastructureErr != nil {
		stopInfrastructureErr = fmt.Errorf("stop infrastructure: %w", stopInfrastructureErr)
	}
	return errors.Join(stopFleetErr, stopInfrastructureErr)
}

func infrastructureDownArgs(envPath string, databaseNode bool) []string {
	args := []string{"--env-file", envPath, "--file", infrastructureCompose}
	if databaseNode {
		args = append(args, "--profile", "database")
	}
	return append(args, "down")
}

func startupEtcdClient(config NodeConfig) (*clientv3.Client, error) {
	tlsConfig, err := ha.LoadServiceTLS(filepath.Join(config.SecretsDir, "service-ca.crt"))
	if err != nil {
		return nil, err
	}
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"https://" + config.NodeIP + ":2379"},
		TLS:         tlsConfig,
		DialTimeout: 2 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("create etcd startup client: %w", err)
	}
	return client, nil
}
