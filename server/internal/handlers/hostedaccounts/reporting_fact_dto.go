package hostedaccounts

import (
	"fmt"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
)

var reportingFactKinds = []reporting.FactKind{reporting.FactOrder, reporting.FactCoverage, reporting.FactModuleCoverage, reporting.FactMeter, reporting.FactFee, reporting.FactIncident, reporting.FactHashrate, reporting.FactPoolObservation, reporting.FactObligation, reporting.FactExternalResult}

func reportingFactKindDTO(kind reporting.FactKind) pb.ReportingFactKind {
	switch kind {
	case reporting.FactOrder:
		return pb.ReportingFactKind_REPORTING_FACT_KIND_ORDER
	case reporting.FactCoverage:
		return pb.ReportingFactKind_REPORTING_FACT_KIND_COVERAGE
	case reporting.FactModuleCoverage:
		return pb.ReportingFactKind_REPORTING_FACT_KIND_MODULE_COVERAGE
	case reporting.FactMeter:
		return pb.ReportingFactKind_REPORTING_FACT_KIND_METER
	case reporting.FactFee:
		return pb.ReportingFactKind_REPORTING_FACT_KIND_FEE
	case reporting.FactIncident:
		return pb.ReportingFactKind_REPORTING_FACT_KIND_INCIDENT
	case reporting.FactHashrate:
		return pb.ReportingFactKind_REPORTING_FACT_KIND_HASHRATE
	case reporting.FactPoolObservation:
		return pb.ReportingFactKind_REPORTING_FACT_KIND_POOL_OBSERVATION
	case reporting.FactObligation:
		return pb.ReportingFactKind_REPORTING_FACT_KIND_OBLIGATION
	case reporting.FactExternalResult:
		return pb.ReportingFactKind_REPORTING_FACT_KIND_EXTERNAL_RESULT

	default:
		return pb.ReportingFactKind_REPORTING_FACT_KIND_UNSPECIFIED
	}
}
func reportingFactKindDomain(kind pb.ReportingFactKind) (reporting.FactKind, error) {
	switch kind {
	case pb.ReportingFactKind_REPORTING_FACT_KIND_UNSPECIFIED:
		return "", fleeterror.NewInvalidArgumentError("invalid reporting register")
	case pb.ReportingFactKind_REPORTING_FACT_KIND_ORDER:
		return reporting.FactOrder, nil
	case pb.ReportingFactKind_REPORTING_FACT_KIND_COVERAGE:
		return reporting.FactCoverage, nil
	case pb.ReportingFactKind_REPORTING_FACT_KIND_MODULE_COVERAGE:
		return reporting.FactModuleCoverage, nil
	case pb.ReportingFactKind_REPORTING_FACT_KIND_METER:
		return reporting.FactMeter, nil
	case pb.ReportingFactKind_REPORTING_FACT_KIND_FEE:
		return reporting.FactFee, nil
	case pb.ReportingFactKind_REPORTING_FACT_KIND_INCIDENT:
		return reporting.FactIncident, nil
	case pb.ReportingFactKind_REPORTING_FACT_KIND_HASHRATE:
		return reporting.FactHashrate, nil
	case pb.ReportingFactKind_REPORTING_FACT_KIND_POOL_OBSERVATION:
		return reporting.FactPoolObservation, nil
	case pb.ReportingFactKind_REPORTING_FACT_KIND_OBLIGATION:
		return reporting.FactObligation, nil
	case pb.ReportingFactKind_REPORTING_FACT_KIND_EXTERNAL_RESULT:
		return reporting.FactExternalResult, nil

	default:
		return "", fleeterror.NewInvalidArgumentError("invalid reporting register")
	}
}

func reportingFactDomain(m *pb.ReportingFact) (reporting.Fact, error) {
	if m == nil {
		return reporting.Fact{}, fleeterror.NewInvalidArgumentError("reporting fact is required")
	}
	d := &reportingDecoder{}
	var value any
	switch v := m.Value.(type) {
	case *pb.ReportingFact_Order:
		value = decodeReportingOrder(v.Order, d)
	case *pb.ReportingFact_Coverage:
		value = decodeReportingCoverageSegment(v.Coverage, d)
	case *pb.ReportingFact_ModuleCoverage:
		value = decodeReportingModuleCoverage(v.ModuleCoverage, d)
	case *pb.ReportingFact_Meter:
		value = decodeReportingMeterInterval(v.Meter, d)
	case *pb.ReportingFact_Fee:
		value = decodeReportingFeeLine(v.Fee, d)
	case *pb.ReportingFact_Incident:
		value = decodeReportingIncident(v.Incident, d)
	case *pb.ReportingFact_Hashrate:
		value = decodeReportingHashrateInterval(v.Hashrate, d)
	case *pb.ReportingFact_PoolObservation:
		value = decodeReportingPoolObservation(v.PoolObservation, d)
	case *pb.ReportingFact_Obligation:
		value = decodeReportingObligationInterval(v.Obligation, d)
	case *pb.ReportingFact_ExternalResult:
		value = decodeReportingContractResult(v.ExternalResult, d)

	default:
		return reporting.Fact{}, fleeterror.NewInvalidArgumentError("typed reporting fact is required")
	}
	if d.err != nil {
		return reporting.Fact{}, d.err
	}
	f, err := reporting.EncodeFact(value)
	if err != nil {
		return f, fleeterror.NewInvalidArgumentError(err.Error())
	}
	return f, nil
}
func reportingFactDTO(f reporting.Fact) (*pb.ReportingFact, error) {
	_, value, err := reporting.DecodeFact(f.Kind, f.Payload)
	if err != nil {
		return nil, err
	}
	out := &pb.ReportingFact{}
	switch v := value.(type) {
	case reporting.Order:
		out.Value = &pb.ReportingFact_Order{Order: reportingOrderDTO(v)}
	case reporting.CoverageSegment:
		out.Value = &pb.ReportingFact_Coverage{Coverage: reportingCoverageSegmentDTO(v)}
	case reporting.ModuleCoverageSegment:
		out.Value = &pb.ReportingFact_ModuleCoverage{ModuleCoverage: reportingModuleCoverageDTO(v)}
	case reporting.MeterInterval:
		out.Value = &pb.ReportingFact_Meter{Meter: reportingMeterIntervalDTO(v)}
	case reporting.FeeLine:
		out.Value = &pb.ReportingFact_Fee{Fee: reportingFeeLineDTO(v)}
	case reporting.Incident:
		out.Value = &pb.ReportingFact_Incident{Incident: reportingIncidentDTO(v)}
	case reporting.HashrateInterval:
		out.Value = &pb.ReportingFact_Hashrate{Hashrate: reportingHashrateIntervalDTO(v)}
	case reporting.PoolObservation:
		out.Value = &pb.ReportingFact_PoolObservation{PoolObservation: reportingPoolObservationDTO(v)}
	case reporting.ObligationInterval:
		out.Value = &pb.ReportingFact_Obligation{Obligation: reportingObligationIntervalDTO(v)}
	case reporting.ContractResult:
		out.Value = &pb.ReportingFact_ExternalResult{ExternalResult: reportingContractResultDTO(v)}

	default:
		return nil, fmt.Errorf("unknown stored reporting fact type %T", value)
	}
	return out, nil
}
func reportingFactMutationDomain(m *pb.ReportingFactMutation) (reporting.FactMutation, error) {
	var out reporting.FactMutation
	if m == nil {
		return out, fleeterror.NewInvalidArgumentError("reporting mutation is required")
	}
	kind, err := reportingFactKindDomain(m.Kind)
	if err != nil {
		return out, err
	}
	out.Kind, out.ID = kind, m.Id
	switch change := m.Change.(type) {
	case *pb.ReportingFactMutation_Upsert:
		f, err := reportingFactDomain(change.Upsert)
		if err != nil {
			return out, err
		}
		if f.Kind != kind || f.ID != m.Id {
			return out, fleeterror.NewInvalidArgumentError("reporting register and identity must match the typed payload")
		}
		out.Payload = f.Payload
	case *pb.ReportingFactMutation_Delete:
		if !change.Delete {
			return out, fleeterror.NewInvalidArgumentError("reporting deletion must be true")
		}
		out.Delete = true
	default:
		return out, fleeterror.NewInvalidArgumentError("reporting mutation operation is required")
	}
	return out, nil
}
