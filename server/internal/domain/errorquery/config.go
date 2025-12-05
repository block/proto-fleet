package errorquery

// TestConfig holds test/development configuration for the error query service.
// This configuration is NOT for production use - it provides deterministic
// error data for UI development, integration testing, and demos.
type TestConfig struct {
	// SeedFile is the path to a YAML file containing seed error data for testing.
	// If empty, no seed data is loaded and errors are randomly generated.
	SeedFile string `help:"Path to YAML file containing seed error data (testing only)" default:"" env:"ERROR_SEED_FILE"`
}
