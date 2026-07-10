package command

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/authn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	fleetpb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

// fakeDeviceResolver records the selector/org it was called with and returns a
// canned identifier list, so tests can assert how the command service wraps the
// all_matching_filter case.
type fakeDeviceResolver struct {
	gotSelector *fleetpb.DeviceSelector
	gotOrgID    int64
	ids         []string
	err         error
	calls       int
}

func (f *fakeDeviceResolver) ResolveDeviceIdentifiers(_ context.Context, selector *fleetpb.DeviceSelector, orgID int64) ([]string, error) {
	f.calls++
	f.gotSelector = selector
	f.gotOrgID = orgID
	return f.ids, f.err
}

func TestFleetSelectorForMatchingFilter(t *testing.T) {
	t.Run("nil filter defaults to command-eligible pairing statuses", func(t *testing.T) {
		selector := fleetSelectorForMatchingFilter(nil)

		all, ok := selector.SelectionType.(*fleetpb.DeviceSelector_AllDevices)
		require.True(t, ok, "expected all_devices selector case")
		assert.Equal(t, []fleetpb.PairingStatus{
			fleetpb.PairingStatus_PAIRING_STATUS_PAIRED,
			fleetpb.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD,
		}, all.AllDevices.PairingStatuses)
	})

	t.Run("preserves filter constraints and applies pairing default when unset", func(t *testing.T) {
		input := &fleetpb.MinerListFilter{
			RackIds: []int64{7, 9},
			Models:  []string{"S19"},
		}

		selector := fleetSelectorForMatchingFilter(input)

		all := selector.SelectionType.(*fleetpb.DeviceSelector_AllDevices).AllDevices
		assert.Equal(t, []int64{7, 9}, all.RackIds)
		assert.Equal(t, []string{"S19"}, all.Models)
		assert.Equal(t, []fleetpb.PairingStatus{
			fleetpb.PairingStatus_PAIRING_STATUS_PAIRED,
			fleetpb.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD,
		}, all.PairingStatuses)
		// Input must not be mutated.
		assert.Empty(t, input.PairingStatuses)
	})

	t.Run("honors explicit pairing statuses", func(t *testing.T) {
		input := &fleetpb.MinerListFilter{
			PairingStatuses: []fleetpb.PairingStatus{fleetpb.PairingStatus_PAIRING_STATUS_PAIRED},
		}

		selector := fleetSelectorForMatchingFilter(input)

		all := selector.SelectionType.(*fleetpb.DeviceSelector_AllDevices).AllDevices
		assert.Equal(t, []fleetpb.PairingStatus{fleetpb.PairingStatus_PAIRING_STATUS_PAIRED}, all.PairingStatuses)
	})
}

func TestResolveSelectorIdentifiers_AllMatchingFilter(t *testing.T) {
	ctx := authn.SetInfo(context.Background(), &session.Info{OrganizationID: 42})

	t.Run("delegates to injected resolver with wrapped selector", func(t *testing.T) {
		resolver := &fakeDeviceResolver{ids: []string{"dev-1", "dev-2"}}
		svc := &Service{deviceResolver: resolver}

		selector := &pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_AllMatchingFilter{
				AllMatchingFilter: &fleetpb.MinerListFilter{RackIds: []int64{5}},
			},
		}

		ids, err := svc.resolveSelectorIdentifiers(ctx, selector, commandtype.Reboot)
		require.NoError(t, err)
		assert.Equal(t, []string{"dev-1", "dev-2"}, ids)
		assert.Equal(t, 1, resolver.calls)
		assert.Equal(t, int64(42), resolver.gotOrgID)

		all := resolver.gotSelector.SelectionType.(*fleetpb.DeviceSelector_AllDevices).AllDevices
		assert.Equal(t, []int64{5}, all.RackIds)
		assert.Equal(t, []fleetpb.PairingStatus{
			fleetpb.PairingStatus_PAIRING_STATUS_PAIRED,
			fleetpb.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD,
		}, all.PairingStatuses)
	})

	t.Run("errors when resolver not configured", func(t *testing.T) {
		svc := &Service{}
		selector := &pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_AllMatchingFilter{
				AllMatchingFilter: &fleetpb.MinerListFilter{},
			},
		}

		_, err := svc.resolveSelectorIdentifiers(ctx, selector, commandtype.Reboot)
		require.Error(t, err)
	})

	t.Run("propagates resolver error", func(t *testing.T) {
		resolver := &fakeDeviceResolver{err: errors.New("boom")}
		svc := &Service{deviceResolver: resolver}
		selector := &pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_AllMatchingFilter{
				AllMatchingFilter: &fleetpb.MinerListFilter{},
			},
		}

		_, err := svc.resolveSelectorIdentifiers(ctx, selector, commandtype.Reboot)
		require.Error(t, err)
	})
}

func TestCapabilityChecker_AllMatchingFilter_RequiresResolver(t *testing.T) {
	ctx := context.Background()
	checker := &CapabilityChecker{}
	selector := &pb.DeviceSelector{
		SelectionType: &pb.DeviceSelector_AllMatchingFilter{
			AllMatchingFilter: &fleetpb.MinerListFilter{},
		},
	}

	_, err := checker.getDeviceInfo(ctx, selector, 42)
	require.Error(t, err)
}
