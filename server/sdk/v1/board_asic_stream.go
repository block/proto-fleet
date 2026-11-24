package sdk

import (
	"context"
	"time"
)

const CapabilityBoardASICStream = "board_asic_stream"

// BoardASICPosition places one ASIC on its board.
type BoardASICPosition struct {
	Index      int32 `json:"index"`
	StackID    int32 `json:"stack_id"`
	StackIndex int32 `json:"stack_index"`
	X          int32 `json:"x"`
	Y          int32 `json:"y"`
}

type BoardASICLayout struct {
	StackCount    int32               `json:"stack_count"`
	ASICsPerStack int32               `json:"asics_per_stack"`
	TotalASICs    int32               `json:"total_asics"`
	Positions     []BoardASICPosition `json:"positions"`
}

// BoardASICReading is one ASIC's readings at the frame's observation time.
type BoardASICReading struct {
	Index           int32   `json:"index"`
	TemperatureC    float64 `json:"temperature_c"`
	HashrateGHs     float64 `json:"hashrate_ghs"`
	VoltageV        float64 `json:"voltage_v"`
	FrequencyMHz    float64 `json:"frequency_mhz"`
	PassRatePercent float64 `json:"pass_rate_percent"`
	// Nil when the controller did not report thermal quarantine.
	ThermalQuarantined *bool `json:"thermal_quarantined,omitempty"`
}

// BoardASICFrame is one sample of a board's ASIC readings. Layout is set on
// the first frame and whenever the board's topology changes.
type BoardASICFrame struct {
	MACAddress string             `json:"mac_address"`
	ObservedAt time.Time          `json:"observed_at"`
	Layout     *BoardASICLayout   `json:"layout,omitempty"`
	ASICs      []BoardASICReading `json:"asics"`
}

// DeviceBoardASICStreamer follows the live ASIC readings of exactly one
// verified board. It calls send for each frame and returns when ctx ends, the
// controller ends the stream, or send fails. A board that no longer matches
// the target must fail before any frame is sent.
type DeviceBoardASICStreamer interface {
	StreamBoardASICReadings(ctx context.Context, board BoardIdentifyTarget, send func(BoardASICFrame) error) error
}
