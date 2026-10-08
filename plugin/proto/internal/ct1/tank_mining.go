package ct1

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func (d *Device) SetTankMining(ctx context.Context, req sdk.TankMiningRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	c := d.client
	c.mu.Lock()
	defer c.mu.Unlock()
	var wire MapResponse
	if err := c.get(ctx, "/api/v2/container/map", &wire); err != nil {
		return err
	}
	if wire.ContainerMap == nil {
		return fmt.Errorf("tank topology unavailable")
	}
	identityCounts := boardIdentityCounts(wire)
	type selector struct {
		Tank   int32 `json:"tank"`
		Module int32 `json:"module"`
	}
	modules := []selector{}
	expected := map[string]bool{}
	for _, position := range req.Positions {
		found := false
		for _, tank := range wire.ContainerMap.Tanks {
			if tank.ID != position {
				continue
			}
			if found || len(tank.Modules) == 0 {
				return fmt.Errorf("tank %d topology unavailable or ambiguous", position)
			}
			found = true
			before := len(expected)
			seen := map[int32]bool{}
			for _, module := range tank.Modules {
				if module.ID <= 0 || seen[module.ID] {
					return fmt.Errorf("tank %d module topology invalid", position)
				}
				seen[module.ID] = true
				if len(module.Slots) != boardsPerModule {
					return fmt.Errorf("tank %d module %d slot topology unavailable", position, module.ID)
				}
				moduleBoards := 0
				for _, slot := range module.Slots {
					if slot.invalid || slot.unreported || (slot.Occupied && slot.Hashboard == nil) {
						return fmt.Errorf("tank %d board topology unavailable", position)
					}
					if !slot.Occupied {
						continue
					}
					mac := normalizedBoardMAC(slot.Hashboard.MACAddress)
					if mac == "" || expected[mac] {
						return fmt.Errorf("tank %d board identity unavailable or ambiguous", position)
					}
					for _, key := range boardIdentityKeys(mac, strings.TrimSpace(slot.Hashboard.Serial)) {
						if identityCounts[key] > 1 {
							return fmt.Errorf("tank %d board identity unavailable or ambiguous", position)
						}
					}
					expected[mac] = true
					moduleBoards++
				}
				if moduleBoards > 0 {
					modules = append(modules, selector{position, module.ID})
				}
			}
			if len(expected) == before {
				return fmt.Errorf("tank %d has no addressable boards", position)
			}
		}
		if !found {
			return fmt.Errorf("tank %d no longer exists", position)
		}
	}
	if len(expected) == 0 {
		return fmt.Errorf("selected tanks have no addressable boards")
	}
	sort.Slice(modules, func(i, j int) bool {
		if modules[i].Tank != modules[j].Tank {
			return modules[i].Tank < modules[j].Tank
		}
		return modules[i].Module < modules[j].Module
	})
	action := "stop"
	if *req.Mining {
		action = "start"
	}
	var response struct {
		Results []struct {
			MAC     string `json:"macAddress"`
			Applied bool   `json:"applied"`
			Message string `json:"message"`
		} `json:"results"`
	}
	body := map[string]any{"scope": map[string]any{"modules": modules}}
	if _, err := c.request(ctx, http.MethodPost, "/api/v2/mining/scoped/"+action, body, &response); err != nil {
		return fmt.Errorf("tank mining outcome unconfirmed: %w", err)
	}
	failures := []string{}
	seen := map[string]bool{}
	for _, result := range response.Results {
		mac := normalizedBoardMAC(result.MAC)
		if !expected[mac] || seen[mac] {
			return fmt.Errorf("controller returned unexpected board results; outcome unconfirmed")
		}
		seen[mac] = true
		if !result.Applied {
			failures = append(failures, result.MAC+": "+result.Message)
		}
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("controller returned incomplete board results; outcome unconfirmed")
	}
	if len(failures) > 0 {
		return fmt.Errorf("tank mining partially applied (%d/%d boards failed): %s", len(failures), len(expected), strings.Join(failures, "; "))
	}
	return nil
}
