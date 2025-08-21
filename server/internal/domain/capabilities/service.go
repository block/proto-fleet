package capabilities

import (
	"context"
	"fmt"
	"strings"

	capabilitiespb "github.com/btc-mining/proto-fleet/server/generated/grpc/capabilities/v1"
	pairingpb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
)

// Service provides methods for working with miner capabilities
type Service struct {
	capabilities map[string]*capabilitiespb.MinerCapabilities
}

func NewService(configPath string) (*Service, error) {
	config, err := LoadCapabilities(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load capabilities config: %w", err)
	}

	capabilities := make(map[string]*capabilitiespb.MinerCapabilities)
	for modelID, minerConfig := range config.Miners {
		capabilities[modelID] = ConvertToPbCapabilities(minerConfig)
	}

	return &Service{
		capabilities: capabilities,
	}, nil
}

// GetCapabilitiesForDevice returns the capabilities for a given device
func (s *Service) GetCapabilitiesForDevice(ctx context.Context, device *pairingpb.Device) *capabilitiespb.MinerCapabilities {
	if device == nil {
		return nil
	}

	// First try exact model match
	if caps := s.capabilities[device.Model]; caps != nil {
		return caps
	}

	// Try to construct a model ID from manufacturer and model
	modelID := normalizedModelID(device.Manufacturer, device.Model)
	if caps := s.capabilities[modelID]; caps != nil {
		return caps
	}

	// Try to find by manufacturer prefix
	for modelID, caps := range s.capabilities {
		if caps.Manufacturer == device.Manufacturer && isModelVariant(modelID, device.Manufacturer, device.Model) {
			return caps
		}
	}

	return nil
}

func normalizedModelID(manufacturer, model string) string {
	id := manufacturer + "-" + model
	id = strings.ToLower(id)
	id = strings.ReplaceAll(id, " ", "-")
	id = strings.ReplaceAll(id, "_", "-")
	return id
}

// isModelVariant checks if a model ID is a variant of a known model ID
func isModelVariant(knownModelID, manufacturer, model string) bool {
	known := strings.ToLower(knownModelID)
	modelID := normalizedModelID(manufacturer, model)

	// Exact match
	if known == modelID {
		return true
	}

	// Check if model starts with known pattern followed by delimiter or end
	if strings.HasPrefix(modelID, known) {
		remainder := modelID[len(known):]
		return remainder == "" || strings.HasPrefix(remainder, "-")
	}

	return false
}
