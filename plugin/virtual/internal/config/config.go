// Package config provides configuration types and loading for the virtual miner plugin.
package config

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"time"
)

const (
	// IP allocation constants for 10.255.x.y range
	virtualIPFirstOctet  = 10
	virtualIPSecondOctet = 255
	// Skip .0 (network) and .1 (gateway) addresses
	minHostOctet = 2
	maxHostOctet = 254
	// Maximum third octet value
	maxThirdOctet = 255
	// defaultPort is the standard CGMiner API port used for virtual miners
	defaultPort = 4028

	// Default variance values
	defaultBaselineVariancePercent = 10.0
	// Temperature varies less than other metrics because it's more stable in real miners
	tempVarianceDivisor = 2.0
)

// BehaviorConfig defines runtime behavior settings for a virtual miner.
type BehaviorConfig struct {
	// HashrateVariancePercent controls hashrate fluctuation (0-100).
	HashrateVariancePercent float64 `json:"hashrate_variance_percent"`
	// TempVarianceC controls temperature fluctuation in Celsius.
	TempVarianceC float64 `json:"temp_variance_c"`
	// ErrorInjection controls simulated error conditions.
	ErrorInjection ErrorInjectionConfig `json:"error_injection"`
}

// ErrorInjectionConfig allows simulation of error conditions.
type ErrorInjectionConfig struct {
	// Enabled turns on error injection.
	Enabled bool `json:"enabled"`
	// OfflineProbability is the chance (0-1) the miner appears offline on each status call.
	OfflineProbability float64 `json:"offline_probability"`
	// DegradedBoardProbability is the chance (0-1) a hashboard appears degraded.
	DegradedBoardProbability float64 `json:"degraded_board_probability"`
}

// VirtualMinerConfig defines a single virtual miner's configuration.
type VirtualMinerConfig struct {
	// Identity
	SerialNumber string `json:"serial_number"`
	MacAddress   string `json:"mac_address"`
	Model        string `json:"model"`
	Manufacturer string `json:"manufacturer"`

	// Network (used for discovery matching)
	IPAddress string `json:"ip_address"`
	Port      int    `json:"port"`

	// Hardware configuration
	Hashboards    int `json:"hashboards"`
	ASICsPerBoard int `json:"asics_per_board"`
	FanCount      int `json:"fan_count"`

	// Baseline metrics
	BaselineHashrateTHS float64 `json:"baseline_hashrate_ths"`
	BaselinePowerW      float64 `json:"baseline_power_w"`
	BaselineTempC       float64 `json:"baseline_temp_c"`
	FanRPMMin           int     `json:"fan_rpm_min"`
	FanRPMMax           int     `json:"fan_rpm_max"`

	// Behavior
	Behavior BehaviorConfig `json:"behavior"`
}

// MinerProfile defines a template for generating miners with similar specs.
type MinerProfile struct {
	// Name identifies this profile (e.g., "s19", "t21")
	Name string `json:"name"`
	// Weight determines how often this profile is used (higher = more common)
	Weight int `json:"weight"`
	// Model name for miners using this profile
	Model string `json:"model"`
	// Manufacturer name
	Manufacturer string `json:"manufacturer"`
	// Hardware configuration
	Hashboards    int `json:"hashboards"`
	ASICsPerBoard int `json:"asics_per_board"`
	FanCount      int `json:"fan_count"`
	// Baseline metrics (actual values will vary by BaselineVariancePercent)
	BaselineHashrateTHS float64 `json:"baseline_hashrate_ths"`
	BaselinePowerW      float64 `json:"baseline_power_w"`
	BaselineTempC       float64 `json:"baseline_temp_c"`
	FanRPMMin           int     `json:"fan_rpm_min"`
	FanRPMMax           int     `json:"fan_rpm_max"`
	// Behavior settings for generated miners
	Behavior BehaviorConfig `json:"behavior"`
}

// GenerateConfig controls automatic miner generation.
type GenerateConfig struct {
	// Count is the total number of miners to generate
	Count int `json:"count"`
	// SerialPrefix is prepended to generated serial numbers (e.g., "VM" -> "VM0001")
	SerialPrefix string `json:"serial_prefix"`
	// IPStart is the starting IP address (e.g., "10.255.0.2")
	IPStart string `json:"ip_start"`
	// BaselineVariancePercent adds per-miner variance to baseline metrics (0-50)
	BaselineVariancePercent float64 `json:"baseline_variance_percent"`
	// Profiles defines miner templates to use; if empty, uses default profile
	Profiles []MinerProfile `json:"profiles"`
}

// Config is the root configuration for the virtual miner plugin.
type Config struct {
	// Generate configures automatic miner generation (optional)
	Generate *GenerateConfig `json:"generate,omitempty"`
	// Miners is a list of explicitly configured miners (added after generated ones)
	Miners []VirtualMinerConfig `json:"miners"`
}

// LoadFromFile loads configuration from a JSON file.
func LoadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Generate miners if configured
	if cfg.Generate != nil && cfg.Generate.Count > 0 {
		generated, err := generateMiners(cfg.Generate)
		if err != nil {
			return nil, fmt.Errorf("failed to generate miners: %w", err)
		}
		// Prepend generated miners, explicit miners come after
		cfg.Miners = append(generated, cfg.Miners...)
	}

	// Validate and set defaults
	for i := range cfg.Miners {
		if err := validateAndSetDefaults(&cfg.Miners[i]); err != nil {
			return nil, fmt.Errorf("miner %d (%s): %w", i, cfg.Miners[i].SerialNumber, err)
		}
	}

	return &cfg, nil
}

// generateMiners creates miners based on the generation config.
func generateMiners(gen *GenerateConfig) ([]VirtualMinerConfig, error) {
	if gen.Count <= 0 {
		return nil, nil
	}

	// Parse starting IP
	thirdOctet, fourthOctet, err := parseVirtualIP(gen.IPStart)
	if err != nil {
		return nil, fmt.Errorf("invalid ip_start: %w", err)
	}

	// Set defaults
	prefix := gen.SerialPrefix
	if prefix == "" {
		prefix = "VM"
	}
	variancePercent := gen.BaselineVariancePercent
	if variancePercent <= 0 {
		variancePercent = defaultBaselineVariancePercent
	}

	// Build profile selector
	profiles := gen.Profiles
	if len(profiles) == 0 {
		profiles = defaultProfiles()
	}
	selector := newProfileSelector(profiles)

	// Create RNG for variance
	rng := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0))

	miners := make([]VirtualMinerConfig, 0, gen.Count)
	for i := range gen.Count {
		// Select profile
		profile := selector.pick(rng)

		// Generate IP address
		ip := fmt.Sprintf("%d.%d.%d.%d", virtualIPFirstOctet, virtualIPSecondOctet, thirdOctet, fourthOctet)

		// Advance to next IP
		fourthOctet++
		if fourthOctet > maxHostOctet {
			fourthOctet = minHostOctet
			thirdOctet++
			if thirdOctet > maxThirdOctet {
				return nil, fmt.Errorf("exceeded maximum IP range after %d miners", i+1)
			}
		}

		// Generate miner with variance
		miner := VirtualMinerConfig{
			SerialNumber:        fmt.Sprintf("%s%04d", prefix, i+1),
			MacAddress:          generateMAC(rng, i),
			Model:               profile.Model,
			Manufacturer:        profile.Manufacturer,
			IPAddress:           ip,
			Port:                defaultPort,
			Hashboards:          profile.Hashboards,
			ASICsPerBoard:       profile.ASICsPerBoard,
			FanCount:            profile.FanCount,
			BaselineHashrateTHS: applyVariance(rng, profile.BaselineHashrateTHS, variancePercent),
			BaselinePowerW:      applyVariance(rng, profile.BaselinePowerW, variancePercent),
			BaselineTempC:       applyVariance(rng, profile.BaselineTempC, variancePercent/tempVarianceDivisor),
			FanRPMMin:           profile.FanRPMMin,
			FanRPMMax:           profile.FanRPMMax,
			Behavior:            profile.Behavior,
		}

		miners = append(miners, miner)
	}

	return miners, nil
}

// parseVirtualIP extracts third and fourth octets from a 10.255.x.y IP.
func parseVirtualIP(ip string) (thirdOctet, fourthOctet int, err error) {
	if ip == "" {
		return 0, minHostOctet, nil // Default to 10.255.0.2
	}

	var first, second int
	_, err = fmt.Sscanf(ip, "%d.%d.%d.%d", &first, &second, &thirdOctet, &fourthOctet)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid IP format: %s", ip)
	}

	if first != virtualIPFirstOctet || second != virtualIPSecondOctet {
		return 0, 0, fmt.Errorf("IP must be in 10.255.x.x range: %s", ip)
	}

	if fourthOctet < minHostOctet || fourthOctet > maxHostOctet {
		return 0, 0, fmt.Errorf("fourth octet must be %d-%d: %s", minHostOctet, maxHostOctet, ip)
	}

	return thirdOctet, fourthOctet, nil
}

// generateMAC creates a locally-administered MAC address.
func generateMAC(rng *rand.Rand, index int) string {
	// Use 02:VM:xx format (locally administered)
	return fmt.Sprintf("02:56:4D:%02X:%02X:%02X",
		(index>>16)&0xFF,
		(index>>8)&0xFF,
		index&0xFF,
	)
}

// applyVariance adds random variance to a baseline value.
func applyVariance(rng *rand.Rand, baseline, variancePercent float64) float64 {
	variance := baseline * (variancePercent / 100.0)
	return baseline + (rng.Float64()*2-1)*variance
}

// profileSelector handles weighted profile selection.
type profileSelector struct {
	profiles    []MinerProfile
	totalWeight int
}

func newProfileSelector(profiles []MinerProfile) *profileSelector {
	total := 0
	for i := range profiles {
		if profiles[i].Weight <= 0 {
			profiles[i].Weight = 1
		}
		total += profiles[i].Weight
	}
	return &profileSelector{profiles: profiles, totalWeight: total}
}

func (ps *profileSelector) pick(rng *rand.Rand) MinerProfile {
	if len(ps.profiles) == 1 {
		return ps.profiles[0]
	}

	r := rng.IntN(ps.totalWeight)
	for _, p := range ps.profiles {
		r -= p.Weight
		if r < 0 {
			return p
		}
	}
	return ps.profiles[0]
}

// defaultProfiles returns a set of realistic miner profiles.
func defaultProfiles() []MinerProfile {
	return []MinerProfile{
		{
			Name:                "s19-pro",
			Weight:              3,
			Model:               "S19 Pro",
			Manufacturer:        "Virtual",
			Hashboards:          3,
			ASICsPerBoard:       76,
			FanCount:            4,
			BaselineHashrateTHS: 110.0,
			BaselinePowerW:      3250.0,
			BaselineTempC:       65.0,
			FanRPMMin:           3000,
			FanRPMMax:           5000,
		},
		{
			Name:                "t21",
			Weight:              2,
			Model:               "T21",
			Manufacturer:        "Virtual",
			Hashboards:          4,
			ASICsPerBoard:       84,
			FanCount:            4,
			BaselineHashrateTHS: 190.0,
			BaselinePowerW:      3610.0,
			BaselineTempC:       62.0,
			FanRPMMin:           3500,
			FanRPMMax:           5500,
		},
		{
			Name:                "s21",
			Weight:              2,
			Model:               "S21",
			Manufacturer:        "Virtual",
			Hashboards:          3,
			ASICsPerBoard:       114,
			FanCount:            4,
			BaselineHashrateTHS: 200.0,
			BaselinePowerW:      3500.0,
			BaselineTempC:       60.0,
			FanRPMMin:           3200,
			FanRPMMax:           5200,
		},
		{
			Name:                "s19-degraded",
			Weight:              1,
			Model:               "S19 (Degraded)",
			Manufacturer:        "Virtual",
			Hashboards:          3,
			ASICsPerBoard:       76,
			FanCount:            4,
			BaselineHashrateTHS: 85.0,
			BaselinePowerW:      3100.0,
			BaselineTempC:       72.0,
			FanRPMMin:           3500,
			FanRPMMax:           5500,
			Behavior: BehaviorConfig{
				ErrorInjection: ErrorInjectionConfig{
					Enabled:                  true,
					DegradedBoardProbability: 0.3,
				},
			},
		},
	}
}

func validateAndSetDefaults(m *VirtualMinerConfig) error {
	if m.SerialNumber == "" {
		return fmt.Errorf("serial_number is required")
	}
	if m.IPAddress == "" {
		return fmt.Errorf("ip_address is required")
	}

	// Set defaults
	if m.Port == 0 {
		m.Port = defaultPort
	}
	if m.Model == "" {
		m.Model = "Miner"
	}
	if m.Manufacturer == "" {
		m.Manufacturer = "Virtual"
	}
	if m.Hashboards == 0 {
		m.Hashboards = 3
	}
	if m.ASICsPerBoard == 0 {
		m.ASICsPerBoard = 76
	}
	if m.FanCount == 0 {
		m.FanCount = 4
	}
	if m.BaselineHashrateTHS == 0 {
		m.BaselineHashrateTHS = 100.0
	}
	if m.BaselinePowerW == 0 {
		m.BaselinePowerW = 3000.0
	}
	if m.BaselineTempC == 0 {
		m.BaselineTempC = 65.0
	}
	if m.FanRPMMin == 0 {
		m.FanRPMMin = 3000
	}
	if m.FanRPMMax == 0 {
		m.FanRPMMax = 5000
	}
	if m.Behavior.HashrateVariancePercent == 0 {
		m.Behavior.HashrateVariancePercent = 5.0
	}
	if m.Behavior.TempVarianceC == 0 {
		m.Behavior.TempVarianceC = 3.0
	}

	return nil
}
