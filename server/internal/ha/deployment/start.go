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

const localEtcdStartupTimeout = time.Minute

// StartInstalledServices is the role-aware systemd entrypoint for an installed HA node.
func StartInstalledServices(ctx context.Context, envPath, rootPasswordFile string) error {
	config, err := loadNodeConfig(envPath)
	if err != nil {
		return err
	}
	if err := RunCompose(ctx, []string{"--env-file", envPath, "--file", infrastructureCompose, "up", "-d", "--no-build", "etcd"}); err != nil {
		return fmt.Errorf("start etcd: %w", err)
	}

	if err := waitForEtcdQuorum(ctx, config); err != nil {
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
	if err := RunCompose(ctx, fleetComposeArgs("up", "-d", "--no-build", "--pull", "never", "fleet-api", "fleet-client")); err != nil {
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

func waitForEtcdQuorum(ctx context.Context, config NodeConfig) error {
	client, endpoints, err := unauthenticatedEtcdClient(config)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	localStartup := time.NewTimer(localEtcdStartupTimeout)
	defer localStartup.Stop()
	localDeadline := localStartup.C
	authenticated := false
	for {
		quorum, authRequired, localReady := startupEtcdQuorum(ctx, client.Status, endpoints, "https://"+config.NodeIP+":2379")
		if localReady {
			localDeadline = nil
		}
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
			return fmt.Errorf("stopped waiting for etcd quorum: %w", ctx.Err())
		case <-localDeadline:
			return errors.New("local etcd member did not become healthy within one minute")
		case <-time.After(2 * time.Second):
		}
	}
}

type startupEtcdStatus func(context.Context, string) (*clientv3.StatusResponse, error)

type startupEtcdIdentity struct {
	memberID uint64
	leaderID uint64
}

type startupEtcdResult struct {
	identity     startupEtcdIdentity
	authRequired bool
	required     bool
	responded    bool
}

func startupEtcdQuorum(ctx context.Context, status startupEtcdStatus, endpoints []string, requiredEndpoint string) (bool, bool, bool) {
	results := gather(endpoints, func(endpoint string) startupEtcdResult {
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		response, err := status(probeCtx, endpoint)
		if err != nil || response == nil || response.Header == nil {
			authRequired := etcdAuthRequired(err)
			return startupEtcdResult{authRequired: authRequired, required: endpoint == requiredEndpoint, responded: authRequired}
		}
		return startupEtcdResult{
			identity: startupEtcdIdentity{response.Header.MemberId, response.Leader},
			required: endpoint == requiredEndpoint, responded: true,
		}
	})
	membersByLeader := make(map[uint64]map[uint64]struct{})
	authRequired := false
	requiredResponded := false
	for _, result := range results {
		authRequired = authRequired || result.authRequired
		requiredResponded = requiredResponded || result.required && result.responded
		identity := result.identity
		if identity.memberID == 0 || identity.leaderID == 0 {
			continue
		}
		if membersByLeader[identity.leaderID] == nil {
			membersByLeader[identity.leaderID] = make(map[uint64]struct{})
		}
		membersByLeader[identity.leaderID][identity.memberID] = struct{}{}
	}
	for leaderID, members := range membersByLeader {
		if _, leaderResponded := members[leaderID]; requiredResponded && leaderResponded && len(members) >= 2 {
			return true, authRequired, requiredResponded
		}
	}
	return false, authRequired, requiredResponded
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
