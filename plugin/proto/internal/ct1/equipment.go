package ct1

import (
	"strings"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

type fanResponse struct {
	ComponentID  string   `json:"componentId"`
	SpeedPercent *float64 `json:"speedPercent"`
	PowerW       *float64 `json:"powerW"`
	Direction    *string  `json:"direction"`
	AlarmActive  *bool    `json:"alarmActive"`
}

func (p plantResponse) fanObservations(now time.Time) []sdk.ContainerFan {
	if p.Timestamp.IsZero() || p.Timestamp.After(now.Add(clockSkewAllowance)) {
		return nil
	}
	fans := make([]sdk.ContainerFan, 0, len(p.CoolingFans))
	seen := map[string]bool{}
	for _, f := range p.CoolingFans {
		if strings.TrimSpace(f.ComponentID) == "" || seen[f.ComponentID] {
			continue
		}
		seen[f.ComponentID] = true
		if !nonnegative(f.SpeedPercent) || *f.SpeedPercent > 100 {
			f.SpeedPercent = nil
		}
		if !nonnegative(f.PowerW) {
			f.PowerW = nil
		}
		fans = append(fans, sdk.ContainerFan{ComponentID: f.ComponentID, ObservedAt: p.Timestamp, SpeedPercent: f.SpeedPercent, PowerW: f.PowerW, Direction: f.Direction, AlarmActive: f.AlarmActive})
	}
	return fans
}

type controlsResponse struct {
	Supported *bool `json:"supported"`
	Controls  []struct {
		Name       string `json:"name"`
		Enabled    bool   `json:"enabled"`
		StateKnown bool   `json:"stateKnown"`
	} `json:"controls"`
	Setpoints                    []sdk.ContainerSetpoint `json:"setpoints"`
	MainCircuitBreakerSupported  *bool                   `json:"mainCircuitBreakerSupported"`
	MainCircuitBreakerClosed     *bool                   `json:"mainCircuitBreakerClosed"`
	AlarmResetSupported          *bool                   `json:"alarmResetSupported"`
	AlarmMuteSupported           *bool                   `json:"alarmMuteSupported"`
	DryCoolerFanReverseSupported *bool                   `json:"dryCoolerFanReverseSupported"`
}

func (w controlsResponse) observation(now time.Time) *sdk.ContainerControls {
	if w.Supported == nil && len(w.Controls) == 0 && len(w.Setpoints) == 0 && w.MainCircuitBreakerSupported == nil &&
		w.MainCircuitBreakerClosed == nil && w.AlarmResetSupported == nil &&
		w.AlarmMuteSupported == nil && w.DryCoolerFanReverseSupported == nil {
		return nil
	}
	out := &sdk.ContainerControls{ObservedAt: now, Supported: w.Supported, MainCircuitBreakerSupported: w.MainCircuitBreakerSupported, MainCircuitBreakerClosed: w.MainCircuitBreakerClosed, AlarmResetSupported: w.AlarmResetSupported, AlarmMuteSupported: w.AlarmMuteSupported, DryCoolerFanReverseSupported: w.DryCoolerFanReverseSupported}
	seen := map[string]bool{}
	for _, c := range w.Controls {
		if strings.TrimSpace(c.Name) == "" || seen[c.Name] {
			continue
		}
		seen[c.Name] = true
		var enabled *bool
		if c.StateKnown {
			enabled = &c.Enabled
		}
		out.Controls = append(out.Controls, sdk.ContainerControl{Name: c.Name, Enabled: enabled})
	}
	seen = map[string]bool{}
	for _, s := range w.Setpoints {
		if strings.TrimSpace(s.Name) == "" || seen[s.Name] {
			continue
		}
		seen[s.Name] = true
		if !finite(s.Value) {
			s.Value = nil
		}
		out.Setpoints = append(out.Setpoints, s)
	}
	return out
}
