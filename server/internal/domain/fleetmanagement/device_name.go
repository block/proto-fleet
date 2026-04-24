package fleetmanagement

import "strings"

// ComposeDeviceName returns the canonical human-readable name for a device,
// used anywhere we render a miner to an operator. Single source of truth —
// both the live fleet read path and the command audit read path call it so
// the two views agree.
func ComposeDeviceName(customName, manufacturer, model string) string {
	if customName != "" {
		return customName
	}
	return strings.TrimSpace(manufacturer + " " + model)
}
