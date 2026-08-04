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
	if err := runWithInput(ctx, rules, "sudo", "nft", "-c", "-f", "-"); err != nil {
		return fmt.Errorf("validate HA firewall: %w", err)
	}
	if err := runWithInput(ctx, rules, "sudo", "nft", "-f", "-"); err != nil {
		return fmt.Errorf("apply HA firewall: %w", err)
	}
	return nil
}

func renderFirewall(template string, config NodeConfig) (string, error) {
	rules := strings.NewReplacer(
		"${HA_DB_A_IP}", config.DatabaseAIP,
		"${HA_DB_B_IP}", config.DatabaseBIP,
		"${HA_DCS_C_IP}", config.WitnessIP,
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
