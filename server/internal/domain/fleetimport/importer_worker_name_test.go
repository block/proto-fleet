package fleetimport

import (
	"testing"

	"go.uber.org/mock/gomock"

	models "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
)

func TestSetWorkerNames_TrimsImportedWorkerName(t *testing.T) {
	ctrl := gomock.NewController(t)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	imp := &Importer{deviceStore: deviceStore}

	mac := "aa:bb:cc:dd:ee:01"
	normalizedMAC := networking.NormalizeMAC(mac)

	deviceStore.EXPECT().
		UpdateWorkerName(gomock.Any(), models.DeviceIdentifier("device-1"), "worker-01").
		Return(nil)

	count := imp.setWorkerNames(
		t.Context(),
		&ImportData{
			Miners: []ImportMiner{{
				MAC:        mac,
				WorkerName: " \nworker-01\t ",
			}},
		},
		map[string]*interfaces.PairedDeviceInfo{
			normalizedMAC: {DeviceIdentifier: "device-1", MacAddress: normalizedMAC},
		},
	)

	if count != 1 {
		t.Fatalf("expected 1 worker name set, got %d", count)
	}
}

func TestSetWorkerNames_FallsBackToNormalizedMACWhenImportedWorkerNameIsWhitespace(t *testing.T) {
	ctrl := gomock.NewController(t)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	imp := &Importer{deviceStore: deviceStore}

	mac := "aa:bb:cc:dd:ee:02"
	normalizedMAC := networking.NormalizeMAC(mac)

	deviceStore.EXPECT().
		UpdateWorkerName(gomock.Any(), models.DeviceIdentifier("device-2"), normalizedMAC).
		Return(nil)

	count := imp.setWorkerNames(
		t.Context(),
		&ImportData{
			Miners: []ImportMiner{{
				MAC:        mac,
				WorkerName: " \n\t ",
			}},
		},
		map[string]*interfaces.PairedDeviceInfo{
			normalizedMAC: {DeviceIdentifier: "device-2", MacAddress: normalizedMAC},
		},
	)

	if count != 1 {
		t.Fatalf("expected 1 worker name set, got %d", count)
	}
}
