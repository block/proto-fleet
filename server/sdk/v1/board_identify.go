package sdk

import (
	"context"
	"fmt"
	"strings"
)

const CapabilityBoardIdentify = "board_identify"

// BoardIdentifyMode is the requested hashboard identification LED mode.
type BoardIdentifyMode string

const (
	BoardIdentifyBlink BoardIdentifyMode = "blink"
	BoardIdentifyOff   BoardIdentifyMode = "off"
)

// BoardIdentifyTarget binds one physical slot to the identities Fleet observed
// there. A replacement board or module at the same position must never inherit
// a queued identify request.
type BoardIdentifyTarget struct {
	Tank             int32  `json:"tank"`
	Module           int32  `json:"module"`
	Slot             int32  `json:"slot"`
	ModuleReportedID string `json:"module_reported_id"`
	MACAddress       string `json:"mac_address"`
}

func (t BoardIdentifyTarget) Validate() error {
	if t.Tank <= 0 || t.Module <= 0 || t.Slot < 1 || t.Slot > CT1HashboardsPerModule {
		return fmt.Errorf("positive tank, module, and slot positions required")
	}
	if strings.TrimSpace(t.ModuleReportedID) == "" || len(t.ModuleReportedID) > 255 {
		return fmt.Errorf("known module identity required")
	}
	if mac := strings.TrimSpace(t.MACAddress); mac == "" || len(mac) > 64 {
		return fmt.Errorf("known board MAC address required")
	}
	return nil
}

type BoardIdentifyRequest struct {
	Board BoardIdentifyTarget `json:"board"`
	Mode  BoardIdentifyMode   `json:"mode"`
}

func (r BoardIdentifyRequest) Validate() error {
	if r.Mode != BoardIdentifyBlink && r.Mode != BoardIdentifyOff {
		return fmt.Errorf("identify mode must be blink or off")
	}
	return r.Board.Validate()
}

// DeviceBoardIdentifier sets the identification LED of exactly one verified
// board. Implementations must return nil only when the controller confirms it
// updated that board.
type DeviceBoardIdentifier interface {
	SetBoardIdentify(ctx context.Context, req BoardIdentifyRequest) error
}
