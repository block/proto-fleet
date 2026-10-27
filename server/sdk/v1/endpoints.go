package sdk

import (
	"fmt"
	"slices"
	"strings"
)

// DeviceEndpointKey checks transport fields and returns the plugin's opaque
// grouping key. Fleet must scope this key by driver and network owner.
func DeviceEndpointKey(endpoints []DeviceEndpoint) (string, error) {
	if len(endpoints) == 0 {
		return "", nil
	}
	if len(endpoints) > 16 {
		return "", fmt.Errorf("device has too many endpoints")
	}
	key := endpoints[0].DeviceKey
	if strings.TrimSpace(key) == "" || len(key) > 256 {
		return "", fmt.Errorf("invalid device endpoint key")
	}
	ids := map[string]bool{}
	addresses := map[string]bool{}
	var members []string
	for _, e := range endpoints {
		if strings.TrimSpace(e.ID) == "" || len(e.ID) > 256 || ids[e.ID] || e.DeviceKey != key {
			return "", fmt.Errorf("invalid or conflicting device endpoint identity")
		}
		if e.Host == "" || e.Port <= 0 || e.Port > 65535 || e.RPCPort < 0 || e.RPCPort > 65535 || (e.URLScheme != "http" && e.URLScheme != "https") {
			return "", fmt.Errorf("invalid device endpoint")
		}
		if len(e.MemberIDs) > 0 {
			if len(e.MemberIDs) > 16 || !slices.Contains(e.MemberIDs, e.ID) {
				return "", fmt.Errorf("invalid device endpoint members")
			}
			declared := slices.Clone(e.MemberIDs)
			slices.Sort(declared)
			for i, id := range declared {
				if strings.TrimSpace(id) == "" || len(id) > 256 || (i > 0 && id == declared[i-1]) {
					return "", fmt.Errorf("invalid device endpoint members")
				}
			}
			if members != nil && !slices.Equal(members, declared) {
				return "", fmt.Errorf("conflicting device endpoint members")
			}
			members = declared
		}
		address := fmt.Sprintf("%s:%d:%d", e.Host, e.Port, e.RPCPort)
		if addresses[address] {
			return "", fmt.Errorf("duplicate device endpoint address")
		}
		ids[e.ID], addresses[address] = true, true
	}
	for id := range ids {
		if members != nil && !slices.Contains(members, id) {
			return "", fmt.Errorf("endpoint is not a declared device member")
		}
	}
	return key, nil
}
