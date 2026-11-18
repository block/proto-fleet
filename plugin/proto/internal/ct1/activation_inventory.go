package ct1

import (
	"fmt"
	"strings"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func activationInventory(wire *mdk.GetContainerMapResponse, at time.Time) (*sdk.MiningGroupInventory, error) {
	out := &sdk.MiningGroupInventory{ObservedAt: at, Boards: []sdk.HashboardIdentityClaim{}}
	if wire.GetContainerMap() == nil {
		return nil, fmt.Errorf("hashboard inventory is unavailable")
	}
	for _, tank := range wire.ContainerMap.Tanks {
		for _, module := range tank.GetModules() {
			for _, slot := range module.GetSlots() {
				if slot == nil || slot.GetOccupied() != (slot.GetHashboard() != nil) {
					return nil, fmt.Errorf("hashboard inventory contains an unidentified board")
				}
				if board := slot.GetHashboard(); board != nil {
					out.Boards = append(out.Boards, sdk.HashboardIdentityClaim{MACAddress: normalizedBoardMAC(board.MacAddress), Serial: strings.TrimSpace(board.Serial)})
				}
			}
		}
	}
	return out, out.Validate()
}
