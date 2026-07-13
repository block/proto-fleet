package cohort

import (
	"encoding/json"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/cohort/v1"
	"github.com/block/proto-fleet/server/internal/domain/cohort/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

func TestToCreateCohortParams_DeviceIdentifierInitialMembers(t *testing.T) {
	req := &pb.CreateCohortRequest{
		Label:   "reservation",
		Purpose: "test",
		InitialMembers: &pb.CreateCohortRequest_DeviceIdentifiers{
			DeviceIdentifiers: &pb.CohortDeviceIdentifierList{
				DeviceIdentifiers: []string{"miner-1", "miner-2"},
			},
		},
	}

	params, err := toCreateCohortParams(req, testSessionInfo())
	if err != nil {
		t.Fatalf("toCreateCohortParams returned error: %v", err)
	}

	if got, want := params.DeviceIdentifiers, []string{"miner-1", "miner-2"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("DeviceIdentifiers = %v, want %v", got, want)
	}
}

func TestToCreateCohortParams_SourceDeviceSetInitialMembers(t *testing.T) {
	req := &pb.CreateCohortRequest{
		Label:   "reservation",
		Purpose: "test",
		InitialMembers: &pb.CreateCohortRequest_SourceDeviceSetId{
			SourceDeviceSetId: 42,
		},
	}

	params, err := toCreateCohortParams(req, testSessionInfo())
	if err != nil {
		t.Fatalf("toCreateCohortParams returned error: %v", err)
	}
	if params.SourceDeviceSetID == nil || *params.SourceDeviceSetID != 42 {
		t.Fatalf("SourceDeviceSetID = %v, want 42", params.SourceDeviceSetID)
	}
}

func TestToCreateCohortParams_SelectInitialMembers(t *testing.T) {
	product := "TestCorp"
	model := "TestMiner"
	req := &pb.CreateCohortRequest{
		Label:   "reservation",
		Purpose: "test",
		InitialMembers: &pb.CreateCohortRequest_Select{
			Select: &pb.CohortDeviceSelector{
				Count:   3,
				Product: &product,
				Model:   &model,
			},
		},
	}

	params, err := toCreateCohortParams(req, testSessionInfo())
	if err != nil {
		t.Fatalf("toCreateCohortParams returned error: %v", err)
	}
	if params.DeviceSelector == nil {
		t.Fatal("DeviceSelector = nil, want populated selector")
	}
	if params.DeviceSelector.Count != 3 {
		t.Fatalf("selector count = %d, want 3", params.DeviceSelector.Count)
	}
	if params.DeviceSelector.Product == nil || *params.DeviceSelector.Product != product {
		t.Fatalf("selector product = %v, want %q", params.DeviceSelector.Product, product)
	}
	if params.DeviceSelector.Model == nil || *params.DeviceSelector.Model != model {
		t.Fatalf("selector model = %v, want %q", params.DeviceSelector.Model, model)
	}
}

func TestToUpdateCohortParams_PreservesPatchPresence(t *testing.T) {
	label := "new label"
	purpose := "new purpose"
	firmwareFileID := ""
	expiresAt := timestamppb.New(time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC))
	desiredConfig := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"profile": structpb.NewStringValue("quiet"),
		},
	}
	req := &pb.UpdateCohortRequest{
		CohortId:              7,
		Label:                 &label,
		Purpose:               &purpose,
		ExpiresAt:             expiresAt,
		DesiredFirmwareFileId: &firmwareFileID,
		DesiredConfig:         desiredConfig,
	}

	params, err := toUpdateCohortParams(req, 99)
	if err != nil {
		t.Fatalf("toUpdateCohortParams returned error: %v", err)
	}

	if params.OrgID != 99 || params.CohortID != 7 {
		t.Fatalf("ids = org %d cohort %d, want org 99 cohort 7", params.OrgID, params.CohortID)
	}
	if params.Label == nil || *params.Label != label {
		t.Fatalf("Label = %v, want %q", params.Label, label)
	}
	if params.Purpose == nil || *params.Purpose != purpose {
		t.Fatalf("Purpose = %v, want %q", params.Purpose, purpose)
	}
	if params.ExpiresAt == nil || !params.ExpiresAt.Equal(expiresAt.AsTime()) {
		t.Fatalf("ExpiresAt = %v, want %v", params.ExpiresAt, expiresAt.AsTime())
	}
	if !params.DesiredFirmwareFileIDSet || params.DesiredFirmwareFileID == nil || *params.DesiredFirmwareFileID != "" {
		t.Fatalf("DesiredFirmwareFileID presence/value = %v/%v, want set empty string", params.DesiredFirmwareFileIDSet, params.DesiredFirmwareFileID)
	}
	if !params.DesiredConfigJSONSet || len(params.DesiredConfigJSON) == 0 {
		t.Fatalf("DesiredConfigJSON presence/value = %v/%s, want populated JSON", params.DesiredConfigJSONSet, params.DesiredConfigJSON)
	}
}

func TestToUpdateCohortParams_ClearFlags(t *testing.T) {
	req := &pb.UpdateCohortRequest{
		CohortId:           7,
		ClearExpiresAt:     true,
		ClearDesiredConfig: true,
	}

	params, err := toUpdateCohortParams(req, 99)
	if err != nil {
		t.Fatalf("toUpdateCohortParams returned error: %v", err)
	}

	if !params.ClearExpiresAt || !params.ClearDesiredConfig {
		t.Fatalf("clear flags = expires_at:%v desired_config:%v, want both true", params.ClearExpiresAt, params.ClearDesiredConfig)
	}
	if params.ExpiresAt != nil || params.DesiredConfigJSONSet || len(params.DesiredConfigJSON) != 0 {
		t.Fatalf("clear request unexpectedly carried set values: expires_at=%v desired_config_set=%v desired_config=%s", params.ExpiresAt, params.DesiredConfigJSONSet, params.DesiredConfigJSON)
	}
}

func TestToProtoCohort_ComposesSummaryAndMembers(t *testing.T) {
	ownerID := int64(11)
	ownerUsername := "owner"
	firmwareFileID := "fw-1"
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	cohort := &models.Cohort{
		ID:                    7,
		OrgID:                 99,
		Label:                 "reservation",
		IsDefault:             false,
		OwnerUserID:           &ownerID,
		OwnerUsername:         &ownerUsername,
		ExpiresAt:             &now,
		DesiredFirmwareFileID: &firmwareFileID,
		DesiredConfigJSON:     json.RawMessage(`{"profile":"quiet"}`),
		State:                 models.CohortStateActive,
		Purpose:               "test",
		SourceActorType:       models.SourceActorUser,
		SourceActorID:         stringPtr("session-1"),
		IdempotencyKey:        stringPtr("idem-1"),
		CreatedAt:             now,
		UpdatedAt:             now,
		ExplicitMemberCount:   1,
		Members: []models.CohortMember{{
			CohortID:         7,
			OrgID:            99,
			DeviceIdentifier: "miner-1",
			AddedAt:          now,
			Display: models.CohortDeviceDisplay{
				Name:         "Rig A",
				WorkerName:   "worker-a",
				Manufacturer: "TestCorp",
				Model:        "TestMiner",
				IPAddress:    "127.0.0.1",
				SerialNumber: "SN-A",
			},
		}},
	}

	got := toProtoCohort(cohort)
	if got.GetSummary().GetId() != 7 || got.GetSummary().GetExplicitMemberCount() != 1 {
		t.Fatalf("summary = %+v, want id 7 and explicit_member_count 1", got.GetSummary())
	}
	if len(got.GetMembers()) != 1 || got.GetMembers()[0].GetDeviceIdentifier() != "miner-1" {
		t.Fatalf("members = %+v, want miner-1", got.GetMembers())
	}
	if got.GetMembers()[0].GetDisplay().GetName() != "Rig A" {
		t.Fatalf("member display = %+v, want Rig A", got.GetMembers()[0].GetDisplay())
	}
}

func testSessionInfo() *session.Info {
	return &session.Info{
		UserID:         1,
		OrganizationID: 2,
		Username:       "operator",
		SessionID:      "session-1",
	}
}

func stringPtr(s string) *string {
	return &s
}
