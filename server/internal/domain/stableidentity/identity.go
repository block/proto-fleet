// Package stableidentity defines the credential-independent miner identity used
// to recognize a device after its network endpoint changes.
package stableidentity

import (
	"strings"

	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
)

// Identity contains the stable identifiers a miner may report without
// authentication. Empty or invalid values are ignored.
type Identity struct {
	SerialNumber string
	MACAddress   string
}

// New normalizes stable identity evidence before it is compared or persisted.
func New(serialNumber, macAddress string) Identity {
	return Identity{
		SerialNumber: strings.TrimSpace(serialNumber),
		MACAddress:   networking.NormalizeMAC(macAddress),
	}
}

// Usable reports whether the identity contains any valid stable evidence.
func (i Identity) Usable() bool {
	return i.SerialNumber != "" || i.MACAddress != ""
}

// Matches reports whether the identities share at least one identifier and no
// identifier present in both conflicts.
func (i Identity) Matches(other Identity) bool {
	matched := false
	if i.MACAddress != "" && other.MACAddress != "" {
		if i.MACAddress != other.MACAddress {
			return false
		}
		matched = true
	}
	if i.SerialNumber != "" && other.SerialNumber != "" {
		if i.SerialNumber != other.SerialNumber {
			return false
		}
		matched = true
	}
	return matched
}
