package ct1

import (
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Plant values are firmware aggregates. They must not be summed from boards:
// cooling and other facility loads are absent from board measurements.
type plantResponse struct {
	CoolingFans []fanResponse `json:"coolingFans"`
	Timestamp   time.Time     `json:"timestamp"`
	Plant       *plantFields  `json:"plant"`
}

type plantFields struct {
	DryCoolerInletTemperatureC *float64 `json:"dryCoolerInletTemperatureC"`
	TotalPowerW                *float64 `json:"totalPowerW"`
	FacilityPowerW             *float64 `json:"facilityPowerW"`
	PUE                        *float64 `json:"pue"`
	InletTemperatureC          *float64 `json:"inletTemperatureC"`
	OutletTemperatureC         *float64 `json:"outletTemperatureC"`
	AlarmActive                *bool    `json:"alarmActive"`
	CriticalAlarmActive        *bool    `json:"criticalAlarmActive"`
	BuzzerActive               *bool    `json:"buzzerActive"`
}

// plantSampleFresh is the one freshness rule for a plant snapshot. Telemetry
// and error polling both use it, so they cannot disagree on whether a
// snapshot is current.
func plantSampleFresh(sampled, now time.Time) bool {
	return !sampled.IsZero() && now.Sub(sampled) <= healthMaxAge && sampled.Sub(now) <= clockSkewAllowance
}

func (p plantResponse) observation(now time.Time) *sdk.ContainerPlant {
	if p.Plant == nil || !plantSampleFresh(p.Timestamp, now) {
		return nil
	}
	return &sdk.ContainerPlant{DryCoolerInletTemperatureC: p.Plant.DryCoolerInletTemperatureC, ObservedAt: p.Timestamp, TotalPowerW: p.Plant.TotalPowerW, FacilityPowerW: p.Plant.FacilityPowerW, PUE: p.Plant.PUE, InletTemperatureC: p.Plant.InletTemperatureC, OutletTemperatureC: p.Plant.OutletTemperatureC, AlarmActive: p.Plant.AlarmActive, CriticalAlarmActive: p.Plant.CriticalAlarmActive, BuzzerActive: p.Plant.BuzzerActive}
}
