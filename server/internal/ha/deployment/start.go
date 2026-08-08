package deployment

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/block/proto-fleet/server/internal/ha"
)

const startupTimeout = 20 * time.Minute

// StartInstalledServices is the role-aware systemd entrypoint for an installed HA node.
func StartInstalledServices(ctx context.Context, envPath, rootPasswordFile string) error {
	config, err := loadNodeConfig(envPath)
	if err != nil {
		return err
	}
	if err := RunCompose(ctx, []string{"--env-file", envPath, "--file", filepath.Join(installRoot, "ha", "compose.yaml"), "up", "-d", "--no-build", "etcd"}); err != nil {
		return fmt.Errorf("start etcd: %w", err)
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	if err := waitForEtcdQuorum(deadlineCtx, config); err != nil {
		return err
	}
	authEnabled, err := etcdAuthEnabled(deadlineCtx, config)
	if err != nil {
		return err
	}
	if !authEnabled && config.NodeName == "ha-a" && rootPasswordFile != "" {
		if err := BootstrapEtcdAuth(deadlineCtx, envPath, rootPasswordFile); err != nil {
			return err
		}
		authEnabled = true
	}
	if !authEnabled && config.NodeName == "ha-a" {
		return errors.New("etcd authentication is not initialized; rerun the clean install with --etcd-root-password-file")
	}
	for !authEnabled {
		select {
		case <-deadlineCtx.Done():
			return errors.New("timed out waiting for ha-a to enable etcd authentication")
		case <-time.After(2 * time.Second):
		}
		authEnabled, err = etcdAuthEnabled(deadlineCtx, config)
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
	if err := RunCompose(ctx, []string{"--env-file", envPath, "--file", filepath.Join(installRoot, "ha", "compose.yaml"), "--profile", "database", "up", "-d", "--no-build", "--pull", "never", "patroni"}); err != nil {
		return fmt.Errorf("start Patroni: %w", err)
	}
	if err := RunCompose(ctx, fleetComposeArgs("up", "-d", "--no-build", "--pull", "never", "fleet-api", "fleet-client")); err != nil {
		return fmt.Errorf("start Fleet: %w", err)
	}
	return nil
}

func removeBootstrapCredential(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove installed etcd root password: %w", err)
	}
	return nil
}

func waitForEtcdQuorum(ctx context.Context, config NodeConfig) error {
	client, endpoints, err := unauthenticatedEtcdClient(config)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	authenticated := false
	for {
		quorum, authRequired := startupEtcdQuorum(ctx, client.Status, endpoints, "https://"+config.NodeIP+":2379")
		if quorum {
			return nil
		}
		if authRequired {
			if authenticated {
				return errors.New("fleet etcd observer credentials were rejected")
			}
			_ = client.Close()
			client, endpoints, err = authenticatedEtcdClient(config)
			if err != nil {
				return err
			}
			authenticated = true
			continue
		}
		select {
		case <-ctx.Done():
			return errors.New("timed out waiting for etcd quorum")
		case <-time.After(2 * time.Second):
		}
	}
}

type startupEtcdStatus func(context.Context, string) (*clientv3.StatusResponse, error)

type startupEtcdIdentity struct {
	clusterID uint64
	memberID  uint64
	leaderID  uint64
}

type startupEtcdResult struct {
	identity     startupEtcdIdentity
	authRequired bool
	required     bool
}

func startupEtcdQuorum(ctx context.Context, status startupEtcdStatus, endpoints []string, requiredEndpoint string) (bool, bool) {
	results := make(chan startupEtcdResult, len(endpoints))
	for _, endpoint := range endpoints {
		go func() {
			probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			response, err := status(probeCtx, endpoint)
			if err != nil || response == nil || response.Header == nil {
				results <- startupEtcdResult{authRequired: etcdAuthRequired(err)}
				return
			}
			results <- startupEtcdResult{
				identity: startupEtcdIdentity{response.Header.ClusterId, response.Header.MemberId, response.Leader},
				required: endpoint == requiredEndpoint,
			}
		}()
	}
	type memberGroup struct {
		members           map[uint64]struct{}
		requiredResponded bool
	}
	groups := make(map[[2]uint64]*memberGroup)
	authRequired := false
	for range endpoints {
		result := <-results
		authRequired = authRequired || result.authRequired
		identity := result.identity
		if identity.clusterID == 0 || identity.memberID == 0 || identity.leaderID == 0 {
			continue
		}
		key := [2]uint64{identity.clusterID, identity.leaderID}
		if groups[key] == nil {
			groups[key] = &memberGroup{members: make(map[uint64]struct{})}
		}
		groups[key].members[identity.memberID] = struct{}{}
		groups[key].requiredResponded = groups[key].requiredResponded || result.required
	}
	for key, group := range groups {
		if _, leaderResponded := group.members[key[1]]; leaderResponded && group.requiredResponded && len(group.members) >= 2 {
			return true, authRequired
		}
	}
	return false, authRequired
}

func etcdAuthRequired(err error) bool {
	return errors.Is(err, rpctypes.ErrPermissionDenied) || errors.Is(err, rpctypes.ErrAuthFailed) || errors.Is(err, rpctypes.ErrUserEmpty)
}

func etcdAuthEnabled(ctx context.Context, config NodeConfig) (bool, error) {
	client, _, err := unauthenticatedEtcdClient(config)
	if err != nil {
		return false, err
	}
	defer client.Close()
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err = client.Get(probeCtx, patroniDCSPath, clientv3.WithPrefix())
	switch {
	case err == nil:
		return false, nil
	case etcdAuthRequired(err):
		return true, nil
	default:
		return false, fmt.Errorf("check etcd authentication: %w", err)
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
	args := []string{"--env-file", envPath, "--file", filepath.Join(installRoot, "ha", "compose.yaml")}
	if databaseNode {
		args = append(args, "--profile", "database")
	}
	return append(args, "down")
}

func unauthenticatedEtcdClient(config NodeConfig) (*clientv3.Client, []string, error) {
	return startupEtcdClient(config, "", "")
}

func authenticatedEtcdClient(config NodeConfig) (*clientv3.Client, []string, error) {
	password, err := readPassword(filepath.Join(config.SecretsDir, fleetEtcdPasswordFile))
	if err != nil {
		return nil, nil, fmt.Errorf("read Fleet etcd password: %w", err)
	}
	return startupEtcdClient(config, "fleet-observer", password)
}

func startupEtcdClient(config NodeConfig, username, password string) (*clientv3.Client, []string, error) {
	tlsConfig, err := ha.LoadServiceTLS(filepath.Join(config.SecretsDir, "service-ca.crt"))
	if err != nil {
		return nil, nil, err
	}
	endpoints := []string{
		"https://" + config.DatabaseAIP + ":2379",
		"https://" + config.DatabaseBIP + ":2379",
		"https://" + config.WitnessIP + ":2379",
	}
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		Username:    username,
		Password:    password,
		TLS:         tlsConfig,
		DialTimeout: 2 * time.Second,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("create etcd startup client: %w", err)
	}
	return client, endpoints, nil
}
