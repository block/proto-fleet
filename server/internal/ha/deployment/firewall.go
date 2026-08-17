package deployment

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func applyFirewall(ctx context.Context, config NodeConfig, templatePath string) error {
	template, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("read firewall template: %w", err)
	}
	rules, err := renderFirewall(string(template), config)
	if err != nil {
		return err
	}
	// Keep an existing ruleset active until nft atomically commits its replacement.
	_, _ = runCommand(ctx, "sudo", "nft", "add", "table", "inet", "proto_fleet_ha")
	replacement := firewallReplacement(rules)
	if err := runWithInput(ctx, replacement, "sudo", "nft", "-c", "-f", "-"); err != nil {
		return fmt.Errorf("validate HA firewall: %w", err)
	}
	if err := runWithInput(ctx, replacement, "sudo", "nft", "-f", "-"); err != nil {
		return fmt.Errorf("apply HA firewall: %w", err)
	}
	return nil
}

func firewallReplacement(rules string) string {
	return "delete table inet proto_fleet_ha\n" + rules
}

func renderFirewall(template string, config NodeConfig) (string, error) {
	rules := strings.NewReplacer(
		"${HA_DB_A_IP}", config.DatabaseAIP,
		"${HA_DB_B_IP}", config.DatabaseBIP,
		"${HA_DCS_C_IP}", config.WitnessIP,
		"${HA_NODE_IP}", config.NodeIP,
		"${HA_NETWORK_INTERFACE}", config.NetworkInterface,
	).Replace(template)
	if strings.Contains(rules, "${") {
		return "", fmt.Errorf("firewall template contains an unresolved placeholder")
	}
	return rules, nil
}

func runWithInput(ctx context.Context, input, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdin = strings.NewReader(input)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("run %s: %s", name, commandError(output, err))
	}
	return nil
}
