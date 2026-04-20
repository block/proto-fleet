package schedule

import (
	"context"
	"slices"
	"strings"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/schedule/v1"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/testutil"
	"google.golang.org/protobuf/proto"
)

func TestValidateScheduleFields(t *testing.T) {
	tests := []struct {
		name       string
		schedName  string
		action     pb.ScheduleAction
		config     *pb.PowerTargetConfig
		schedType  pb.ScheduleType
		recurrence *pb.ScheduleRecurrence
		wantErr    bool
	}{
		{
			name:      "empty name",
			schedName: "",
			action:    pb.ScheduleAction_SCHEDULE_ACTION_REBOOT,
			schedType: pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME,
			wantErr:   true,
		},
		{
			name:      "name longer than max length",
			schedName: strings.Repeat("a", maxScheduleNameLength+1),
			action:    pb.ScheduleAction_SCHEDULE_ACTION_REBOOT,
			schedType: pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME,
			wantErr:   true,
		},
		{
			name:      "unspecified action",
			schedName: "Night reboot",
			action:    pb.ScheduleAction_SCHEDULE_ACTION_UNSPECIFIED,
			schedType: pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME,
			wantErr:   true,
		},
		{
			name:      "unspecified schedule type",
			schedName: "Night reboot",
			action:    pb.ScheduleAction_SCHEDULE_ACTION_REBOOT,
			schedType: pb.ScheduleType_SCHEDULE_TYPE_UNSPECIFIED,
			wantErr:   true,
		},
		{
			name:      "set_power_target missing config",
			schedName: "Limit power",
			action:    pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET,
			schedType: pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME,
			wantErr:   true,
		},
		{
			name:      "set_power_target with valid config",
			schedName: "Limit power",
			action:    pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET,
			config: &pb.PowerTargetConfig{
				Mode: pb.PowerTargetMode_POWER_TARGET_MODE_DEFAULT,
			},
			schedType: pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME,
			wantErr:   false,
		},
		{
			name:      "reboot rejects action_config",
			schedName: "Night reboot",
			action:    pb.ScheduleAction_SCHEDULE_ACTION_REBOOT,
			config: &pb.PowerTargetConfig{
				Mode: pb.PowerTargetMode_POWER_TARGET_MODE_DEFAULT,
			},
			schedType: pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME,
			wantErr:   true,
		},
		{
			name:      "recurring missing recurrence",
			schedName: "Night reboot",
			action:    pb.ScheduleAction_SCHEDULE_ACTION_REBOOT,
			schedType: pb.ScheduleType_SCHEDULE_TYPE_RECURRING,
			wantErr:   true,
		},
		{
			name:      "valid one-time reboot",
			schedName: "Night reboot",
			action:    pb.ScheduleAction_SCHEDULE_ACTION_REBOOT,
			schedType: pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME,
			wantErr:   false,
		},
		{
			name:      "valid one-time sleep",
			schedName: "Overnight sleep",
			action:    pb.ScheduleAction_SCHEDULE_ACTION_SLEEP,
			schedType: pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateScheduleFields(tt.schedName, tt.action, tt.config, tt.schedType, tt.recurrence)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateScheduleFields() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateRecurrence(t *testing.T) {
	tests := []struct {
		name    string
		rec     *pb.ScheduleRecurrence
		wantErr bool
	}{
		{
			name: "daily valid",
			rec: &pb.ScheduleRecurrence{
				Frequency: pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY,
				Interval:  1,
			},
		},
		{
			name: "weekly valid",
			rec: &pb.ScheduleRecurrence{
				Frequency:  pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_WEEKLY,
				Interval:   1,
				DaysOfWeek: []pb.DayOfWeek{pb.DayOfWeek_DAY_OF_WEEK_MONDAY},
			},
		},
		{
			name: "weekly no days",
			rec: &pb.ScheduleRecurrence{
				Frequency: pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_WEEKLY,
				Interval:  1,
			},
			wantErr: true,
		},
		{
			name: "weekly unspecified day rejected",
			rec: &pb.ScheduleRecurrence{
				Frequency:  pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_WEEKLY,
				Interval:   1,
				DaysOfWeek: []pb.DayOfWeek{pb.DayOfWeek_DAY_OF_WEEK_UNSPECIFIED},
			},
			wantErr: true,
		},
		{
			name: "monthly valid",
			rec: &pb.ScheduleRecurrence{
				Frequency:  pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_MONTHLY,
				Interval:   1,
				DayOfMonth: proto.Int32(15),
			},
		},
		{
			name: "monthly no day",
			rec: &pb.ScheduleRecurrence{
				Frequency: pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_MONTHLY,
				Interval:  1,
			},
			wantErr: true,
		},
		{
			name: "monthly day out of range",
			rec: &pb.ScheduleRecurrence{
				Frequency:  pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_MONTHLY,
				Interval:   1,
				DayOfMonth: proto.Int32(32),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRecurrence(tt.rec)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateRecurrence() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateTargets(t *testing.T) {
	tests := []struct {
		name    string
		targets []*pb.ScheduleTarget
		wantErr bool
	}{
		{
			name:    "empty targets",
			targets: nil,
		},
		{
			name: "valid targets",
			targets: []*pb.ScheduleTarget{
				{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_RACK, TargetId: "1"},
				{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_GROUP, TargetId: "2"},
				{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER, TargetId: "miner-1"},
			},
		},
		{
			name: "duplicate targets",
			targets: []*pb.ScheduleTarget{
				{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_RACK, TargetId: "1"},
				{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_RACK, TargetId: "1"},
			},
			wantErr: true,
		},
		{
			name: "unspecified target type",
			targets: []*pb.ScheduleTarget{
				{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_UNSPECIFIED, TargetId: "1"},
			},
			wantErr: true,
		},
		{
			name: "non-numeric rack target_id",
			targets: []*pb.ScheduleTarget{
				{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_RACK, TargetId: "rack-1"},
			},
			wantErr: true,
		},
		{
			name: "non-numeric group target_id",
			targets: []*pb.ScheduleTarget{
				{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_GROUP, TargetId: "group-1"},
			},
			wantErr: true,
		},
		{
			name: "blank target id",
			targets: []*pb.ScheduleTarget{
				{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER, TargetId: "   "},
			},
			wantErr: true,
		},
		{
			name:    "nil target",
			targets: []*pb.ScheduleTarget{nil},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTargets(tt.targets)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateTargets() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

type stubTransactor struct{}

func (stubTransactor) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func (stubTransactor) RunInTxWithResult(ctx context.Context, fn func(context.Context) (any, error)) (any, error) {
	return fn(ctx)
}

type stubSchedulePriorityStore struct {
	lockSchedulePriorityFn func(ctx context.Context, orgID int64) error
	listScheduleStatusesFn func(ctx context.Context, orgID int64) ([]interfaces.ScheduleIDStatus, error)
	reorderSchedulesFn     func(ctx context.Context, orgID int64, ids []int64) error
	getMaxPriorityFn       func(ctx context.Context, orgID int64) (int32, error)
}

func (s stubSchedulePriorityStore) GetMaxPriority(ctx context.Context, orgID int64) (int32, error) {
	if s.getMaxPriorityFn != nil {
		return s.getMaxPriorityFn(ctx, orgID)
	}
	return 0, nil
}

func (s stubSchedulePriorityStore) LockSchedulePriority(ctx context.Context, orgID int64) error {
	if s.lockSchedulePriorityFn != nil {
		return s.lockSchedulePriorityFn(ctx, orgID)
	}
	return nil
}

func (s stubSchedulePriorityStore) ReorderSchedules(ctx context.Context, orgID int64, ids []int64) error {
	if s.reorderSchedulesFn != nil {
		return s.reorderSchedulesFn(ctx, orgID, ids)
	}
	return nil
}

func (s stubSchedulePriorityStore) ListScheduleIDStatuses(ctx context.Context, orgID int64) ([]interfaces.ScheduleIDStatus, error) {
	if s.listScheduleStatusesFn != nil {
		return s.listScheduleStatusesFn(ctx, orgID)
	}
	return nil, nil
}

func TestReorderSchedulesAcceptsCompletedIDsInPayload(t *testing.T) {
	var gotIDs []int64

	priorityStore := stubSchedulePriorityStore{
		listScheduleStatusesFn: func(_ context.Context, orgID int64) ([]interfaces.ScheduleIDStatus, error) {
			if orgID != 7 {
				t.Fatalf("unexpected org ID: %d", orgID)
			}
			return []interfaces.ScheduleIDStatus{
				{ID: 1, Status: "active"},
				{ID: 3, Status: statusCompleted},
				{ID: 2, Status: "paused"},
			}, nil
		},
		reorderSchedulesFn: func(_ context.Context, orgID int64, ids []int64) error {
			if orgID != 7 {
				t.Fatalf("unexpected org ID: %d", orgID)
			}
			gotIDs = append([]int64(nil), ids...)
			return nil
		},
	}

	svc := NewService(nil, nil, priorityStore, stubTransactor{}, nil)
	ctx := testutil.MockAuthContextForTesting(context.Background(), 11, 7)

	if err := svc.ReorderSchedules(ctx, []int64{2, 3, 1}); err != nil {
		t.Fatalf("ReorderSchedules() error = %v", err)
	}

	want := []int64{2, 1, 3}
	if !slices.Equal(gotIDs, want) {
		t.Fatalf("ReorderSchedules() reordered IDs = %v, want %v", gotIDs, want)
	}
}
