package ct1

import (
	"testing"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// A release channel verifies a container's PSU firmware by this observation,
// so it must only name a version every PSU a Fleet update can reach reports.
func TestObservedPSUFirmwareRequiresCompleteFreshInventory(t *testing.T) {
	now := time.Now().UTC()
	const version = "app=1410;bl=1110"
	psu := func(version string) *mdk.PsuIdentity { return &mdk.PsuIdentity{FirmwareVersion: version} }
	unlocated := func(version string) *mdk.Hashboard {
		return &mdk.Hashboard{MacAddress: "aa:bb:cc:dd:ee:99", PsuIdentity: psu(version), Health: &mdk.HashboardHealth{Timestamp: timestamppb.New(now)}}
	}
	for _, tc := range []struct {
		name        string
		change      func(*mdk.GetContainerMapResponse)
		wantVersion string
		wantMixed   bool
	}{
		{"uniform", func(*mdk.GetContainerMapResponse) {}, version, false},
		{"surrounding space", func(w *mdk.GetContainerMapResponse) { firstBoard(w).PsuIdentity = psu(" " + version + " ") }, version, false},
		{"one module on another version", func(w *mdk.GetContainerMapResponse) {
			for _, slot := range w.ContainerMap.Tanks[3].Modules[5].Slots {
				slot.Hashboard.PsuIdentity = psu("app=1310;bl=1110")
			}
		}, "", true},
		{"boards of one module disagree", func(w *mdk.GetContainerMapResponse) { firstBoard(w).PsuIdentity = psu("app=1310;bl=1110") }, "", true},
		// A PSU update reaches located modules only, so an unlocated board's
		// PSU can neither hold the container back nor be brought up to date.
		{"unlocated board on another version", func(w *mdk.GetContainerMapResponse) {
			w.ContainerMap.Tanks = append(w.ContainerMap.Tanks, unlocatedTank(unlocated("app=1310;bl=1110")))
		}, version, false},
		{"version unavailable", func(w *mdk.GetContainerMapResponse) { firstBoard(w).PsuIdentity = psu("") }, "", false},
		{"no PSU identity", func(w *mdk.GetContainerMapResponse) { firstBoard(w).PsuIdentity = nil }, "", false},
		{"unavailable hides a difference", func(w *mdk.GetContainerMapResponse) {
			firstBoard(w).PsuIdentity = psu("app=1310;bl=1110")
			w.ContainerMap.Tanks[5].Modules[15].Slots[0].Hashboard.PsuIdentity = nil
		}, "", false},
		{"missing board", func(w *mdk.GetContainerMapResponse) { w.ContainerMap.Tanks[0].Modules[0].Slots[0].Hashboard = nil }, "", false},
		{"duplicate identity", func(w *mdk.GetContainerMapResponse) { firstBoard(w).MacAddress = boardMAC(1, 1, 2) }, "", false},
		{"missing identity", func(w *mdk.GetContainerMapResponse) { firstBoard(w).MacAddress = "" }, "", false},
		{"missing health", func(w *mdk.GetContainerMapResponse) { firstBoard(w).Health = nil }, "", false},
		{"stale", func(w *mdk.GetContainerMapResponse) {
			firstBoard(w).Health.Timestamp = timestamppb.New(now.Add(-healthMaxAge - time.Second))
		}, "", false},
		{"future", func(w *mdk.GetContainerMapResponse) {
			firstBoard(w).Health.Timestamp = timestamppb.New(now.Add(clockSkewAllowance + time.Second))
		}, "", false},
		{"missing module", func(w *mdk.GetContainerMapResponse) {
			w.ContainerMap.Tanks[2].Modules = w.ContainerMap.Tanks[2].Modules[1:]
		}, "", false},
		{"missing tank", func(w *mdk.GetContainerMapResponse) { w.ContainerMap.Tanks = w.ContainerMap.Tanks[1:] }, "", false},
		{"unlocated only", func(w *mdk.GetContainerMapResponse) {
			w.ContainerMap.Tanks = []*mdk.Tank{unlocatedTank(unlocated(version))}
		}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := firmwareContainerMap(now)
			for _, tank := range wire.ContainerMap.Tanks {
				for _, module := range tank.Modules {
					for _, slot := range module.Slots {
						slot.Hashboard.PsuIdentity = psu(version)
					}
				}
			}
			tc.change(wire)
			topology, err := snapshot(mapResponse(wire), now)
			if err != nil {
				topology = nil
			}
			got, mixed := observedPSUFirmware(wire, topology, now)
			require.Equal(t, tc.wantVersion, got)
			require.Equal(t, tc.wantMixed, mixed)
		})
	}
	got, mixed := observedPSUFirmware(nil, nil, now)
	require.Empty(t, got)
	require.False(t, mixed)

	// Hashboard firmware and PSU firmware are observed independently.
	wire := firmwareContainerMap(now)
	topology, err := snapshot(mapResponse(wire), now)
	require.NoError(t, err)
	hashboards, _ := observedHashboardFirmware(wire, topology, now)
	require.Equal(t, "2.0.0", hashboards)
	got, mixed = observedPSUFirmware(wire, topology, now)
	require.Empty(t, got, "boards that report no PSU firmware leave it unknown")
	require.False(t, mixed)
}
