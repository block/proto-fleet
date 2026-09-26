package deployment

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// PrepareExternal creates protected per-host configuration for a separately
// provisioned load balancer. It neither contacts peers nor installs services.
// Transfer each node directory only to its matching host; keep offline secrets
// separate. No cloud-provider SDK or SSH access is required.
func PrepareExternal(output string, hostIPs [3]string, publicURL string) error {
	root, err := filepath.Abs(output)
	if err != nil {
		return fmt.Errorf("resolve output directory: %w", err)
	}
	profile, err := captureFleetApplicationEnvironment()
	if err != nil {
		return err
	}
	names := [3]string{"ha-a", "ha-b", "ha-c"}
	configs := [3]NodeConfig{}
	for i, name := range names {
		configs[i] = NodeConfig{
			EndpointMode: endpointModeExternal, PublicURL: publicURL,
			NodeName: name, NodeIP: hostIPs[i], DatabaseAIP: hostIPs[0], DatabaseBIP: hostIPs[1], WitnessIP: hostIPs[2],
			DataDir: dataRoot, SecretsDir: filepath.Join(root, name),
		}
		if err := validateNodeConfig(configs[i]); err != nil {
			return err
		}
	}
	if err := generateSecrets(root, hostIPs, "", true); err != nil {
		return err
	}
	for i, name := range names {
		dir := filepath.Join(root, name)
		if err := writeFile(filepath.Join(dir, "node.env"), []byte(renderNodeEnvironment(configs[i])), 0o600); err != nil {
			return err
		}
		if i < 2 {
			path := filepath.Join(dir, fleetEnvironmentFile)
			contents, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read prepared application environment: %w", err)
			}
			if err := writeFile(path, append(contents, renderFleetDeploymentEnvironment(profile)...), 0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

// InstallPreparedConfig reuses the dedicated-host installer for protected
// configuration delivered by an orchestrator such as SSM. The root credential
// is accepted only on ha-a and removed by the existing authenticated start gate.
func InstallPreparedConfig(ctx context.Context, options InstallOptions) error {
	config, err := loadNodeConfig(options.NodeEnvPath)
	if err != nil {
		return err
	}
	if err := validateNodeConfig(config); err != nil {
		return err
	}
	if !config.externalEndpoint() {
		return fmt.Errorf("prepared-config installation requires external endpoint mode; use guided install for VIP mode")
	}
	if config.DataDir != dataRoot {
		return fmt.Errorf("HA_DATA_DIR must be %s", dataRoot)
	}
	if (config.NodeName == "ha-a") != (options.EtcdRootPasswordFile != "") {
		return fmt.Errorf("provide the etcd root password file only on ha-a")
	}
	return install(ctx, options, defaultInstallDependencies())
}
