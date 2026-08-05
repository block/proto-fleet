package deployment

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/block/proto-fleet/server/internal/ha"
)

// RenderKeepalivedConfig renders the local Fleet host's unicast VRRP configuration.
func RenderKeepalivedConfig(envPath, templatePath, outputPath string) error {
	config, err := loadNodeConfig(envPath)
	if err != nil {
		return err
	}
	if err := validateNodeConfig(config); err != nil {
		return fmt.Errorf("render keepalived config: %w", err)
	}
	template, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("read keepalived template: %w", err)
	}
	rendered, err := renderKeepalivedConfig(string(template), config)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
		return fmt.Errorf("create keepalived config directory: %w", err)
	}
	if err := os.WriteFile(outputPath, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("write keepalived config: %w", err)
	}
	return nil
}

func renderKeepalivedConfig(template string, config NodeConfig) (string, error) {
	var peerIP string
	switch config.NodeName {
	case "ha-a":
		peerIP = config.DatabaseBIP
	case "ha-b":
		peerIP = config.DatabaseAIP
	case "ha-c":
		return "", fmt.Errorf("keepalived runs only on Fleet hosts")
	default:
		return "", fmt.Errorf("invalid HA node name: %s", config.NodeName)
	}
	rendered := strings.NewReplacer(
		"${HA_NODE_IP}", config.NodeIP,
		"${HA_PEER_IP}", peerIP,
		"${HA_VIRTUAL_IP}", config.VirtualIP,
		"${HA_NETWORK_INTERFACE}", config.NetworkInterface,
		"${HA_ENDPOINT_HEARTBEAT_FILE}", ha.EndpointHeartbeatFile,
	).Replace(template)
	if strings.Contains(rendered, "${") {
		return "", errors.New("keepalived template contains an unresolved placeholder")
	}
	return rendered, nil
}
