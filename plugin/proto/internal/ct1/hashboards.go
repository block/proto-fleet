package ct1

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func normalizedBoardMAC(value string) string {
	mac := strings.ToLower(strings.TrimSpace(value))
	if parsed, err := net.ParseMAC(mac); err == nil {
		return parsed.String()
	}
	return mac
}

// UnmarshalJSON retains every bulk-response field, including firmware additions, without
// interpreting raw diagnostic payloads as normalized current measurements.
func (b *Board) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode raw hashboard: %w", err)
	}
	// Retain safely decoded identity claims even if diagnostics are malformed.
	// Other slots claiming this identity must also be considered ambiguous.
	*b = Board{raw: raw}
	var mac, serial string
	if err := json.Unmarshal(raw["macAddress"], &mac); err == nil && sdk.ValidateHashboardJSON(raw["macAddress"]) == nil {
		b.MACAddress = mac
	}
	if err := json.Unmarshal(raw["serial"], &serial); err == nil && sdk.ValidateHashboardJSON(raw["serial"]) == nil {
		b.Serial = serial
	}
	if err := sdk.ValidateHashboardJSON(data); err != nil {
		return err
	}
	type wireBoard Board
	var wire wireBoard
	if err := json.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("decode hashboard: %w", err)
	}
	*b = Board(wire)
	b.raw = raw
	return nil
}

// UnmarshalJSON contains malformed board/occupancy data within its slot.
func (s *Slot) UnmarshalJSON(data []byte) error {
	var wire struct {
		Occupied  json.RawMessage `json:"occupied"`
		Hashboard json.RawMessage `json:"hashboard"`
	}
	*s = Slot{}
	if err := json.Unmarshal(data, &wire); err != nil {
		s.invalid = true
		return nil //nolint:nilerr // malformed slot becomes unavailable without aborting the controller map
	}
	s.unreported = len(wire.Occupied) == 0 || string(wire.Occupied) == "null"
	if !s.unreported {
		if err := json.Unmarshal(wire.Occupied, &s.Occupied); err != nil {
			s.invalid = true
		}
	}
	// Decode identity even when occupancy itself is missing, false or malformed.
	if len(wire.Hashboard) > 0 && string(wire.Hashboard) != "null" {
		s.Hashboard = &Board{}
		if err := json.Unmarshal(wire.Hashboard, s.Hashboard); err != nil {
			s.invalid = true
		}
	}
	return nil
}

func hashboardObservations(slots []Slot, now time.Time) []sdk.ContainerHashboard {
	out := make([]sdk.ContainerHashboard, boardsPerModule)
	for i := range out {
		position := int32(i + 1) //nolint:gosec // fixed three-slot array
		if len(slots) > boardsPerModule {
			out[i] = unavailableBoard(position, sdk.HashboardAmbiguousSlot, slots[i].Hashboard)
			if i == 0 {
				for _, extra := range slots[boardsPerModule:] {
					out[i].IdentityClaims = append(out[i].IdentityClaims, unavailableBoard(position, "", extra.Hashboard).IdentityClaims...)
				}
			}
			continue
		}
		if i >= len(slots) {
			out[i] = sdk.UnavailableHashboard(position, "")
			continue
		}
		out[i] = hashboardObservation(slots[i], position, now)
	}
	return out
}

func hashboardObservation(slot Slot, position int32, now time.Time) sdk.ContainerHashboard {
	unavailable := func(reason string) sdk.ContainerHashboard { return unavailableBoard(position, reason, slot.Hashboard) }
	if slot.invalid {
		return unavailable(sdk.HashboardInvalidObservation)
	}
	if slot.unreported {
		return unavailable("")
	}
	if !slot.Occupied {
		if slot.Hashboard != nil {
			return unavailable(sdk.HashboardInvalidObservation)
		}
		return sdk.ContainerHashboard{Position: position, Occupancy: "empty", State: sdk.ModuleUnknown}
	}
	if slot.Hashboard == nil {
		return unavailable("")
	}
	b := slot.Hashboard
	v := sdk.ContainerHashboard{Position: position, Occupancy: "occupied", MACAddress: normalizedBoardMAC(b.MACAddress), Serial: strings.TrimSpace(b.Serial)}
	if !sdk.SafeHashboardIdentity(v.MACAddress) || !sdk.SafeHashboardIdentity(v.Serial) || !sdk.SafeHashboardIdentity(b.MiningState) {
		return unavailable(sdk.HashboardInvalidObservation)
	}
	if v.MACAddress == "" && v.Serial == "" {
		return unavailable(sdk.HashboardMissingIdentity)
	}
	v.MiningState, v.State = b.MiningState, boardState(*b, now)
	v.StateObservedAt = moduleObservedAt([]Board{*b}, v.State, now)
	if b.Source != "HASHBOARD_SOURCE_REMOTE" && (b.raw == nil || len(b.raw["isolated"]) > 0 && string(b.raw["isolated"]) != "null") {
		v.Isolated = &b.Isolated
	}
	raw := b.raw
	if raw == nil {
		data, err := json.Marshal(b)
		if err != nil {
			return unavailable(sdk.HashboardInvalidObservation)
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return unavailable(sdk.HashboardInvalidObservation)
		}
	}
	metadata := make(map[string]json.RawMessage, len(raw))
	for key, value := range raw {
		if key != "health" {
			metadata[key] = value
		}
	}
	if locationData := raw["location"]; len(locationData) > 0 && string(locationData) != "null" {
		var location map[string]json.RawMessage
		if err := json.Unmarshal(locationData, &location); err != nil {
			return unavailable(sdk.HashboardAmbiguousSlot)
		}
		if p := location["hashboardPosition"]; len(p) > 0 {
			resolved, err := strconv.ParseInt(strings.Trim(string(p), "\""), 10, 32)
			if err != nil || (resolved != 0 && resolved != int64(position)) {
				return unavailable(sdk.HashboardAmbiguousSlot)
			}
		}
	}
	var err error
	v.MetadataJSON, err = json.Marshal(metadata)
	if err != nil {
		return unavailable(sdk.HashboardInvalidObservation)
	}
	v.HealthJSON = raw["health"]
	for _, data := range []json.RawMessage{v.MetadataJSON, v.HealthJSON} {
		if len(data) > 0 && (len(data) > 256*1024 || sdk.ValidateHashboardJSON(data) != nil) {
			return unavailable(sdk.HashboardInvalidObservation)
		}
	}
	if h := b.Health; h != nil {
		v.ObservedAt = h.Timestamp
		if !h.Timestamp.IsZero() && now.Sub(h.Timestamp) < healthMaxAge && h.Timestamp.Sub(now) <= clockSkewAllowance {
			if nonnegative(h.Hashrate) && !math.IsInf(*h.Hashrate*1e12, 0) {
				v.HashrateHS = new(*h.Hashrate * 1e12)
			}
			if nonnegative(h.Power) {
				v.PowerW = h.Power
			}
			if v.HashrateHS != nil && *v.HashrateHS > 0 && v.PowerW != nil {
				v.EfficiencyJH = new(*v.PowerW / *v.HashrateHS)
			}
			if finite(h.AverageAsicTemperatureC) {
				v.TemperatureC = h.AverageAsicTemperatureC
			}
			v.HashrailOK, v.RefclksOK = h.HashrailOK, h.RefclksOK
		}
	}
	return v
}

// Count before accepting any slot so map order never decides identity ownership.
func boardIdentityCounts(wire MapResponse) map[string]int {
	counts := map[string]int{}
	for _, tank := range wire.ContainerMap.Tanks {
		for _, module := range tank.Modules {
			for _, slot := range module.Slots {
				if slot.Hashboard == nil {
					continue
				}
				for _, key := range boardIdentityKeys(normalizedBoardMAC(slot.Hashboard.MACAddress), strings.TrimSpace(slot.Hashboard.Serial)) {
					counts[key]++
				}
			}
		}
	}
	return counts
}
func boardIdentityKeys(mac, serial string) []string {
	keys := []string{}
	if mac != "" {
		keys = append(keys, "mac:"+mac)
	}
	if serial != "" {
		keys = append(keys, "serial:"+serial)
	}
	return keys
}

func unavailableBoard(position int32, reason string, board *Board) sdk.ContainerHashboard {
	out := sdk.UnavailableHashboard(position, reason)
	if board == nil {
		return out
	}
	claim := sdk.HashboardIdentityClaim{}
	if sdk.SafeHashboardIdentity(board.MACAddress) {
		claim.MACAddress = normalizedBoardMAC(board.MACAddress)
	}
	if sdk.SafeHashboardIdentity(board.Serial) {
		claim.Serial = strings.TrimSpace(board.Serial)
	}
	if claim.MACAddress != "" || claim.Serial != "" {
		out.IdentityClaims = []sdk.HashboardIdentityClaim{claim}
	}
	return out
}
