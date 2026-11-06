package curtailment

import (
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/schedulewindow"
)

func weeknightScheduleProto() *pb.CurtailmentSchedule {
	return &pb.CurtailmentSchedule{
		ScheduleType: pb.CurtailmentScheduleType_CURTAILMENT_SCHEDULE_TYPE_RECURRING,
		Timezone:     "America/Chicago",
		StartDate:    "2026-06-01",
		StartTime:    "22:00",
		EndTime:      "06:00",
		Frequency:    pb.CurtailmentScheduleFrequency_CURTAILMENT_SCHEDULE_FREQUENCY_WEEKLY,
		DaysOfWeek: []pb.CurtailmentScheduleDayOfWeek{
			pb.CurtailmentScheduleDayOfWeek_CURTAILMENT_SCHEDULE_DAY_OF_WEEK_MONDAY,
			pb.CurtailmentScheduleDayOfWeek_CURTAILMENT_SCHEDULE_DAY_OF_WEEK_FRIDAY,
		},
	}
}

func TestAutomationScheduleRequestRoundTrip(t *testing.T) {
	t.Parallel()
	schedule := weeknightScheduleProto()
	rule := automationRuleFromCreateRequest(42, 501, &pb.CreateCurtailmentAutomationRuleRequest{
		RuleName:          "Weeknight peak",
		TriggerType:       pb.CurtailmentAutomationTriggerType_CURTAILMENT_AUTOMATION_TRIGGER_TYPE_SCHEDULE,
		Schedule:          schedule,
		ResponseProfileId: 3001,
	})

	assert.Equal(t, models.AutomationTriggerTypeSchedule, rule.TriggerType)
	assert.Equal(t, int64(0), rule.MQTTSourceID)
	require.NotNil(t, rule.Schedule)
	assert.Equal(t, int64(501), rule.Schedule.ServiceUserID, "the saving user runs the schedule")
	assert.Equal(t, schedulewindow.Definition{
		Type:       schedulewindow.TypeRecurring,
		Timezone:   "America/Chicago",
		StartDate:  "2026-06-01",
		StartTime:  "22:00",
		EndTime:    "06:00",
		Frequency:  schedulewindow.FrequencyWeekly,
		DaysOfWeek: []time.Weekday{time.Monday, time.Friday},
	}, rule.Schedule.Definition)

	out := toAutomationRuleProto(&rule)
	assert.Equal(t, pb.CurtailmentAutomationTriggerType_CURTAILMENT_AUTOMATION_TRIGGER_TYPE_SCHEDULE, out.GetTriggerType())
	assert.True(t, proto.Equal(schedule, out.GetSchedule()), "schedule must round-trip: %v", out.GetSchedule())
	require.NotNil(t, out.GetWindowStartAt())
	require.NotNil(t, out.GetWindowEndAt())
	assert.Equal(t, 8*time.Hour, out.GetWindowEndAt().AsTime().Sub(out.GetWindowStartAt().AsTime()))
}

func TestAutomationScheduleUpdateRequest(t *testing.T) {
	t.Parallel()
	rule := automationRuleFromUpdateRequest(42, 502, &pb.UpdateCurtailmentAutomationRuleRequest{
		RuleId:            9,
		RuleName:          "Weeknight peak",
		TriggerType:       pb.CurtailmentAutomationTriggerType_CURTAILMENT_AUTOMATION_TRIGGER_TYPE_SCHEDULE,
		Schedule:          weeknightScheduleProto(),
		ResponseProfileId: 3001,
	})

	assert.Equal(t, int64(9), rule.ID)
	assert.Equal(t, models.AutomationTriggerTypeSchedule, rule.TriggerType)
	assert.Equal(t, int64(0), rule.MQTTSourceID)
	require.NotNil(t, rule.Schedule)
	assert.Equal(t, int64(502), rule.Schedule.ServiceUserID, "the updating user runs the schedule")
	assert.Equal(t, schedulewindow.Definition{
		Type:       schedulewindow.TypeRecurring,
		Timezone:   "America/Chicago",
		StartDate:  "2026-06-01",
		StartTime:  "22:00",
		EndTime:    "06:00",
		Frequency:  schedulewindow.FrequencyWeekly,
		DaysOfWeek: []time.Weekday{time.Monday, time.Friday},
	}, rule.Schedule.Definition)
}

func TestSetAutomationScheduleWindow(t *testing.T) {
	t.Parallel()
	schedule := &models.AutomationSchedule{Definition: schedulewindow.Definition{
		Type: schedulewindow.TypeOneTime, Timezone: "UTC",
		StartDate: "2026-06-11", StartTime: "20:00", EndDate: "2026-06-12", EndTime: "02:00",
	}}
	start := time.Date(2026, 6, 11, 20, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 12, 2, 0, 0, 0, time.UTC)

	for _, now := range []time.Time{start.Add(-time.Hour), start.Add(time.Hour)} {
		out := &pb.CurtailmentAutomationRule{}
		setAutomationScheduleWindow(out, schedule, now)
		require.NotNil(t, out.GetWindowStartAt(), now)
		assert.True(t, out.GetWindowStartAt().AsTime().Equal(start))
		assert.True(t, out.GetWindowEndAt().AsTime().Equal(end))
	}

	out := &pb.CurtailmentAutomationRule{}
	setAutomationScheduleWindow(out, schedule, end)
	assert.Nil(t, out.GetWindowStartAt(), "a finished one-time schedule has no window")
	assert.Nil(t, out.GetWindowEndAt())
}

func TestCreateAutomationRuleRequestTriggerValidation(t *testing.T) {
	t.Parallel()
	schedule := pb.CurtailmentAutomationTriggerType_CURTAILMENT_AUTOMATION_TRIGGER_TYPE_SCHEDULE
	mqtt := pb.CurtailmentAutomationTriggerType_CURTAILMENT_AUTOMATION_TRIGGER_TYPE_MQTT
	oneTime := &pb.CurtailmentSchedule{
		ScheduleType: pb.CurtailmentScheduleType_CURTAILMENT_SCHEDULE_TYPE_ONE_TIME,
		Timezone:     "UTC", StartDate: "2026-06-11", StartTime: "20:00", EndTime: "22:00",
	}
	monthlyWithDays := weeknightScheduleProto()
	monthlyWithDays.Frequency = pb.CurtailmentScheduleFrequency_CURTAILMENT_SCHEDULE_FREQUENCY_MONTHLY
	monthlyWithDays.DayOfMonth = 15

	tests := []struct {
		name string
		req  *pb.CreateCurtailmentAutomationRuleRequest
		ok   bool
	}{
		{"schedule rule", &pb.CreateCurtailmentAutomationRuleRequest{TriggerType: schedule, Schedule: weeknightScheduleProto()}, true},
		{"mqtt rule", &pb.CreateCurtailmentAutomationRuleRequest{TriggerType: mqtt, MqttSourceId: 7}, true},
		{"schedule rule with source", &pb.CreateCurtailmentAutomationRuleRequest{TriggerType: schedule, Schedule: weeknightScheduleProto(), MqttSourceId: 7}, false},
		{"schedule rule without schedule", &pb.CreateCurtailmentAutomationRuleRequest{TriggerType: schedule}, false},
		{"mqtt rule without source", &pb.CreateCurtailmentAutomationRuleRequest{TriggerType: mqtt}, false},
		{"mqtt rule with schedule", &pb.CreateCurtailmentAutomationRuleRequest{TriggerType: mqtt, MqttSourceId: 7, Schedule: weeknightScheduleProto()}, false},
		{"one-time schedule without end date", &pb.CreateCurtailmentAutomationRuleRequest{TriggerType: schedule, Schedule: oneTime}, false},
		{"monthly schedule with weekdays", &pb.CreateCurtailmentAutomationRuleRequest{TriggerType: schedule, Schedule: monthlyWithDays}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.req.RuleName = "rule"
			tt.req.ResponseProfileId = 3001
			tt.req.ExpectedResponseProfileRevision = uuid.NewString()
			err := protovalidate.Validate(tt.req)
			if tt.ok {
				assert.NoError(t, err)
				return
			}
			assert.Error(t, err)
		})
	}
}

func TestUpdateAutomationRuleRequestTriggerValidation(t *testing.T) {
	t.Parallel()
	schedule := pb.CurtailmentAutomationTriggerType_CURTAILMENT_AUTOMATION_TRIGGER_TYPE_SCHEDULE
	mqtt := pb.CurtailmentAutomationTriggerType_CURTAILMENT_AUTOMATION_TRIGGER_TYPE_MQTT
	tests := []struct {
		name string
		req  *pb.UpdateCurtailmentAutomationRuleRequest
		ok   bool
	}{
		{"schedule rule", &pb.UpdateCurtailmentAutomationRuleRequest{TriggerType: schedule, Schedule: weeknightScheduleProto()}, true},
		{"mqtt rule", &pb.UpdateCurtailmentAutomationRuleRequest{TriggerType: mqtt, MqttSourceId: 7}, true},
		{"schedule rule with source", &pb.UpdateCurtailmentAutomationRuleRequest{TriggerType: schedule, Schedule: weeknightScheduleProto(), MqttSourceId: 7}, false},
		{"schedule rule without schedule", &pb.UpdateCurtailmentAutomationRuleRequest{TriggerType: schedule}, false},
		{"mqtt rule without source", &pb.UpdateCurtailmentAutomationRuleRequest{TriggerType: mqtt}, false},
		{"mqtt rule with schedule", &pb.UpdateCurtailmentAutomationRuleRequest{TriggerType: mqtt, MqttSourceId: 7, Schedule: weeknightScheduleProto()}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.req.RuleId = 9
			tt.req.RuleName = "rule"
			tt.req.ResponseProfileId = 3001
			tt.req.ExpectedResponseProfileRevision = uuid.NewString()
			err := protovalidate.Validate(tt.req)
			if tt.ok {
				assert.NoError(t, err)
				return
			}
			assert.Error(t, err)
		})
	}
}

func TestToEventProtoSourceActorType(t *testing.T) {
	t.Parallel()
	tests := map[models.SourceActorType]pb.CurtailmentSourceActorType{
		models.SourceActorUser:       pb.CurtailmentSourceActorType_CURTAILMENT_SOURCE_ACTOR_TYPE_USER,
		models.SourceActorAPIKey:     pb.CurtailmentSourceActorType_CURTAILMENT_SOURCE_ACTOR_TYPE_API_KEY,
		models.SourceActorWebhook:    pb.CurtailmentSourceActorType_CURTAILMENT_SOURCE_ACTOR_TYPE_WEBHOOK,
		models.SourceActorScheduler:  pb.CurtailmentSourceActorType_CURTAILMENT_SOURCE_ACTOR_TYPE_SCHEDULER,
		models.SourceActorAutomation: pb.CurtailmentSourceActorType_CURTAILMENT_SOURCE_ACTOR_TYPE_AUTOMATION,
	}
	for actor, want := range tests {
		out := toEventProto(&models.Event{EventUUID: uuid.New(), SourceActorType: actor})
		assert.Equal(t, want, out.GetSourceActorType(), actor)
	}
}
