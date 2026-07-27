// Package preflight builds direct and translated pool routes from one
// capability snapshot before any miner update is queued.
package preflight

import (
	"errors"
	"fmt"

	"github.com/block/proto-fleet/server/internal/domain/sv2"
	"github.com/block/proto-fleet/server/internal/domain/sv2/translator"
)

var ErrNonContiguousSV2Slots = errors.New("translated Stratum V2 slots must be contiguous")

type SlotAssignment struct {
	URL      string
	Username string
}

type Device struct {
	Identifier      string
	NativeStratumV2 bool
}

type EffectiveSlot struct {
	SourceIndex     int
	UsesTranslation bool
}

type DeviceRoute struct {
	DeviceIdentifier string
	Slots            []EffectiveSlot
}

type PlanResult struct {
	Devices           []DeviceRoute
	TranslatorProfile translator.Profile
}

func (r PlanResult) TranslationRequired() bool {
	return len(r.TranslatorProfile.Upstreams) > 0
}

// Plan leaves native-SV2 devices unchanged. For SV1-only devices, one
// contiguous run of SV2 slots becomes one local tProxy slot while surrounding
// SV1 slots retain their order.
func Plan(devices []Device, slots []SlotAssignment) (PlanResult, error) {
	result := PlanResult{Devices: make([]DeviceRoute, 0, len(devices))}
	if len(devices) == 0 || len(slots) == 0 {
		return result, nil
	}

	firstSV2, lastSV2 := -1, -1
	for index, slot := range slots {
		if !sv2.IsSV2URL(slot.URL) {
			continue
		}
		if firstSV2 == -1 {
			firstSV2 = index
		}
		lastSV2 = index
	}

	translationRequired := false
	for _, device := range devices {
		if firstSV2 >= 0 && !device.NativeStratumV2 {
			translationRequired = true
			break
		}
	}
	if translationRequired {
		for index := firstSV2; index <= lastSV2; index++ {
			if !sv2.IsSV2URL(slots[index].URL) {
				return PlanResult{}, ErrNonContiguousSV2Slots
			}
		}
		upstreams := make([]translator.Upstream, 0, lastSV2-firstSV2+1)
		for index := firstSV2; index <= lastSV2; index++ {
			upstreams = append(upstreams, translator.Upstream{
				URL:      slots[index].URL,
				Username: slots[index].Username,
			})
		}
		profile, err := translator.NormalizeProfile(translator.Profile{Upstreams: upstreams})
		if err != nil {
			return PlanResult{}, fmt.Errorf("build translator profile: %w", err)
		}
		result.TranslatorProfile = profile
	}

	for _, device := range devices {
		route := DeviceRoute{
			DeviceIdentifier: device.Identifier,
			Slots:            make([]EffectiveSlot, 0, len(slots)),
		}
		for index := 0; index < len(slots); index++ {
			if translationRequired && !device.NativeStratumV2 && index == firstSV2 {
				route.Slots = append(route.Slots, EffectiveSlot{
					SourceIndex:     firstSV2,
					UsesTranslation: true,
				})
				index = lastSV2
				continue
			}
			route.Slots = append(route.Slots, EffectiveSlot{SourceIndex: index})
		}
		result.Devices = append(result.Devices, route)
	}
	return result, nil
}
