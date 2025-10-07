package sdk

// APIVersion represents the current SDK API version
const APIVersion = "v1.0.0"

// IsCapabilitySupported checks if a capability is supported in the given capability map
func IsCapabilitySupported(caps Capabilities, capability string) bool {
	if caps == nil {
		return false
	}
	supported, exists := caps[capability]
	return exists && supported
}

// ValidateCapabilities ensures the provided capabilities include required ones
func ValidateCapabilities(required map[string]bool, caps Capabilities) error {
	for cap, required := range required {
		if required && !IsCapabilitySupported(caps, cap) {
			return NewErrUnsupportedCapability(cap)
		}
	}
	return nil
}
