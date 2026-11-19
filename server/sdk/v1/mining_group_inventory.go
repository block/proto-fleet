package sdk

import (
	"fmt"
	"net"
	"time"
)

// MiningGroupInventory lists every board in the controller map, including
// retained inventory and boards without a confirmed physical position.
type MiningGroupInventory struct {
	Boards     []HashboardIdentityClaim `json:"boards"`
	ObservedAt time.Time                `json:"observed_at"`
}

func (v MiningGroupInventory) Validate() error {
	if v.ObservedAt.IsZero() || v.Boards == nil || len(v.Boards) > 288 {
		return fmt.Errorf("hashboard inventory is unavailable")
	}
	macs, serials := map[string]bool{}, map[string]bool{}
	for _, board := range v.Boards {
		mac, err := net.ParseMAC(board.MACAddress)
		if err != nil || len(mac) != 6 || mac.String() != board.MACAddress || board.Serial == "" || !SafeHashboardIdentity(board.Serial) {
			return fmt.Errorf("hashboard inventory has a missing or invalid identity")
		}
		if macs[board.MACAddress] || serials[board.Serial] {
			return fmt.Errorf("hashboard inventory has duplicate identities")
		}
		macs[board.MACAddress], serials[board.Serial] = true, true
	}
	return nil
}
