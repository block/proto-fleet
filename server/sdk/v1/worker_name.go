package sdk

import (
	"net"
	"strings"
)

// MACWorkerName uses the full lowercase, colon-separated MAC address.
func MACWorkerName(mac string) string {
	mac = strings.TrimSpace(mac)
	address, err := net.ParseMAC(mac)
	if err != nil {
		return strings.ToLower(mac)
	}
	return address.String()
}

// DefaultWorkerName is the plugin's policy for equipment without a saved name.
// Explicitly saved names, including the empty string, take precedence.
func (caps Capabilities) DefaultWorkerName(mac string) string {
	if caps[CapabilityWorkerNameDefaultNone] {
		return ""
	}
	return MACWorkerName(mac)
}
