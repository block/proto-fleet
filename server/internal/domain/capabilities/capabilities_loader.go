package capabilities

import (
	"strings"

	"gopkg.in/yaml.v3"

	"buf.build/go/protovalidate"
	files "github.com/btc-mining/proto-fleet/server"
	capabilitiespb "github.com/btc-mining/proto-fleet/server/generated/grpc/capabilities/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
)

type MinerCapabilities struct {
	Manufacturer   string               `yaml:"manufacturer"`
	Authentication AuthenticationConfig `yaml:"authentication"`
	Commands       CommandsConfig       `yaml:"commands"`
	Telemetry      TelemetryConfig      `yaml:"telemetry"`
	Firmware       FirmwareConfig       `yaml:"firmware"`
}

type AuthenticationConfig struct {
	SupportedMethods []string `yaml:"supportedMethods"`
}

type CommandsConfig struct {
	RebootSupported        bool     `yaml:"rebootSupported"`
	MiningStartSupported   bool     `yaml:"miningStartSupported"`
	MiningStopSupported    bool     `yaml:"miningStopSupported"`
	LedBlinkSupported      bool     `yaml:"ledBlinkSupported"`
	FactoryResetSupported  bool     `yaml:"factoryResetSupported"`
	CoolingModeSupported   bool     `yaml:"coolingModeSupported"`
	CoolingModesAvailable  []string `yaml:"coolingModesAvailable"`
	PoolSwitchingSupported bool     `yaml:"poolSwitchingSupported"`
	PoolMaxCount           int32    `yaml:"poolMaxCount"`
	PoolPrioritySupported  bool     `yaml:"poolPrioritySupported"`
	LogsDownloadSupported  bool     `yaml:"logsDownloadSupported"`
}

type TelemetryConfig struct {
	RealtimeTelemetrySupported        bool  `yaml:"realtimeTelemetrySupported"`
	HistoricalDataSupported           bool  `yaml:"historicalDataSupported"`
	HashrateReported                  bool  `yaml:"hashrateReported"`
	PowerUsageReported                bool  `yaml:"powerUsageReported"`
	TemperatureReported               bool  `yaml:"temperatureReported"`
	FanSpeedReported                  bool  `yaml:"fanSpeedReported"`
	EfficiencyReported                bool  `yaml:"efficiencyReported"`
	UptimeReported                    bool  `yaml:"uptimeReported"`
	ErrorCountReported                bool  `yaml:"errorCountReported"`
	MinerStatusReported               bool  `yaml:"minerStatusReported"`
	PoolStatsReported                 bool  `yaml:"poolStatsReported"`
	PerChipStatsReported              bool  `yaml:"perChipStatsReported"`
	PerBoardStatsReported             bool  `yaml:"perBoardStatsReported"`
	PsuStatsReported                  bool  `yaml:"psuStatsReported"`
	PollingIntervalSecondsRecommended int32 `yaml:"pollingIntervalSecondsRecommended"`
}

type FirmwareConfig struct {
	OtaUpdateSupported    bool `yaml:"otaUpdateSupported"`
	ManualUploadSupported bool `yaml:"manualUploadSupported"`
}

func LoadCapabilities(capabilitiesPath string) (map[ModelID]*capabilitiespb.MinerCapabilities, error) {
	data, err := files.MinerConfigs.ReadFile(capabilitiesPath)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to read config file: %v", err)
	}

	var minerMap map[string]MinerCapabilities
	if err := yaml.Unmarshal(data, &minerMap); err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to unmarshal capabilities: %v", err)
	}

	if len(minerMap) == 0 {
		return nil, fleeterror.NewInternalError("no miners defined in capabilities")
	}

	minerCapabilities := make(map[ModelID]*capabilitiespb.MinerCapabilities)

	validator, err := protovalidate.New()
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create validator: %v", err)
	}

	for minerName, minerConfig := range minerMap {
		pbCapabilities, err := convertToPbCapabilities(minerConfig)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to convert capabilities to protobuf: %v", err)
		}

		if err := validator.Validate(pbCapabilities); err != nil {
			return nil, fleeterror.NewInternalErrorf("miner %s has invalid configuration: %v", minerName, err)
		}

		minerCapabilities[NewModelID(minerName)] = pbCapabilities
	}

	return minerCapabilities, nil
}

func convertToPbCapabilities(capabilities MinerCapabilities) (*capabilitiespb.MinerCapabilities, error) {
	authMethods := make([]capabilitiespb.AuthenticationMethod, 0, len(capabilities.Authentication.SupportedMethods))
	for _, method := range capabilities.Authentication.SupportedMethods {
		switch strings.ToLower(method) {
		case "basic":
			authMethods = append(authMethods, capabilitiespb.AuthenticationMethod_AUTHENTICATION_METHOD_BASIC)
		case "asymmetric", "asymmetrickey":
			authMethods = append(authMethods, capabilitiespb.AuthenticationMethod_AUTHENTICATION_METHOD_ASYMMETRIC_KEY)
		default:
			return nil, fleeterror.NewInternalErrorf("invalid authentication method: %s", method)
		}
	}

	var auth *capabilitiespb.AuthenticationCapabilities
	if len(authMethods) > 0 {
		auth = &capabilitiespb.AuthenticationCapabilities{
			SupportedMethods: authMethods,
		}
	}

	return &capabilitiespb.MinerCapabilities{
		Manufacturer:   capabilities.Manufacturer,
		Authentication: auth,
		Commands: &capabilitiespb.CommandCapabilities{
			RebootSupported:        capabilities.Commands.RebootSupported,
			MiningStartSupported:   capabilities.Commands.MiningStartSupported,
			MiningStopSupported:    capabilities.Commands.MiningStopSupported,
			LedBlinkSupported:      capabilities.Commands.LedBlinkSupported,
			FactoryResetSupported:  capabilities.Commands.FactoryResetSupported,
			CoolingModeSupported:   capabilities.Commands.CoolingModeSupported,
			CoolingModesAvailable:  capabilities.Commands.CoolingModesAvailable,
			PoolSwitchingSupported: capabilities.Commands.PoolSwitchingSupported,
			PoolMaxCount:           capabilities.Commands.PoolMaxCount,
			PoolPrioritySupported:  capabilities.Commands.PoolPrioritySupported,
			LogsDownloadSupported:  capabilities.Commands.LogsDownloadSupported,
		},
		Telemetry: &capabilitiespb.TelemetryCapabilities{
			RealtimeTelemetrySupported:        capabilities.Telemetry.RealtimeTelemetrySupported,
			HistoricalDataSupported:           capabilities.Telemetry.HistoricalDataSupported,
			HashrateReported:                  capabilities.Telemetry.HashrateReported,
			PowerUsageReported:                capabilities.Telemetry.PowerUsageReported,
			TemperatureReported:               capabilities.Telemetry.TemperatureReported,
			FanSpeedReported:                  capabilities.Telemetry.FanSpeedReported,
			EfficiencyReported:                capabilities.Telemetry.EfficiencyReported,
			UptimeReported:                    capabilities.Telemetry.UptimeReported,
			ErrorCountReported:                capabilities.Telemetry.ErrorCountReported,
			MinerStatusReported:               capabilities.Telemetry.MinerStatusReported,
			PoolStatsReported:                 capabilities.Telemetry.PoolStatsReported,
			PerChipStatsReported:              capabilities.Telemetry.PerChipStatsReported,
			PerBoardStatsReported:             capabilities.Telemetry.PerBoardStatsReported,
			PsuStatsReported:                  capabilities.Telemetry.PsuStatsReported,
			PollingIntervalSecondsRecommended: capabilities.Telemetry.PollingIntervalSecondsRecommended,
		},
		Firmware: &capabilitiespb.FirmwareCapabilities{
			OtaUpdateSupported:    capabilities.Firmware.OtaUpdateSupported,
			ManualUploadSupported: capabilities.Firmware.ManualUploadSupported,
		},
	}, nil
}
