package sdk

import (
	"context"
	"fmt"
)

const CapabilityTankMining = "tank_mining"

// TankMiningRequest selects whole tanks at the owning controller. Empty scope
// must never be interpreted as container-wide mining intent.
type TankMiningRequest struct {
	Positions []int32 `json:"positions"`
	Mining    *bool   `json:"mining"`
}

func (r TankMiningRequest) Validate() error {
	if r.Mining == nil || len(r.Positions) == 0 || len(r.Positions) > 100 {
		return fmt.Errorf("explicit mining state and 1–100 tank positions required")
	}
	seen := map[int32]bool{}
	for _, p := range r.Positions {
		if p <= 0 || seen[p] {
			return fmt.Errorf("tank positions must be positive and unique")
		}
		seen[p] = true
	}
	return nil
}

type DeviceTankMiner interface {
	SetTankMining(ctx context.Context, request TankMiningRequest) error
}
