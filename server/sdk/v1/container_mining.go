package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
)

const CapabilityContainerMining = "container_mining"
const containerMiningPowerTarget = "power_target"

// ContainerMiningRequest is a closed controller operation, never a raw HTTP path.
// Container-wide mining retains firmware default-group and isolated-board semantics.
type ContainerMiningRequest struct {
	Action string                `json:"action"`
	Mining *bool                 `json:"mining,omitempty"`
	Power  *ContainerPowerTarget `json:"power,omitempty"`
}
type ContainerPowerTarget struct {
	Watts            float64 `json:"watts"`
	PerformanceMode  string  `json:"performance_mode,omitempty"`
	BalanceBays      *bool   `json:"balance_bays,omitempty"`
	HashOnDisconnect *bool   `json:"hash_on_disconnect,omitempty"`
}
type ContainerMiningStatus struct {
	Mining         *bool  `json:"mining"`
	OperatingState string `json:"operating_state"`
}
type ContainerPowerState struct {
	Watts            *float64 `json:"watts"`
	PerformanceMode  string   `json:"performance_mode,omitempty"`
	BalanceBays      *bool    `json:"balance_bays,omitempty"`
	HashOnDisconnect *bool    `json:"hash_on_disconnect,omitempty"`
	MinimumWatts     *float64 `json:"minimum_watts,omitempty"`
	MaximumWatts     *float64 `json:"maximum_watts,omitempty"`
}
type ContainerMiningResult struct {
	Status *ContainerMiningStatus `json:"status,omitempty"`
	Power  *ContainerPowerState   `json:"power,omitempty"`
}

func (r ContainerMiningRequest) ReadOnly() bool {
	return r.Action == "status" || r.Action == containerMiningPowerTarget
}
func (r ContainerMiningRequest) Validate() error {
	switch r.Action {
	case "status", containerMiningPowerTarget:
		if r.Mining != nil || r.Power != nil {
			return fmt.Errorf("unexpected fields on container read")
		}
	case "set_mining":
		if r.Mining == nil || r.Power != nil {
			return fmt.Errorf("explicit mining state required")
		}
	case "set_power_target":
		if r.Mining != nil || r.Power == nil {
			return fmt.Errorf("power settings required")
		}
		if err := r.Power.validate(); err != nil {
			return err
		}
		if r.Power.Watts <= 0 {
			return fmt.Errorf("positive power target required")
		}
	default:
		return fmt.Errorf("unsupported container mining operation")
	}
	return nil
}
func (p ContainerPowerTarget) validate() error {
	if math.IsNaN(p.Watts) || math.IsInf(p.Watts, 0) || p.Watts < 0 {
		return fmt.Errorf("invalid power target")
	}
	switch p.PerformanceMode {
	case "", "PERFORMANCE_MODE_UNSPECIFIED", "PERFORMANCE_MODE_MAXIMUM_HASHRATE", "PERFORMANCE_MODE_EFFICIENCY":
		return nil
	default:
		return fmt.Errorf("invalid performance mode")
	}
}
func (r ContainerMiningResult) Validate(req ContainerMiningRequest) error {
	switch req.Action {
	case "status":
		if r.Status == nil || r.Status.Mining == nil || r.Power != nil {
			return fmt.Errorf("invalid mining status result")
		}
	case containerMiningPowerTarget:
		if r.Power == nil || r.Power.Watts == nil || r.Status != nil {
			return fmt.Errorf("invalid power target result")
		}
		if err := (ContainerPowerTarget{Watts: *r.Power.Watts, PerformanceMode: r.Power.PerformanceMode}).validate(); err != nil {
			return err
		}
		for _, v := range []*float64{r.Power.MinimumWatts, r.Power.MaximumWatts} {
			if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0) {
				return fmt.Errorf("invalid power limits")
			}
		}
	case "set_mining", "set_power_target":
		if r.Status != nil || r.Power != nil {
			return fmt.Errorf("unexpected fields on container write result")
		}
	default:
		return fmt.Errorf("unsupported container mining result")
	}
	return nil
}
func decodeContainerMining(data []byte, out any) error {
	if len(data) > 4096 {
		return fmt.Errorf("container mining payload too large")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("invalid container mining payload: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("trailing container mining payload")
	}
	return nil
}
func DecodeContainerMiningRequest(data []byte) (ContainerMiningRequest, error) {
	var r ContainerMiningRequest
	if err := decodeContainerMining(data, &r); err != nil {
		return r, err
	}
	return r, r.Validate()
}
func DecodeContainerMiningResult(data []byte, req ContainerMiningRequest) (ContainerMiningResult, error) {
	var r ContainerMiningResult
	if err := decodeContainerMining(data, &r); err != nil {
		return r, err
	}
	return r, r.Validate(req)
}

// DeviceContainerMining supports container reads and writes. A successful write
// returns an empty result and nil error once the controller acknowledges it;
// this does not confirm the final physical state.
type DeviceContainerMining interface {
	ContainerMining(ctx context.Context, request ContainerMiningRequest) (ContainerMiningResult, error)
}
