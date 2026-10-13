package ct1

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"

	"github.com/stretchr/testify/require"
)

func TestMalformedBoardRetainsModuleIdentityConflict(t *testing.T) {
	for _, health := range []string{`{"hashrate":"bad"}`, `{"hashrate":1e999}`} {
		t.Run(health, func(t *testing.T) {
			var wire MapResponse
			data := fmt.Sprintf(`{"containerMap":{"tanks":[{"id":1,"modules":[{"id":1,"slots":[
				{"occupied":true,"hashboard":{"macAddress":"a","moduleId":"module-a","health":{"timestamp":"2026-10-05T12:00:00Z","psuElectrical":{"inputPower":3600}}}},
				{"occupied":true,"hashboard":{"macAddress":"b","moduleId":"replacement","health":%s}},
				{"occupied":false}
			]}]}]}}`, health)
			require.NoError(t, json.Unmarshal([]byte(data), &wire))
			slot := wire.ContainerMap.Tanks[0].Modules[0].Slots[1]
			require.True(t, slot.invalid)
			require.Equal(t, "replacement", slot.Hashboard.ModuleID)
			got, err := snapshot(wire, time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC))
			require.NoError(t, err)
			module := got.Tanks[0].Modules[0]
			require.NotNil(t, module.Metrics.PowerW)
			require.Empty(t, module.ReportedID, "disputed hardware must not own the surviving measurements")
			require.False(t, module.IdentityConfirmed)
		})
	}
}

func TestHashboardObservationsPreserveSlotsAndEvidence(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	var wire MapResponse
	require.NoError(t, json.Unmarshal([]byte(`{"containerMap":{"tanks":[{"id":1,"modules":[{"id":2,"slots":[{"occupied":true,"hashboard":{"macAddress":"AA:BB:CC:DD:EE:01","serial":"board-A","firmwareVersion":"1.2.3","miningState":"MINING_STATE_MINING","isolated":false,"location":{"hashboardPosition":"1"},"health":{"timestamp":"2026-10-05T11:59:59Z","hashrate":100,"averageAsicTemperatureC":65,"hashrailOk":false,"uptime":"9007199254740993","errors":[{"cause":"ASIC","stack":0,"asic":{"id":5}}],"temperatureSensors":[{"index":0,"temperature":42}],"lastShutdownError":{"cause":"old"}}}},{"occupied":false}]}]}]}}`), &wire))
	got, err := snapshot(wire, now)
	require.NoError(t, err)
	boards := got.Tanks[0].Modules[0].Hashboards
	require.Len(t, boards, 3)
	require.Equal(t, "occupied", boards[0].Occupancy)
	require.Equal(t, "aa:bb:cc:dd:ee:01", boards[0].MACAddress)
	require.Equal(t, "board-A", boards[0].Serial)
	require.NotNil(t, boards[0].Isolated)
	require.False(t, *boards[0].Isolated)
	require.Equal(t, 100e12, *boards[0].HashrateHS)
	require.Equal(t, now.Add(-time.Second), boards[0].ObservedAt)
	require.Equal(t, "attention", string(boards[0].State))
	require.Contains(t, string(boards[0].HealthJSON), `"9007199254740993"`)
	require.Contains(t, string(boards[0].MetadataJSON), `"firmwareVersion":"1.2.3"`)
	require.NotContains(t, string(boards[0].MetadataJSON), `"health"`)
	require.Equal(t, "empty", boards[1].Occupancy)
	require.Equal(t, "unknown", boards[2].Occupancy)
}

func TestHashboardFreshnessAndRemoteIsolation(t *testing.T) {
	now := time.Now().UTC()
	for _, age := range []time.Duration{2 * time.Minute, -6 * time.Second} {
		b := Board{MACAddress: "a", MiningState: "MINING_STATE_MINING", Health: &Health{Timestamp: now.Add(-age), Hashrate: new(100.0), HashrailOK: new(false)}}
		got := hashboardObservations([]Slot{{Occupied: true, Hashboard: &b}}, now)
		require.Nil(t, got[0].HashrateHS)
		require.Nil(t, got[0].HashrailOK)
		require.Equal(t, "unknown", string(got[0].State))
	}
	var b Board
	require.NoError(t, json.Unmarshal([]byte(`{"macAddress":"a","source":"HASHBOARD_SOURCE_REMOTE","isolated":false}`), &b))
	got := hashboardObservations([]Slot{{Occupied: true, Hashboard: &b}}, now)
	require.Nil(t, got[0].Isolated)
	// Remote isolation is not authoritative, even when a controller emits true.
	b.Isolated = true
	b.MiningState = "MINING_STATE_MINING"
	b.Health = &Health{Timestamp: now, Hashrate: new(100.0)}
	got = hashboardObservations([]Slot{{Occupied: true, Hashboard: &b}}, now)
	require.Nil(t, got[0].Isolated)
	require.Equal(t, "hashing", string(got[0].State))
	got = hashboardObservations([]Slot{{Occupied: true}}, now)
	require.Equal(t, "unknown", got[0].Occupancy)
}

func TestHashboardIdentityAndMissingOccupancy(t *testing.T) {
	var wire MapResponse
	require.NoError(t, json.Unmarshal([]byte(`{"containerMap":{"tanks":[{"id":1,"modules":[{"id":1,"slots":[{"occupied":true,"hashboard":{"serial":" board-serial "}},{}, {"occupied":false}]}]}]}}`), &wire))
	got, err := snapshot(wire, time.Now())
	require.NoError(t, err)
	boards := got.Tanks[0].Modules[0].Hashboards
	require.Equal(t, "board-serial", boards[0].Serial)
	require.Equal(t, "unknown", boards[1].Occupancy)
	require.Equal(t, "empty", boards[2].Occupancy)
	wire.ContainerMap.Tanks[0].Modules[0].Slots = []Slot{
		{Occupied: true, Hashboard: &Board{MACAddress: "AA-BB-CC-DD-EE-01"}},
		{Occupied: true, Hashboard: &Board{MACAddress: "aa:bb:cc:dd:ee:01"}},
	}
	got, err = snapshot(wire, time.Now())
	require.NoError(t, err)
	for _, board := range got.Tanks[0].Modules[0].Hashboards[:2] {
		require.Equal(t, "unknown", board.Occupancy)
		require.Equal(t, "identity_conflict", board.UnavailableReason)
	}
}

func TestMalformedHashboardDoesNotAbortSnapshot(t *testing.T) {
	for _, slots := range []string{
		`[{"occupied":false,"hashboard":{"macAddress":"a"}}]`,
		`[{},{},{},{}]`,
		`[{"occupied":true,"hashboard":{"macAddress":"a","location":{"hashboardPosition":"2"}}}]`,
		`[{"occupied":true,"hashboard":{"macAddress":"a","health":{"hashrate":"bad"}}}]`,
		`[{"occupied":true,"hashboard":{}}]`,
	} {
		t.Run(slots, func(t *testing.T) {
			var wire MapResponse
			require.NoError(t, json.Unmarshal([]byte(`{"containerMap":{"tanks":[{"id":1,"modules":[{"id":1,"slots":`+slots+`},{"id":2,"slots":[{"occupied":false}]}]}]}}`), &wire))
			got, err := snapshot(wire, time.Now())
			require.NoError(t, err)
			require.Len(t, got.Tanks[0].Modules, 2)
			require.Equal(t, "unknown", got.Tanks[0].Modules[0].Hashboards[0].Occupancy)
			require.Nil(t, got.Tanks[0].Modules[0].Metrics.HashrateHS)
			require.Equal(t, "empty", got.Tanks[0].Modules[1].Hashboards[0].Occupancy)
		})
	}
}

func TestHashboardUnknownResolvedPositionAndMissingIsolation(t *testing.T) {
	var wire MapResponse
	require.NoError(t, json.Unmarshal([]byte(`{"containerMap":{"tanks":[{"id":1,"modules":[{"id":1,"slots":[{"occupied":true,"hashboard":{"macAddress":"a","location":{"hashboardPosition":"0"}}}]}]}]}}`), &wire))
	got, err := snapshot(wire, time.Now())
	require.NoError(t, err)
	board := got.Tanks[0].Modules[0].Hashboards[0]
	require.Equal(t, "occupied", board.Occupancy)
	require.Nil(t, board.Isolated)
}

func TestMalformedBoardStillParticipatesInDuplicateDetection(t *testing.T) {
	for _, bad := range []string{
		`{"occupied":true,"hashboard":{"macAddress":"a","health":{"hashrate":"bad"}}}`,
		`{"occupied":false,"hashboard":{"macAddress":"a"}}`,
		`{"occupied":"bad","hashboard":{"macAddress":"a"}}`,
		`{"hashboard":{"macAddress":"a"}}`,
	} {
		t.Run(bad, func(t *testing.T) {
			var wire MapResponse
			require.NoError(t, json.Unmarshal([]byte(`{"containerMap":{"tanks":[{"id":1,"modules":[{"id":1,"slots":[`+bad+`,{"occupied":true,"hashboard":{"macAddress":"a"}}]}]}]}}`), &wire))
			got, err := snapshot(wire, time.Now())
			require.NoError(t, err)
			for _, board := range got.Tanks[0].Modules[0].Hashboards[:2] {
				require.Equal(t, "unknown", board.Occupancy)
			}
		})
	}
}

func TestRejectedHashboardClaimsSurviveSnapshot(t *testing.T) {
	for _, payload := range []string{
		`"health":{"hashrate":"bad"}`,
		`"health":{"diagnostic":"\u0000"}`,
		`"health":{"diagnostic":"\ud800"}`,
		`"health":{"diagnostic":1e200000}`,
		`"serial":"bad\u0000"`,
	} {
		t.Run(payload, func(t *testing.T) {
			var wire MapResponse
			require.NoError(t, json.Unmarshal([]byte(`{"containerMap":{"tanks":[{"id":1,"modules":[{"id":1,"slots":[{"occupied":true,"hashboard":{"macAddress":"a",`+payload+`}}]},{"id":2,"slots":[{"occupied":true,"hashboard":{"serial":"serial-a"}}]}]}]}}`), &wire))
			got, err := snapshot(wire, time.Now())
			require.NoError(t, err)
			data, err := json.Marshal(got)
			require.NoError(t, err)
			var transported sdk.ContainerSnapshot
			require.NoError(t, json.Unmarshal(data, &transported))
			first := transported.Tanks[0].Modules[0]
			require.Equal(t, "unknown", first.Hashboards[0].Occupancy)
			require.Equal(t, []sdk.HashboardIdentityClaim{{MACAddress: "a"}}, first.Hashboards[0].IdentityClaims)
			require.Empty(t, first.Hashboards[0].MACAddress)
			require.Nil(t, first.Metrics.HashrateHS)
			require.Equal(t, "serial-a", transported.Tanks[0].Modules[1].Hashboards[0].Serial)
		})
	}
}
