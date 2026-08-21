package targets

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	fm "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/schedule/v1"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

func TestExpandDeduplicatesMinerRackAndGroupTargets(t *testing.T) {
	ctrl := gomock.NewController(t)
	collectionStore := mocks.NewMockCollectionStore(ctrl)
	collectionStore.EXPECT().
		GetDeviceIdentifiersByDeviceSetID(gomock.Any(), int64(10), int64(1)).
		Return([]string{"rack-1", "shared"}, nil)
	collectionStore.EXPECT().
		GetDeviceIdentifiersByDeviceSetID(gomock.Any(), int64(20), int64(1)).
		Return([]string{"group-1", "shared"}, nil)

	got, err := Expand(context.Background(), collectionStore, nil, []*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER, TargetId: "miner-1"},
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_RACK, TargetId: "10"},
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_GROUP, TargetId: "20"},
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER, TargetId: "miner-1"},
	}, 1, nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"miner-1", "rack-1", "shared", "group-1"}, got)
}

func TestExpandResolvesSiteAndBuildingTargets(t *testing.T) {
	ctrl := gomock.NewController(t)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	deviceStore.EXPECT().
		GetDeviceIdentifiersByOrgWithFilter(gomock.Any(), int64(1), &stores.MinerFilter{
			SiteIDs:         []int64{7},
			PairingStatuses: scheduleTargetPairingStatuses,
		}).
		Return([]string{"site-miner", "shared"}, nil)
	deviceStore.EXPECT().
		GetDeviceIdentifiersByOrgWithFilter(gomock.Any(), int64(1), &stores.MinerFilter{
			BuildingIDs:     []int64{9},
			PairingStatuses: scheduleTargetPairingStatuses,
		}).
		Return([]string{"building-miner", "shared"}, nil)

	got, err := Expand(context.Background(), nil, deviceStore, []*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_SITE, TargetId: "7"},
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_BUILDING, TargetId: "9"},
	}, 1, nil)

	require.NoError(t, err)
	// "shared" is deduped across the site and building expansions.
	assert.Equal(t, []string{"site-miner", "shared", "building-miner"}, got)
}

func TestExpandSiteTargetUsesPairedLikeStatuses(t *testing.T) {
	ctrl := gomock.NewController(t)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	var captured *stores.MinerFilter
	deviceStore.EXPECT().
		GetDeviceIdentifiersByOrgWithFilter(gomock.Any(), int64(1), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ int64, filter *stores.MinerFilter) ([]string, error) {
			captured = filter
			return []string{"m"}, nil
		})

	_, err := Expand(context.Background(), nil, deviceStore, []*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_SITE, TargetId: "7"},
	}, 1, nil)

	require.NoError(t, err)
	require.NotNil(t, captured)
	// Must not silently collapse to PAIRED-only (the device-resolver default).
	assert.ElementsMatch(t, []fm.PairingStatus{
		fm.PairingStatus_PAIRING_STATUS_PAIRED,
		fm.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
		fm.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD,
	}, captured.PairingStatuses)
}

func TestExpandDeduplicatesAcrossAllTargetTypes(t *testing.T) {
	ctrl := gomock.NewController(t)
	collectionStore := mocks.NewMockCollectionStore(ctrl)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	collectionStore.EXPECT().
		GetDeviceIdentifiersByDeviceSetID(gomock.Any(), int64(10), int64(1)).
		Return([]string{"shared"}, nil)
	deviceStore.EXPECT().
		GetDeviceIdentifiersByOrgWithFilter(gomock.Any(), int64(1), gomock.Any()).
		Return([]string{"shared", "site-only"}, nil)

	got, err := Expand(context.Background(), collectionStore, deviceStore, []*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER, TargetId: "shared"},
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_RACK, TargetId: "10"},
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_SITE, TargetId: "7"},
	}, 1, nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"shared", "site-only"}, got)
}

func TestExpandCallsUnspecifiedHandler(t *testing.T) {
	var warned []string

	got, err := Expand(context.Background(), nil, nil, []*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_UNSPECIFIED, TargetId: "bad-target"},
	}, 1, func(targetID string) {
		warned = append(warned, targetID)
	})

	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Equal(t, []string{"bad-target"}, warned)
}

func TestExpandReturnsInvalidRackIDError(t *testing.T) {
	_, err := Expand(context.Background(), nil, nil, []*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_RACK, TargetId: "not-an-id"},
	}, 1, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid rack target_id "not-an-id"`)
}

func TestExpandReturnsInvalidSiteAndBuildingIDErrors(t *testing.T) {
	_, siteErr := Expand(context.Background(), nil, nil, []*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_SITE, TargetId: "nope"},
	}, 1, nil)
	require.Error(t, siteErr)
	assert.Contains(t, siteErr.Error(), `invalid site target_id "nope"`)

	_, buildingErr := Expand(context.Background(), nil, nil, []*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_BUILDING, TargetId: "nope"},
	}, 1, nil)
	require.Error(t, buildingErr)
	assert.Contains(t, buildingErr.Error(), `invalid building target_id "nope"`)
}

func TestExpandWrapsCollectionStoreErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	collectionStore := mocks.NewMockCollectionStore(ctrl)
	collectionStore.EXPECT().
		GetDeviceIdentifiersByDeviceSetID(gomock.Any(), int64(10), int64(1)).
		Return(nil, errors.New("db down"))

	_, err := Expand(context.Background(), collectionStore, nil, []*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_RACK, TargetId: "10"},
	}, 1, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to resolve rack 10")
}

func TestExpandWrapsDeviceStoreErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	deviceStore.EXPECT().
		GetDeviceIdentifiersByOrgWithFilter(gomock.Any(), int64(1), gomock.Any()).
		Return(nil, errors.New("db down"))

	_, err := Expand(context.Background(), nil, deviceStore, []*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_SITE, TargetId: "7"},
	}, 1, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to resolve site 7")
}
