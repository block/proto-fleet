package pairing

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	storemocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestMDNSProcessingFailureWarnsAndRetainsHealthyDevices(t *testing.T) {
	s := newScanTestService(t, func(context.Context, string, string) (*discoverymodels.DiscoveredDevice, error) {
		return testIdentifiedDevice(), nil
	})
	store := storemocks.NewMockDiscoveredDeviceStore(gomock.NewController(t))
	store.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ discoverymodels.DeviceOrgIdentifier, d *discoverymodels.DiscoveredDevice) (*discoverymodels.DiscoveredDevice, error) {
			if d.IpAddress == "192.168.1.1" {
				return nil, errors.New("private schema detail: SQLSTATE 23505")
			}
			return d, nil
		}).Times(2)
	s.discoveredDeviceStore = store
	results := make(chan *pb.DiscoverResponse, 2)
	ctx := mockSessionContext(t.Context(), 1, 1)
	s.discoverMDNSDevice(ctx, "192.168.1.1", "80", results)
	s.discoverMDNSDevice(ctx, "192.168.1.2", "80", results)

	warning := <-results
	require.Equal(t, "Fleet Server mDNS discovery incomplete: could not save discovered device", warning.Warning)
	require.Empty(t, warning.Devices)
	device := <-results
	require.Empty(t, device.Warning)
	require.Len(t, device.Devices, 1)
	require.Equal(t, "192.168.1.2", device.Devices[0].IpAddress)
	require.Empty(t, results)
}

func TestMDNSProbeMissDoesNotWarn(t *testing.T) {
	for _, probeErr := range []error{errors.New("not a miner"), context.DeadlineExceeded, context.Canceled} {
		t.Run(probeErr.Error(), func(t *testing.T) {
			s := newScanTestService(t, func(context.Context, string, string) (*discoverymodels.DiscoveredDevice, error) {
				return nil, probeErr
			})
			results := make(chan *pb.DiscoverResponse, 1)
			s.discoverMDNSDevice(t.Context(), "192.168.1.1", "80", results)
			require.Empty(t, results)
		})
	}
}

func TestMDNSProcessingWarningHonorsCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newScanTestService(t, func(context.Context, string, string) (*discoverymodels.DiscoveredDevice, error) {
			return testIdentifiedDevice(), nil
		})
		store := storemocks.NewMockDiscoveredDeviceStore(gomock.NewController(t))
		store.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("save failed"))
		s.discoveredDeviceStore = store
		ctx, cancel := context.WithCancel(mockSessionContext(t.Context(), 1, 1))
		defer cancel()
		done := make(chan struct{})
		go func() {
			defer close(done)
			s.discoverMDNSDevice(ctx, "192.168.1.1", "80", make(chan *pb.DiscoverResponse))
		}()
		synctest.Wait()
		cancel()
		<-done
	})
}
