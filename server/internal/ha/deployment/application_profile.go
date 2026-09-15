package deployment

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

var fleetDeploymentEnvironmentKeys = []string{
	"DD_API_KEY",
	"DD_ENV",
	"DD_SITE",
	"ENABLE_BETA_ALERTS",
	"ENABLE_SYSTEM_MONITORING",
	"ENABLE_TRACING",
	"FLEET_TELEMETRY_SAMPLE_RATE",
	"FLEET_TELEMETRY_TRUST_INCOMING_TRACES",
}

type fleetApplicationProfile map[string]string

func fleetApplicationEnvironment(lookup func(string) (string, bool)) (fleetApplicationProfile, error) {
	values := make(map[string]string, len(fleetDeploymentEnvironmentKeys))
	for _, key := range fleetDeploymentEnvironmentKeys {
		value, ok := lookup(key)
		if !ok {
			continue
		}
		if !envLine.MatchString(key + "=" + value) {
			return nil, fmt.Errorf("%s contains an unsupported value", key)
		}
		values[key] = value
	}
	return fleetApplicationProfileFromValues(values, true)
}

func captureFleetApplicationEnvironment() (fleetApplicationProfile, error) {
	profile, profileErr := fleetApplicationEnvironment(os.LookupEnv)
	unsetErr := os.Unsetenv("DD_API_KEY")
	if profileErr != nil {
		return nil, profileErr
	}
	if unsetErr != nil {
		return nil, fmt.Errorf("clear DD_API_KEY after capture: %w", unsetErr)
	}
	return profile, nil
}

func loadFleetApplicationProfileFile(path string, defaultBetaAlerts bool) (fleetApplicationProfile, error) {
	values, err := loadFleetEnvironment(path)
	if err != nil {
		return nil, fmt.Errorf("Fleet environment rejected: %w", err)
	}
	return fleetApplicationProfileFromValues(values, defaultBetaAlerts)
}

func parseFleetDeploymentEnvironment(contents []byte) (fleetApplicationProfile, error) {
	values, err := parseFleetEnvironment(strings.NewReader(string(contents)))
	if err != nil {
		return nil, fmt.Errorf("Fleet environment %w", err)
	}
	return fleetApplicationProfileFromValues(values, true)
}

func fleetApplicationProfileFromValues(values map[string]string, defaultBetaAlerts bool) (fleetApplicationProfile, error) {
	profile := make(fleetApplicationProfile, len(fleetDeploymentEnvironmentKeys))
	for _, key := range fleetDeploymentEnvironmentKeys {
		if value, ok := values[key]; ok {
			profile[key] = value
		}
	}
	betaAlerts, err := fleetFeatureFlagOrDefault(profile, "ENABLE_BETA_ALERTS", defaultBetaAlerts)
	if err != nil {
		return nil, err
	}
	systemMonitoring, err := fleetFeatureFlagOrDefault(profile, "ENABLE_SYSTEM_MONITORING", false)
	if err != nil {
		return nil, err
	}
	tracing, err := fleetFeatureFlagOrDefault(profile, "ENABLE_TRACING", false)
	if err != nil {
		return nil, err
	}
	if !tracing {
		delete(profile, "DD_API_KEY")
	}
	if systemMonitoring && !betaAlerts {
		return nil, errors.New("ENABLE_SYSTEM_MONITORING=true requires ENABLE_BETA_ALERTS=true")
	}
	if site := profile["DD_SITE"]; site != "" && !validDatadogSite(site) {
		return nil, errors.New("DD_SITE must be an official Datadog site")
	}
	if tracing && profile["DD_API_KEY"] == "" {
		return nil, errors.New("ENABLE_TRACING=true requires DD_API_KEY")
	}
	if tracing {
		if value, ok := profile["FLEET_TELEMETRY_SAMPLE_RATE"]; ok {
			sampleRate, parseErr := strconv.ParseFloat(value, 64)
			if parseErr != nil || math.IsNaN(sampleRate) || sampleRate < 0 || sampleRate > 1 {
				return nil, errors.New("FLEET_TELEMETRY_SAMPLE_RATE must be a number from 0.0 to 1.0")
			}
		}
		if _, ok := profile["FLEET_TELEMETRY_TRUST_INCOMING_TRACES"]; ok {
			trustIncoming, parseErr := fleetFeatureFlagOrDefault(profile, "FLEET_TELEMETRY_TRUST_INCOMING_TRACES", false)
			if parseErr != nil {
				return nil, parseErr
			}
			profile["FLEET_TELEMETRY_TRUST_INCOMING_TRACES"] = strconv.FormatBool(trustIncoming)
		}
	}
	profile["ENABLE_BETA_ALERTS"] = strconv.FormatBool(betaAlerts)
	profile["ENABLE_SYSTEM_MONITORING"] = strconv.FormatBool(systemMonitoring)
	profile["ENABLE_TRACING"] = strconv.FormatBool(tracing)
	return profile, nil
}

func validDatadogSite(site string) bool {
	switch site {
	case "datadoghq.com",
		"us3.datadoghq.com",
		"us5.datadoghq.com",
		"datadoghq.eu",
		"ap1.datadoghq.com",
		"ap2.datadoghq.com",
		"uk1.datadoghq.com",
		"ddog-gov.com",
		"us2.ddog-gov.com":
		return true
	default:
		return false
	}
}

func fleetFeatureFlagOrDefault(values map[string]string, name string, fallback bool) (bool, error) {
	value, ok := values[name]
	if !ok {
		return fallback, nil
	}
	switch strings.ToLower(value) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be true or false", name)
	}
}

func renderFleetDeploymentEnvironment(profile fleetApplicationProfile) []byte {
	var environment strings.Builder
	for _, key := range fleetDeploymentEnvironmentKeys {
		value, ok := profile[key]
		if ok {
			fmt.Fprintf(&environment, "%s=%s\n", key, value)
		}
	}
	return []byte(environment.String())
}

func (profile fleetApplicationProfile) sidecars() []string {
	var services []string
	if profile.enabled("ENABLE_BETA_ALERTS") {
		services = append(services, "grafana")
	}
	if profile.enabled("ENABLE_TRACING") {
		services = append(services, "otel-collector")
	}
	return services
}

func (profile fleetApplicationProfile) enabled(key string) bool {
	return profile[key] == "true"
}
