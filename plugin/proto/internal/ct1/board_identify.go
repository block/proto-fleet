package ct1

import (
	"context"
	"fmt"
	"strings"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// SetBoardIdentify blinks or clears the identification LED of exactly one
// board. The controller map must still place the confirmed board, under the
// confirmed module identity, at the confirmed slot; a replacement never
// inherits the request. Success requires the controller to report that it
// updated that one board.
func (d *Device) SetBoardIdentify(ctx context.Context, req sdk.BoardIdentifyRequest) error {
	if err := req.Validate(); err != nil {
		return sdk.InvalidWriteRequest(err)
	}
	mode := mdk.HashboardIdentifyLEDMode_HASHBOARD_IDENTIFY_LED_MODE_OFF
	if req.Mode == sdk.BoardIdentifyBlink {
		mode = mdk.HashboardIdentifyLEDMode_HASHBOARD_IDENTIFY_LED_MODE_BLINK
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
	identifier, err := identifyBoardSelector(mapResponse(mapWire), req.Board)
	if err != nil {
		return sdk.WriteRefused(err, sdk.WriteStageConfigurationRead, sdk.WriteReasonStaleBinding)
	}
	response, err := c.boards.SetIdentifyLED(ctx, &mdk.SetIdentifyLEDRequest{
		Scope: &mdk.BoardScope{Identifiers: []string{identifier}},
		Mode:  mode,
	})
	if err != nil {
		return fmt.Errorf("board identify outcome unconfirmed: %w", err)
	}
	if response.GetBoardsUpdated() != 1 {
		return fmt.Errorf("controller updated %d boards for one board identify request; outcome unconfirmed", response.GetBoardsUpdated())
	}
	return nil
}

// identifyBoardSelector returns the controller's own identifier for the board
// at target's slot, refusing any mismatch or ambiguous identity.
func identifyBoardSelector(wire MapResponse, target sdk.BoardIdentifyTarget) (string, error) {
	if wire.ContainerMap == nil {
		return "", fmt.Errorf("tank topology unavailable")
	}
	identities := collectTopologyIdentities(wire)
	if !identities.uniqueModule(target.Tank, target.Module, target.ModuleReportedID) {
		return "", fmt.Errorf("tank %d module %d identity changed or unavailable", target.Tank, target.Module)
	}
	var slot *Slot
	for ti := range wire.ContainerMap.Tanks {
		tank := &wire.ContainerMap.Tanks[ti]
		if tank.ID != target.Tank {
			continue
		}
		for mi := range tank.Modules {
			module := &tank.Modules[mi]
			if module.ID != target.Module {
				continue
			}
			if slot != nil || len(module.Slots) != boardsPerModule {
				return "", fmt.Errorf("tank %d module %d slot topology unavailable", target.Tank, target.Module)
			}
			slot = &module.Slots[target.Slot-1]
		}
	}
	if slot == nil {
		return "", fmt.Errorf("tank %d module %d no longer exists", target.Tank, target.Module)
	}
	if slot.unreported || slot.invalid {
		return "", fmt.Errorf("tank %d module %d slot %d topology unavailable", target.Tank, target.Module, target.Slot)
	}
	board := slot.Hashboard
	if !slot.Occupied || board == nil {
		return "", fmt.Errorf("tank %d module %d slot %d no longer has a board", target.Tank, target.Module, target.Slot)
	}
	mac := normalizedBoardMAC(board.MACAddress)
	if mac == "" || mac != normalizedBoardMAC(target.MACAddress) {
		return "", fmt.Errorf("tank %d module %d slot %d board was replaced", target.Tank, target.Module, target.Slot)
	}
	if strings.TrimSpace(board.ModuleID) != strings.TrimSpace(target.ModuleReportedID) {
		return "", fmt.Errorf("tank %d module %d identity changed or unavailable", target.Tank, target.Module)
	}
	if identities.selectors[mac] != 1 {
		return "", fmt.Errorf("board identifier ambiguous")
	}
	for _, key := range boardIdentityKeys(mac, strings.TrimSpace(board.Serial)) {
		if identities.boards[key] > 1 {
			return "", fmt.Errorf("board identity ambiguous")
		}
	}
	return board.MACAddress, nil
}
