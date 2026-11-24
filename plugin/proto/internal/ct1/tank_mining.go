package ct1

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/grpc/status"
)

func (d *Device) SetTankMining(ctx context.Context, req sdk.TankMiningRequest) error {
	if err := req.Validate(); err != nil {
		return sdk.InvalidWriteRequest(err)
	}
	return d.setScopedMining(ctx, req.Positions, nil, req.Mining)
}
func (d *Device) SetModuleMining(ctx context.Context, req sdk.ModuleMiningRequest) error {
	if err := req.Validate(); err != nil {
		return sdk.InvalidWriteRequest(err)
	}
	positions := []int32{}
	for _, m := range req.Modules {
		if !slices.Contains(positions, m.Tank) {
			positions = append(positions, m.Tank)
		}
	}
	return d.setScopedMining(ctx, positions, req.Modules, req.Mining)
}
func (d *Device) setScopedMining(ctx context.Context, positions []int32, targets []sdk.ModuleMiningTarget, mining *bool) error {
	scopeName := "tank"
	if len(targets) > 0 {
		scopeName = "module"
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	c, err := d.verifiedWriteClient(ctx)
	if err != nil {
		return err
	}
	mapWire, err := c.boards.GetContainerMap(ctx, &mdk.GetContainerMapRequest{})
	if err != nil {
		return sdk.WriteRefused(err, sdk.WriteStageConfigurationRead, sdk.WriteReasonReadFailed)
	}
	scope, expected, err := scopedMiningScope(mapResponse(mapWire), positions, targets, scopeName)
	if err != nil {
		return sdk.WriteRefused(err, sdk.WriteStageConfigurationRead, sdk.WriteReasonStaleBinding)
	}
	var results []*mdk.BoardResult
	if *mining {
		response, rpcErr := c.mining.StartMiningScoped(ctx, &mdk.StartMiningScopedRequest{Scope: scope})
		err = rpcErr
		results = response.GetResults()
	} else {
		response, rpcErr := c.mining.StopMiningScoped(ctx, &mdk.StopMiningScopedRequest{Scope: scope})
		err = rpcErr
		results = response.GetResults()
	}
	if err != nil {
		return fmt.Errorf("%s mining outcome unconfirmed: %w", scopeName, err)
	}
	failures := []string{}
	seen := map[string]bool{}
	for _, result := range results {
		mac := normalizedBoardMAC(result.GetMacAddress())
		if expected[mac] == "" || seen[mac] {
			return fmt.Errorf("controller returned unexpected board results; outcome unconfirmed")
		}
		seen[mac] = true
		if !result.GetApplied() {
			failures = append(failures, result.GetMacAddress()+": "+result.GetMessage())
		}
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("controller returned incomplete board results; outcome unconfirmed")
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s mining partially applied (%d/%d boards failed): %s", scopeName, len(failures), len(expected), strings.Join(failures, "; "))
	}
	return nil
}

// verifiedWriteClient selects the write backend and confirms it still serves
// the paired container. Callers must hold writeMu.
func (d *Device) verifiedWriteClient(ctx context.Context) (*Client, error) {
	c, err := d.selectClient(ctx, true)
	if err != nil {
		return nil, sdk.WriteRefused(err, sdk.WriteStageBackendSelection, sdk.WriteReasonTransportFailed)
	}
	expectedSerial := strings.TrimSpace(d.info.SerialNumber)
	if expectedSerial == "" {
		return nil, sdk.WriteRefused(fmt.Errorf("container identity unavailable"), sdk.WriteStageConfigurationRead, sdk.WriteReasonStaleBinding)
	}
	// Paired endpoints were already verified during controller selection.
	if len(d.info.Endpoints) == 0 {
		info, err := c.Discover(ctx, d.info)
		if err != nil {
			reason := sdk.WriteReasonStaleBinding
			if _, rpcError := status.FromError(err); rpcError {
				reason = sdk.WriteReasonReadFailed
			}
			return nil, sdk.WriteRefused(fmt.Errorf("container identity unavailable: %w", err), sdk.WriteStageConfigurationRead, reason)
		}
		if strings.TrimSpace(info.SerialNumber) != expectedSerial {
			return nil, sdk.WriteRefused(fmt.Errorf("container identity changed"), sdk.WriteStageConfigurationRead, sdk.WriteReasonStaleBinding)
		}
	}
	return c, nil
}

func scopedMiningScope(wire MapResponse, positions []int32, targets []sdk.ModuleMiningTarget, scopeName string) (*mdk.BoardScope, map[string]string, error) {
	if wire.ContainerMap == nil {
		return nil, nil, fmt.Errorf("tank topology unavailable")
	}
	identities := collectTopologyIdentities(wire)
	for _, target := range targets {
		if !identities.uniqueModule(target.Tank, target.Module, target.ReportedID) {
			return nil, nil, fmt.Errorf("selected module identity unavailable or ambiguous")
		}
	}
	modules := []*mdk.ModuleSelector{}
	expected := map[string]string{}
	selectedModules := map[int32]map[int32]sdk.ModuleMiningTarget{}
	for _, target := range targets {
		if selectedModules[target.Tank] == nil {
			selectedModules[target.Tank] = map[int32]sdk.ModuleMiningTarget{}
		}
		selectedModules[target.Tank][target.Module] = target
	}
	for _, position := range positions {
		found := false
		for _, tank := range wire.ContainerMap.Tanks {
			if tank.ID != position {
				continue
			}
			if found || len(tank.Modules) == 0 {
				return nil, nil, fmt.Errorf("tank %d topology unavailable or ambiguous", position)
			}
			found = true
			before := len(expected)
			seen := map[int32]bool{}
			matched := 0
			for _, module := range tank.Modules {
				selected, isSelected := selectedModules[position][module.ID]
				if len(targets) > 0 {
					if !isSelected {
						continue
					}
					matched++
				}
				if module.ID <= 0 || seen[module.ID] {
					return nil, nil, fmt.Errorf("tank %d module topology invalid", position)
				}
				seen[module.ID] = true
				if len(module.Slots) != boardsPerModule {
					return nil, nil, fmt.Errorf("tank %d module %d slot topology unavailable", position, module.ID)
				}
				moduleBoards := 0
				for _, slot := range module.Slots {
					if slot.unreported {
						return nil, nil, fmt.Errorf("tank %d module %d slot topology unavailable", position, module.ID)
					}
					if slot.invalid || (slot.Occupied && slot.Hashboard == nil) || (isSelected && !slot.Occupied && slot.Hashboard != nil) {
						return nil, nil, fmt.Errorf("tank %d board topology unavailable", position)
					}
					if !slot.Occupied {
						continue
					}
					if isSelected {
						id := slot.Hashboard.ModuleID
						if strings.TrimSpace(id) == "" || id != selected.ReportedID {
							return nil, nil, fmt.Errorf("tank %d module %d identity changed or unavailable", position, module.ID)
						}
					}
					mac := normalizedBoardMAC(slot.Hashboard.MACAddress)
					if mac == "" || expected[mac] != "" {
						return nil, nil, fmt.Errorf("tank %d board identity unavailable or ambiguous", position)
					}
					if isSelected && identities.selectors[mac] > 1 {
						return nil, nil, fmt.Errorf("selected module board identifier ambiguous")
					}
					for _, key := range boardIdentityKeys(mac, strings.TrimSpace(slot.Hashboard.Serial)) {
						if identities.boards[key] > 1 {
							return nil, nil, fmt.Errorf("tank %d board identity unavailable or ambiguous", position)
						}
					}
					expected[mac] = slot.Hashboard.MACAddress
					moduleBoards++
				}
				if isSelected && moduleBoards == 0 {
					return nil, nil, fmt.Errorf("selected module has no addressable boards")
				}
				if len(targets) == 0 && moduleBoards > 0 {
					modules = append(modules, &mdk.ModuleSelector{Tank: position, Module: module.ID})
				}
			}
			if len(targets) > 0 && matched != len(selectedModules[position]) {
				return nil, nil, fmt.Errorf("selected module no longer exists")
			}
			if len(expected) == before {
				return nil, nil, fmt.Errorf("tank %d has no addressable boards", position)
			}
		}
		if !found {
			return nil, nil, fmt.Errorf("tank %d no longer exists", position)
		}
	}
	if len(expected) == 0 {
		return nil, nil, fmt.Errorf("selected %ss have no addressable boards", scopeName)
	}
	var scope *mdk.BoardScope
	if len(targets) > 0 {
		// Address only the verified boards, so a replacement entering the same
		// module position after the map read cannot inherit this write.
		identifiers := make([]string, 0, len(expected))
		for _, identifier := range expected {
			identifiers = append(identifiers, identifier)
		}
		sort.Strings(identifiers)
		scope = &mdk.BoardScope{Identifiers: identifiers}
	} else {
		sort.Slice(modules, func(i, j int) bool {
			if modules[i].Tank != modules[j].Tank {
				return modules[i].Tank < modules[j].Tank
			}
			return modules[i].Module < modules[j].Module
		})
		scope = &mdk.BoardScope{Modules: modules}
	}
	return scope, expected, nil
}
