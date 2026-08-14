package deployment

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

const installedNodeEnvironment = configRoot + "/node.env"

type uninstallDependencies struct {
	input        io.Reader
	output       io.Writer
	euid         func() int
	terminal     func() bool
	loadConfig   func(string) (NodeConfig, error)
	lstat        func(string) (os.FileInfo, error)
	run          func(context.Context, string, ...string) ([]byte, error)
	stopServices func(context.Context, string) error
}

// Uninstall removes the local HA runtime while preserving host-level dependencies.
func Uninstall(ctx context.Context, purgeData bool) error {
	return uninstall(ctx, purgeData, defaultUninstallDependencies())
}

func defaultUninstallDependencies() uninstallDependencies {
	return uninstallDependencies{
		input: os.Stdin, output: os.Stdout, euid: os.Geteuid,
		terminal:   func() bool { return term.IsTerminal(int(os.Stdin.Fd())) },
		loadConfig: loadNodeConfig, lstat: os.Lstat, run: runCommand,
		stopServices: StopInstalledServices,
	}
}

func uninstall(ctx context.Context, purgeData bool, deps uninstallDependencies) error {
	config, err := validateUninstall(ctx, deps)
	if err != nil {
		return err
	}
	if err := printUninstallSummary(deps.output, config, purgeData); err != nil {
		return err
	}
	if err := confirmUninstall(deps.input, deps.output); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(deps.output, "[shutdown] Stopping the local HA runtime..."); err != nil {
		return fmt.Errorf("write uninstall output: %w", err)
	}
	if config.isDatabaseNode() {
		if err := uninstallStep(ctx, deps, "stop host updater", "systemctl", "disable", "--now", "proto-fleet-updater.service"); err != nil {
			return err
		}
		if err := uninstallStep(ctx, deps, "stop VIP routing", "systemctl", "disable", "--now", "keepalived.service"); err != nil {
			return err
		}
		if err := uninstallStep(ctx, deps, "withdraw virtual IP", "ip", "address", "flush", "to", config.VirtualIP+"/32", "dev", config.NetworkInterface); err != nil {
			return err
		}
	}
	if err := uninstallStep(ctx, deps, "disable HA services", "systemctl", "disable", "--now", "proto-fleet-ha.service"); err != nil {
		return err
	}
	if err := deps.stopServices(ctx, installedNodeEnvironment); err != nil {
		return fmt.Errorf("stop installed HA containers: %w", err)
	}

	if _, err := fmt.Fprintln(deps.output, "[host cleanup] Removing HA-owned services, firewall, and runtime files..."); err != nil {
		return fmt.Errorf("write uninstall output: %w", err)
	}
	if err := uninstallStep(ctx, deps, "remove Docker HA dependencies", "rm", "-f", "--", dockerDropIn, dockerRecoveryDropIn); err != nil {
		return err
	}
	if err := uninstallStep(ctx, deps, "reload systemd without Docker HA dependencies", "systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := uninstallStep(ctx, deps, "disable HA firewall", "systemctl", "disable", "--now", "proto-fleet-ha-firewall.service"); err != nil {
		return err
	}
	if err := deleteHAFirewallTable(ctx, deps); err != nil {
		return err
	}

	resetUnits := []string{"reset-failed", "proto-fleet-ha.service", "proto-fleet-ha-firewall.service"}
	if config.isDatabaseNode() {
		resetUnits = append(resetUnits, "keepalived.service")
		resetUnits = append(resetUnits, "proto-fleet-updater.service")
	}
	if err := uninstallStep(ctx, deps, "clear HA service failures", "systemctl", resetUnits...); err != nil {
		return err
	}
	if err := removeHAArtifacts(ctx, deps, config.isDatabaseNode()); err != nil {
		return err
	}
	if err := uninstallStep(ctx, deps, "reload systemd after HA removal", "systemctl", "daemon-reload"); err != nil {
		return err
	}

	if purgeData {
		if _, err := fmt.Fprintln(deps.output, "[data removal] Deleting HA database, DCS, configuration, and credentials..."); err != nil {
			return fmt.Errorf("write uninstall output: %w", err)
		}
		if err := uninstallStep(ctx, deps, "remove HA data", "rm", "-rf", "--", dataRoot); err != nil {
			return err
		}
		if err := uninstallStep(ctx, deps, "remove HA configuration", "rm", "-rf", "--", configRoot); err != nil {
			return err
		}
	}

	if purgeData {
		_, err = fmt.Fprintln(deps.output, "Proto Fleet HA was removed; this host is ready for a fresh guided installation")
	} else {
		_, err = fmt.Fprintf(deps.output, "Proto Fleet HA was removed; retained state remains at %s and %s and blocks a fresh install\n", configRoot, dataRoot)
	}
	if err != nil {
		return fmt.Errorf("write uninstall output: %w", err)
	}
	return nil
}

func validateUninstall(ctx context.Context, deps uninstallDependencies) (NodeConfig, error) {
	if deps.euid() != 0 {
		return NodeConfig{}, errors.New("fleet-ha uninstall requires root; run it with sudo")
	}
	if !deps.terminal() {
		return NodeConfig{}, errors.New("fleet-ha uninstall requires an interactive terminal; use ssh -t HOST 'sudo fleet-ha uninstall [--purge-data]'")
	}
	config, err := deps.loadConfig(installedNodeEnvironment)
	if err != nil {
		return NodeConfig{}, err
	}
	if err := validateNodeConfig(config); err != nil {
		return NodeConfig{}, fmt.Errorf("installed HA configuration is invalid: %w", err)
	}
	if config.DataDir != dataRoot {
		return NodeConfig{}, fmt.Errorf("installed HA_DATA_DIR must be %s", dataRoot)
	}
	if config.SecretsDir != configRoot {
		return NodeConfig{}, fmt.Errorf("installed HA_SECRETS_DIR must be %s", configRoot)
	}

	required := []string{serviceUnit, firewallUnit, nftablesDropIn, dockerDropIn, infrastructureCompose, installRoot + "/ha/fleet-ha"}
	if config.isDatabaseNode() {
		required = append(required,
			keepalivedConfig,
			updaterDropIn, haUpdaterDropIn,
			updaterBinary, updaterUnit, updaterEnvironment,
		)
	}
	for _, path := range required {
		info, err := deps.lstat(path)
		if err != nil {
			return NodeConfig{}, fmt.Errorf("HA installation is incomplete: inspect %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return NodeConfig{}, fmt.Errorf("HA installation is incomplete: expected a regular file at %s", path)
		}
	}
	if output, err := deps.run(ctx, "docker", "info"); err != nil {
		return NodeConfig{}, fmt.Errorf("Docker must be running before uninstall: %s", commandError(output, err))
	}
	return config, nil
}

func printUninstallSummary(output io.Writer, config NodeConfig, purgeData bool) error {
	role := "witness"
	if config.isDatabaseNode() {
		role = "database"
	}
	dataAction := fmt.Sprintf("preserve %s and %s", configRoot, dataRoot)
	if purgeData {
		dataAction = fmt.Sprintf("permanently delete %s and %s", configRoot, dataRoot)
	}
	if _, err := fmt.Fprintf(output, "Proto Fleet HA uninstall\n  node: %s (%s)\n  runtime: remove\n  persistent state: %s\n", config.NodeName, role, dataAction); err != nil {
		return fmt.Errorf("write uninstall summary: %w", err)
	}
	if !purgeData {
		if _, err := fmt.Fprintln(output, "Retained state blocks a fresh guided install and cannot be purged by a later uninstall invocation."); err != nil {
			return fmt.Errorf("write uninstall summary: %w", err)
		}
	}
	return nil
}

func confirmUninstall(input io.Reader, output io.Writer) error {
	if _, err := fmt.Fprint(output, "Type UNINSTALL to continue: "); err != nil {
		return fmt.Errorf("write uninstall prompt: %w", err)
	}
	scanner := bufio.NewScanner(input)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("read uninstall input: %w", err)
		}
		return errors.New("uninstall canceled: input closed")
	}
	if !strings.EqualFold(strings.TrimSpace(scanner.Text()), "UNINSTALL") {
		return errors.New("uninstall canceled")
	}
	return nil
}

func uninstallStep(ctx context.Context, deps uninstallDependencies, action, name string, args ...string) error {
	output, err := deps.run(ctx, name, args...)
	if err != nil {
		return fmt.Errorf("%s: %s", action, commandError(output, err))
	}
	return nil
}

func deleteHAFirewallTable(ctx context.Context, deps uninstallDependencies) error {
	output, err := deps.run(ctx, "nft", "delete", "table", "inet", "proto_fleet_ha")
	if err == nil || strings.Contains(string(output), "No such file or directory") {
		return nil
	}
	return fmt.Errorf("remove HA firewall table: %s", commandError(output, err))
}

func removeHAArtifacts(ctx context.Context, deps uninstallDependencies, databaseNode bool) error {
	files := []string{
		serviceUnit, firewallUnit, nftablesDropIn,
	}
	if databaseNode {
		files = append(files,
			updaterDropIn, haUpdaterDropIn,
			"/etc/systemd/system/proto-fleet-ha.service.d/keepalived.conf",
			keepalivedOverride,
			keepalivedConfig, keepalivedHealthCheck,
			updaterUnit, updaterEnvironment,
			updaterBinary, updaterBinary+".candidate", updaterBinary+".previous",
			updaterBinary+".handoff", updaterBinary+".handoff.tmp", updaterBinary+".restore",
		)
	}
	if err := uninstallStep(ctx, deps, "remove HA service files", "rm", append([]string{"-f", "--"}, files...)...); err != nil {
		return err
	}
	paths := []string{"/run/proto-fleet-ha", installBase}
	if databaseNode {
		paths = append([]string{"/var/lib/proto-fleet-updater", "/run/proto-fleet-updater"}, paths...)
	}
	for _, path := range paths {
		if err := uninstallStep(ctx, deps, "remove HA runtime path "+path, "rm", "-rf", "--", path); err != nil {
			return err
		}
	}
	return nil
}
