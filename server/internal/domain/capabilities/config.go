package capabilities

// Config holds the configuration for the capabilities service
type Config struct {
	// CapabilitiesPath is the path to the YAML file containing miner capabilities
	CapabilitiesPath string `help:"Path to the capabilities YAML config file" default:"miner-configs/capabilities.yaml" env:"CONFIG_PATH"`
}
