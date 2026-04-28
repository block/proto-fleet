package targets

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	pb "github.com/block/proto-fleet/server/generated/grpc/schedule/v1"
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

	got, err := Expand(context.Background(), collectionStore, []*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER, TargetId: "miner-1"},
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_RACK, TargetId: "10"},
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_GROUP, TargetId: "20"},
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER, TargetId: "miner-1"},
	}, 1, nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"miner-1", "rack-1", "shared", "group-1"}, got)
}

func TestExpandCallsUnspecifiedHandler(t *testing.T) {
	var warned []string

	got, err := Expand(context.Background(), nil, []*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_UNSPECIFIED, TargetId: "bad-target"},
	}, 1, func(targetID string) {
		warned = append(warned, targetID)
	})

	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Equal(t, []string{"bad-target"}, warned)
}

func TestExpandReturnsInvalidRackIDError(t *testing.T) {
	_, err := Expand(context.Background(), nil, []*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_RACK, TargetId: "not-an-id"},
	}, 1, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid rack target_id "not-an-id"`)
}

func TestExpandWrapsCollectionStoreErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	collectionStore := mocks.NewMockCollectionStore(ctrl)
	collectionStore.EXPECT().
		GetDeviceIdentifiersByDeviceSetID(gomock.Any(), int64(10), int64(1)).
		Return(nil, errors.New("db down"))

	_, err := Expand(context.Background(), collectionStore, []*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_RACK, TargetId: "10"},
	}, 1, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to resolve rack 10")
}
