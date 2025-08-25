package capabilities

import (
	"context"
	"fmt"
	"strings"

	capabilitiespb "github.com/btc-mining/proto-fleet/server/generated/grpc/capabilities/v1"
	pairingpb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
)

// ModelID represents a unique identifier for a miner model
type ModelID string

func NewModelID(id string) ModelID {
	return ModelID(id)
}

func NewModelIDFromDeviceDetails(manufacturer, model string) ModelID {
	return NewModelID(normalizedModelID(manufacturer, model))
}

// Service provides methods for working with miner capabilities
type Service struct {
	capabilities map[ModelID]*capabilitiespb.MinerCapabilities
}

func NewService(config Config) (*Service, error) {
	capabilities, err := LoadCapabilities(config.CapabilitiesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load capabilities config: %w", err)
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
	if caps := s.capabilities[NewModelID(device.Model)]; caps != nil {
		return caps
	}

	// Try to construct a model ID from manufacturer and model
	if caps := s.capabilities[NewModelIDFromDeviceDetails(device.Manufacturer, device.Model)]; caps != nil {
		return caps
	}

	// Try to find by manufacturer prefix
	for modelID, caps := range s.capabilities {
		if caps.Manufacturer == device.Manufacturer && isModelVariant(string(modelID), device.Manufacturer, device.Model) {
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
