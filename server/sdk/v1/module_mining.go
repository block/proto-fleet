package sdk

import (
	"context"
	"fmt"
	"strings"
)

const CapabilityModuleMining = "module_mining"

// ModuleMiningTarget binds a physical selector to the identity observed by Fleet.
// A replacement at the same position must never inherit a queued selection.
type ModuleMiningTarget struct {
	Tank       int32  `json:"tank"`
	Module     int32  `json:"module"`
	ReportedID string `json:"reported_id"`
}

type ModuleMiningRequest struct {
	Modules []ModuleMiningTarget `json:"modules"`
	Mining  *bool                `json:"mining"`
}

func (r ModuleMiningRequest) Validate() error {
	if r.Mining == nil || len(r.Modules) == 0 || len(r.Modules) > 100 {
		return fmt.Errorf("explicit mining state and 1–100 modules required")
	}
	seen := map[[2]int32]bool{}
	identities := map[string]bool{}
	for _, m := range r.Modules {
		key := [2]int32{m.Tank, m.Module}
		if m.Tank <= 0 || m.Module <= 0 || strings.TrimSpace(m.ReportedID) == "" || len(m.ReportedID) > 255 || seen[key] || identities[m.ReportedID] {
			return fmt.Errorf("positive unique module positions and known module identity required")
		}
		seen[key] = true
		identities[m.ReportedID] = true
	}
	return nil
}

type DeviceModuleMiner interface {
	SetModuleMining(ctx context.Context, req ModuleMiningRequest) error
}
