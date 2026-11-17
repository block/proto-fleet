package hostedaccounts

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestReportingInputConversionPreservesExactIDsDecimalsAndTimes(t *testing.T) {
	now := time.Date(2026, 11, 1, 3, 4, 5, 123456789, time.UTC)
	largeID := int64(1<<60) + 123
	values := []any{
		reporting.Order{ID: "order", Name: "Hosting", Start: now},
		reporting.ModuleCoverageSegment{ID: "module", ModuleID: largeID, SiteID: largeID + 2, ContainerID: largeID + 3, OrderID: "order", Start: now, End: now.Add(time.Hour), BoardCoverageIDs: []string{"b1", "b2", "b3"}, WholeModule: true, SourceRef: "reviewed"},
		reporting.CoverageSegment{ID: "coverage", AllocationIntervalID: largeID, AssetID: largeID + 1, Kind: 6, SiteID: largeID + 2, ContainerID: largeID + 3, Start: now, CapacityTHS: "12345678901234567890.000000000000000001"},
		reporting.MeterInterval{ID: "meter", KWh: "123.000000000000000001", Start: now, End: now.Add(time.Hour)},
		reporting.FeeLine{ID: "fee", Start: now},
		reporting.Incident{ID: "incident", Start: now},
		reporting.HashrateInterval{ID: "hashrate", Start: now},
		reporting.PoolObservation{ID: "pool", SourceAt: now, ReceivedAt: now.Add(time.Second)},
		reporting.ObligationInterval{ID: "obligation", AssetID: largeID, Start: now, ReceivedAt: now, DiagnosedAt: now.Add(time.Second), RestoredAt: now.Add(time.Minute)},
		reporting.ContractResult{ID: "external", Start: now},
	}
	for _, value := range values {
		fact, err := reporting.EncodeFact(value)
		if err != nil {
			t.Fatal(err)
		}
		dto, err := reportingFactDTO(fact)
		if err != nil {
			t.Fatal(err)
		}
		got, err := reportingFactDomain(dto)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, fact) {
			t.Fatalf("typed conversion changed native %T: got %#v want %#v", value, got, fact)
		}
	}
	config := reporting.Configuration{Timezone: "Africa/Nairobi", ReportingYearMonth: 1, ReportingYearDay: 1, Representatives: []reporting.Representative{{UserID: "9fec2e20-51a8-43c9-8936-826655c363f0", CanSubmit: true, SourceRef: "authority"}}}
	d := &reportingDecoder{}
	got := decodeReportingConfiguration(reportingConfigurationDTO(config), d)
	if d.err != nil || !reflect.DeepEqual(got, config) {
		t.Fatalf("configuration changed: %#v, %v", got, d.err)
	}
}

func TestReportingConversionRejectsMalformedNestedTimestamps(t *testing.T) {
	invalid := &timestamppb.Timestamp{Seconds: 1 << 62}
	for _, fact := range []*pb.ReportingFact{
		{Value: &pb.ReportingFact_Obligation{Obligation: &pb.ReportingObligationInterval{RestoredAt: invalid}}},
		{Value: &pb.ReportingFact_ModuleCoverage{ModuleCoverage: &pb.ReportingModuleCoverageSegment{End: invalid}}},
	} {
		if _, err := reportingFactDomain(fact); err == nil {
			t.Fatal("malformed nested timestamp accepted")
		}
	}
	if _, err := reportingPeriodDomain(&pb.ReportingPeriod{Start: invalid}); err == nil {
		t.Fatal("malformed report period accepted")
	}
}

func TestClientReportDTOExcludesDraftAndHostMetadata(t *testing.T) {
	record := domain.ReportRecord{ReportID: "public-report", Status: "draft", CreatedBy: 918273, OwnerID: 817263, InputRevision: 123, Deliveries: []domain.DeliveryEvidence{{Evidence: "private-delivery-evidence"}}}
	if accountReportDTO(record) != nil || accountReportSummaryDTO(record) != nil {
		t.Fatal("draft entered client projection")
	}
	record.Status = "issued"
	record.Result.Obligations = []reporting.ObligationDetail{{TicketRef: "private-host-ticket"}}
	payload, err := protojson.Marshal(accountReportDTO(record))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"918273", "817263", "private-delivery-evidence", "private-host-ticket", "createdBy", "ownerId", "inputRevision"} {
		if strings.Contains(string(payload), secret) {
			t.Fatalf("host metadata %q leaked in client report", secret)
		}
	}
	summary := accountReportSummaryDTO(record)
	if summary.ProtoReflect().Descriptor().Fields().ByName("result") != nil {
		t.Fatal("list summary carries frozen result/evidence payload")
	}
}

func TestHostReportActionsRequireCreationPermissionAndDraft(t *testing.T) {
	record := domain.ReportRecord{Status: "draft", CreatedBy: 42}
	setup := domain.ReportingSetup{CanCreate: true}
	if !hostReportDTO(record, setup).CanPublish {
		t.Fatal("creator lacks publication action for draft")
	}
	record.Status = "issued"
	if hostReportDTO(record, setup).CanPublish {
		t.Fatal("published report offered publication again")
	}
	record.Status = "draft"
	setup = domain.ReportingSetup{CanAdminister: true}
	if hostReportDTO(record, setup).CanPublish {
		t.Fatal("administration alone offered publication")
	}
}

func TestReportingExportValidationKeepsSourceInputsHostOnly(t *testing.T) {
	const id = "9fec2e20-51a8-43c9-8936-826655c363f0"
	if err := protovalidate.Validate(&pb.ExportHostReportRequest{AccountId: id, ReportId: id, Name: "source-inputs.json"}); err != nil {
		t.Fatalf("host source download rejected: %v", err)
	}
	if err := protovalidate.Validate(&pb.ExportAccountReportRequest{AccountId: id, ReportId: id, Name: "source-inputs.json"}); err == nil {
		t.Fatal("client export accepted host-only source inputs")
	}
}

func TestReportingRequestValidationAdmitsBoardsAndKeepsRepairUnitsExplicit(t *testing.T) {
	at := timestamppb.New(time.Now())
	for _, kind := range []int32{5, 6, 7} {
		facts := []proto.Message{
			&pb.ReportingCoverageSegment{Kind: kind, Start: at},
			&pb.ReportingHashrateInterval{Kind: kind, Start: at, End: at},
			&pb.ReportingPoolObservation{Kind: kind, SourceAt: at, ReceivedAt: at, State: "unknown"},
		}
		for _, fact := range facts {
			if err := protovalidate.Validate(fact); err != nil {
				t.Fatalf("kind %d: %v", kind, err)
			}
		}
	}
	repair := &pb.ReportingObligationInterval{Kind: 6, Start: at, AsOf: at, ReceivedAt: at}
	if err := protovalidate.Validate(repair); err != nil {
		t.Fatal(err)
	}
	repair.Kind = 7
	if err := protovalidate.Validate(repair); err == nil {
		t.Fatal("board accepted as a module repair unit")
	}
	mapping := &pb.ReportingModuleCoverageSegment{ModuleId: 1, SiteId: 1, ContainerId: 1, Start: at, End: at, BoardCoverageIds: []string{"a", "b", "c"}, WholeModule: true}
	if err := protovalidate.Validate(mapping); err != nil {
		t.Fatal(err)
	}
	mapping.BoardCoverageIds = []string{"a", "a"}
	if err := protovalidate.Validate(mapping); err == nil {
		t.Fatal("duplicate mapping reference accepted")
	}
}

func TestReportingInputsContractUsesBoundedOperations(t *testing.T) {
	service := pb.File_hosted_v1_hosted_proto.Services().ByName("HostedAccountService")
	for _, method := range []string{"MutateHostReportingInputs", "ListHostReportingFacts"} {
		if service.Methods().ByName(protoreflect.Name(method)) == nil {
			t.Fatalf("bounded reporting method %s is missing", method)
		}
	}
	if service.Methods().ByName("SaveHostReportingInputs") != nil {
		t.Fatal("whole-account replacement API is still exposed")
	}
	metadata := pb.File_hosted_v1_reporting_proto.Messages().ByName("HostReportingInputsMetadata")
	if metadata == nil {
		t.Fatal("input metadata contract is missing")
	}
	if metadata.Fields().ByName("inputs") != nil {
		t.Fatal("metadata embeds complete account facts")
	}
	for _, field := range []string{"configuration", "revision", "register_counts"} {
		if metadata.Fields().ByName(protoreflect.Name(field)) == nil {
			t.Fatalf("input metadata lacks %s", field)
		}
	}
}
