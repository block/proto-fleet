package hostedaccounts

import (
	"math"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func reportingTimeDTO(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}
func reportingOptionalTimeDTO(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return reportingTimeDTO(*t)
}

// Check timestamp structure before AsTime, which otherwise normalizes invalid values.
type reportingDecoder struct{ err error }

func (d *reportingDecoder) time(t *timestamppb.Timestamp) time.Time {
	if t == nil {
		return time.Time{}
	}
	if err := t.CheckValid(); err != nil {
		d.err = fleeterror.NewInvalidArgumentError("invalid reporting timestamp")
		return time.Time{}
	}
	return t.AsTime()
}
func (d *reportingDecoder) optionalTime(t *timestamppb.Timestamp) *time.Time {
	if t == nil {
		return nil
	}
	v := d.time(t)
	return &v
}
func reportingPeriodDomain(m *pb.ReportingPeriod) (reporting.Period, error) {
	d := &reportingDecoder{}
	v := decodeReportingPeriod(m, d)
	return v, d.err
}

func reportingPeriodDTO(v reporting.Period) *pb.ReportingPeriod {
	out := &pb.ReportingPeriod{}
	out.RequestReceivedAt = reportingTimeDTO(v.RequestReceivedAt)
	out.RequestRef = v.RequestRef
	out.Kind = v.Kind
	out.Start = reportingTimeDTO(v.Start)
	out.End = reportingTimeDTO(v.End)
	out.AsOf = reportingTimeDTO(v.AsOf)
	out.Timezone = v.Timezone
	return out
}
func decodeReportingPeriod(m *pb.ReportingPeriod, d *reportingDecoder) reporting.Period {
	if m == nil {
		return reporting.Period{}
	}
	out := reporting.Period{}
	out.RequestReceivedAt = d.time(m.RequestReceivedAt)
	out.RequestRef = m.RequestRef
	out.Kind = m.Kind
	out.Start = d.time(m.Start)
	out.End = d.time(m.End)
	out.AsOf = d.time(m.AsOf)
	out.Timezone = m.Timezone
	return out
}
func reportingPrerequisiteDTO(v reporting.Prerequisite) *pb.ReportingPrerequisite {
	out := &pb.ReportingPrerequisite{}
	out.Key = v.Key
	out.Owner = v.Owner
	out.Ready = v.Ready
	out.SourceRef = v.SourceRef
	return out
}
func decodeReportingPrerequisite(m *pb.ReportingPrerequisite, d *reportingDecoder) reporting.Prerequisite {
	if m == nil {
		return reporting.Prerequisite{}
	}
	out := reporting.Prerequisite{}
	out.Key = m.Key
	out.Owner = m.Owner
	out.Ready = m.Ready
	out.SourceRef = m.SourceRef
	return out
}
func reportingSourceDTO(v reporting.Source) *pb.ReportingSource {
	out := &pb.ReportingSource{}
	out.Id = v.ID
	out.Metric = v.Metric
	out.Certified = v.Certified
	out.SourceRef = v.SourceRef
	return out
}
func decodeReportingSource(m *pb.ReportingSource, d *reportingDecoder) reporting.Source {
	if m == nil {
		return reporting.Source{}
	}
	out := reporting.Source{}
	out.ID = m.Id
	out.Metric = m.Metric
	out.Certified = m.Certified
	out.SourceRef = m.SourceRef
	return out
}
func reportingRegisterCompletenessDTO(v reporting.RegisterCompleteness) *pb.ReportingRegisterCompleteness {
	out := &pb.ReportingRegisterCompleteness{}
	out.Kind = v.Kind
	out.Start = reportingTimeDTO(v.Start)
	out.End = reportingTimeDTO(v.End)
	out.SourceRef = v.SourceRef
	return out
}
func decodeReportingRegisterCompleteness(m *pb.ReportingRegisterCompleteness, d *reportingDecoder) reporting.RegisterCompleteness {
	if m == nil {
		return reporting.RegisterCompleteness{}
	}
	out := reporting.RegisterCompleteness{}
	out.Kind = m.Kind
	out.Start = d.time(m.Start)
	out.End = d.time(m.End)
	out.SourceRef = m.SourceRef
	return out
}
func reportingConfigurationDTO(v reporting.Configuration) *pb.ReportingConfiguration {
	out := &pb.ReportingConfiguration{}
	if v.Schedule != nil {
		out.Schedule = &pb.ReportingSchedule{OwnerUserId: v.Schedule.OwnerUserID, SourceRef: v.Schedule.SourceRef, MonthlyStart: reportingTimeDTO(v.Schedule.MonthlyStart), QuarterlyStart: reportingTimeDTO(v.Schedule.QuarterlyStart), AnnualStart: reportingTimeDTO(v.Schedule.AnnualStart)}
	}
	for _, x := range v.Registers {
		out.Registers = append(out.Registers, reportingRegisterCompletenessDTO(x))
	}
	out.ContractRef = v.ContractRef
	out.Timezone = v.Timezone
	out.ReportingYearMonth = reportingInt32(v.ReportingYearMonth)
	out.ReportingYearDay = reportingInt32(v.ReportingYearDay)
	out.Holidays = append([]string(nil), v.Holidays...)
	for _, x := range v.Prerequisites {
		out.Prerequisites = append(out.Prerequisites, reportingPrerequisiteDTO(x))
	}
	for _, x := range v.Sources {
		out.Sources = append(out.Sources, reportingSourceDTO(x))
	}
	out.DeliveryChannel = v.DeliveryChannel
	out.SharedAllocationRef = v.SharedAllocationRef
	for _, x := range v.Representatives {
		out.Representatives = append(out.Representatives, reportingRepresentativeDTO(x))
	}
	return out
}
func decodeReportingConfiguration(m *pb.ReportingConfiguration, d *reportingDecoder) reporting.Configuration {
	if m == nil {
		return reporting.Configuration{}
	}
	out := reporting.Configuration{}
	if m.Schedule != nil {
		out.Schedule = &reporting.Schedule{OwnerUserID: m.Schedule.OwnerUserId, SourceRef: m.Schedule.SourceRef, MonthlyStart: d.time(m.Schedule.MonthlyStart), QuarterlyStart: d.time(m.Schedule.QuarterlyStart), AnnualStart: d.time(m.Schedule.AnnualStart)}
	}
	for _, x := range m.Registers {
		out.Registers = append(out.Registers, decodeReportingRegisterCompleteness(x, d))
	}
	out.ContractRef = m.ContractRef
	out.Timezone = m.Timezone
	out.ReportingYearMonth = int(m.ReportingYearMonth)
	out.ReportingYearDay = int(m.ReportingYearDay)
	out.Holidays = append([]string(nil), m.Holidays...)
	for _, x := range m.Prerequisites {
		out.Prerequisites = append(out.Prerequisites, decodeReportingPrerequisite(x, d))
	}
	for _, x := range m.Sources {
		out.Sources = append(out.Sources, decodeReportingSource(x, d))
	}
	out.DeliveryChannel = m.DeliveryChannel
	out.SharedAllocationRef = m.SharedAllocationRef
	for _, x := range m.Representatives {
		out.Representatives = append(out.Representatives, decodeReportingRepresentative(x, d))
	}
	return out
}
func reportingRepresentativeDTO(v reporting.Representative) *pb.ReportingRepresentative {
	out := &pb.ReportingRepresentative{}
	out.UserId = v.UserID
	out.CanSubmit = v.CanSubmit
	out.SourceRef = v.SourceRef
	return out
}
func decodeReportingRepresentative(m *pb.ReportingRepresentative, d *reportingDecoder) reporting.Representative {
	if m == nil {
		return reporting.Representative{}
	}
	out := reporting.Representative{}
	out.UserID = m.UserId
	out.CanSubmit = m.CanSubmit
	out.SourceRef = m.SourceRef
	return out
}
func reportingOrderDTO(v reporting.Order) *pb.ReportingOrder {
	out := &pb.ReportingOrder{}
	out.Id = v.ID
	out.Name = v.Name
	out.Start = reportingTimeDTO(v.Start)
	out.End = reportingTimeDTO(v.End)
	out.SourceRef = v.SourceRef
	return out
}
func decodeReportingOrder(m *pb.ReportingOrder, d *reportingDecoder) reporting.Order {
	if m == nil {
		return reporting.Order{}
	}
	out := reporting.Order{}
	out.ID = m.Id
	out.Name = m.Name
	out.Start = d.time(m.Start)
	out.End = d.time(m.End)
	out.SourceRef = m.SourceRef
	return out
}
func reportingModuleCoverageDTO(v reporting.ModuleCoverageSegment) *pb.ReportingModuleCoverageSegment {
	return &pb.ReportingModuleCoverageSegment{Id: v.ID, ModuleId: v.ModuleID, SiteId: v.SiteID, ContainerId: v.ContainerID, OrderId: v.OrderID, Start: reportingTimeDTO(v.Start), End: reportingTimeDTO(v.End), BoardCoverageIds: append([]string(nil), v.BoardCoverageIDs...), WholeModule: v.WholeModule, SourceRef: v.SourceRef}
}
func decodeReportingModuleCoverage(m *pb.ReportingModuleCoverageSegment, d *reportingDecoder) reporting.ModuleCoverageSegment {
	if m == nil {
		return reporting.ModuleCoverageSegment{}
	}
	return reporting.ModuleCoverageSegment{ID: m.Id, ModuleID: m.ModuleId, SiteID: m.SiteId, ContainerID: m.ContainerId, OrderID: m.OrderId, Start: d.time(m.Start), End: d.time(m.End), BoardCoverageIDs: append([]string(nil), m.BoardCoverageIds...), WholeModule: m.WholeModule, SourceRef: m.SourceRef}
}

func reportingCoverageSegmentDTO(v reporting.CoverageSegment) *pb.ReportingCoverageSegment {
	out := &pb.ReportingCoverageSegment{}
	out.Id = v.ID
	out.AllocationIntervalId = v.AllocationIntervalID
	out.Kind = v.Kind
	out.AssetId = v.AssetID
	out.SiteId = v.SiteID
	out.ContainerId = v.ContainerID
	out.OrderId = v.OrderID
	out.Start = reportingTimeDTO(v.Start)
	out.End = reportingTimeDTO(v.End)
	out.CommissionedAt = reportingTimeDTO(v.CommissionedAt)
	out.CapacityThs = v.CapacityTHS
	out.ReplacementOf = v.ReplacementOf
	out.SourceRef = v.SourceRef
	return out
}
func decodeReportingCoverageSegment(m *pb.ReportingCoverageSegment, d *reportingDecoder) reporting.CoverageSegment {
	if m == nil {
		return reporting.CoverageSegment{}
	}
	out := reporting.CoverageSegment{}
	out.ID = m.Id
	out.AllocationIntervalID = m.AllocationIntervalId
	out.Kind = m.Kind
	out.AssetID = m.AssetId
	out.SiteID = m.SiteId
	out.ContainerID = m.ContainerId
	out.OrderID = m.OrderId
	out.Start = d.time(m.Start)
	out.End = d.time(m.End)
	out.CommissionedAt = d.time(m.CommissionedAt)
	out.CapacityTHS = m.CapacityThs
	out.ReplacementOf = m.ReplacementOf
	out.SourceRef = m.SourceRef
	return out
}
func reportingMeterIntervalDTO(v reporting.MeterInterval) *pb.ReportingMeterInterval {
	out := &pb.ReportingMeterInterval{}
	out.Id = v.ID
	out.MeterId = v.MeterID
	out.SourceId = v.SourceID
	out.SiteId = v.SiteID
	out.ContainerId = v.ContainerID
	out.OrderId = v.OrderID
	out.Start = reportingTimeDTO(v.Start)
	out.End = reportingTimeDTO(v.End)
	out.Kwh = v.KWh
	out.Quality = v.Quality
	out.SourceRef = v.SourceRef
	out.AllocationFraction = v.AllocationFraction
	return out
}
func decodeReportingMeterInterval(m *pb.ReportingMeterInterval, d *reportingDecoder) reporting.MeterInterval {
	if m == nil {
		return reporting.MeterInterval{}
	}
	out := reporting.MeterInterval{}
	out.ID = m.Id
	out.MeterID = m.MeterId
	out.SourceID = m.SourceId
	out.SiteID = m.SiteId
	out.ContainerID = m.ContainerId
	out.OrderID = m.OrderId
	out.Start = d.time(m.Start)
	out.End = d.time(m.End)
	out.KWh = m.Kwh
	out.Quality = m.Quality
	out.SourceRef = m.SourceRef
	out.AllocationFraction = m.AllocationFraction
	return out
}
func reportingFeeLineDTO(v reporting.FeeLine) *pb.ReportingFeeLine {
	out := &pb.ReportingFeeLine{}
	out.Id = v.ID
	out.SiteId = v.SiteID
	out.ContainerId = v.ContainerID
	out.OrderId = v.OrderID
	out.Start = reportingTimeDTO(v.Start)
	out.End = reportingTimeDTO(v.End)
	out.Amount = v.Amount
	out.Currency = v.Currency
	out.SourceRef = v.SourceRef
	return out
}
func decodeReportingFeeLine(m *pb.ReportingFeeLine, d *reportingDecoder) reporting.FeeLine {
	if m == nil {
		return reporting.FeeLine{}
	}
	out := reporting.FeeLine{}
	out.ID = m.Id
	out.SiteID = m.SiteId
	out.ContainerID = m.ContainerId
	out.OrderID = m.OrderId
	out.Start = d.time(m.Start)
	out.End = d.time(m.End)
	out.Amount = m.Amount
	out.Currency = m.Currency
	out.SourceRef = m.SourceRef
	return out
}
func reportingIncidentDTO(v reporting.Incident) *pb.ReportingIncident {
	out := &pb.ReportingIncident{}
	out.Id = v.ID
	out.SiteId = v.SiteID
	out.ContainerId = v.ContainerID
	out.OrderId = v.OrderID
	out.Start = reportingTimeDTO(v.Start)
	out.End = reportingTimeDTO(v.End)
	out.AsOf = reportingTimeDTO(v.AsOf)
	out.Category = v.Category
	out.Summary = v.Summary
	out.RootCause = v.RootCause
	out.ExclusionClause = v.ExclusionClause
	out.SourceRef = v.SourceRef
	return out
}
func decodeReportingIncident(m *pb.ReportingIncident, d *reportingDecoder) reporting.Incident {
	if m == nil {
		return reporting.Incident{}
	}
	out := reporting.Incident{}
	out.ID = m.Id
	out.SiteID = m.SiteId
	out.ContainerID = m.ContainerId
	out.OrderID = m.OrderId
	out.Start = d.time(m.Start)
	out.End = d.time(m.End)
	out.AsOf = d.time(m.AsOf)
	out.Category = m.Category
	out.Summary = m.Summary
	out.RootCause = m.RootCause
	out.ExclusionClause = m.ExclusionClause
	out.SourceRef = m.SourceRef
	return out
}
func reportingHashrateIntervalDTO(v reporting.HashrateInterval) *pb.ReportingHashrateInterval {
	out := &pb.ReportingHashrateInterval{}
	out.Id = v.ID
	out.AssetId = v.AssetID
	out.Kind = v.Kind
	out.SiteId = v.SiteID
	out.ContainerId = v.ContainerID
	out.OrderId = v.OrderID
	out.SourceId = v.SourceID
	out.Start = reportingTimeDTO(v.Start)
	out.End = reportingTimeDTO(v.End)
	out.HashrateHs = v.HashrateHS
	out.Complete = v.Complete
	out.SourceRef = v.SourceRef
	return out
}
func decodeReportingHashrateInterval(m *pb.ReportingHashrateInterval, d *reportingDecoder) reporting.HashrateInterval {
	if m == nil {
		return reporting.HashrateInterval{}
	}
	out := reporting.HashrateInterval{}
	out.ID = m.Id
	out.AssetID = m.AssetId
	out.Kind = m.Kind
	out.SiteID = m.SiteId
	out.ContainerID = m.ContainerId
	out.OrderID = m.OrderId
	out.SourceID = m.SourceId
	out.Start = d.time(m.Start)
	out.End = d.time(m.End)
	out.HashrateHS = m.HashrateHs
	out.Complete = m.Complete
	out.SourceRef = m.SourceRef
	return out
}
func reportingPoolObservationDTO(v reporting.PoolObservation) *pb.ReportingPoolObservation {
	out := &pb.ReportingPoolObservation{}
	out.Id = v.ID
	out.AssetId = v.AssetID
	out.Kind = v.Kind
	out.SourceId = v.SourceID
	out.SourceAt = reportingTimeDTO(v.SourceAt)
	out.ReceivedAt = reportingTimeDTO(v.ReceivedAt)
	out.State = v.State
	out.Reason = v.Reason
	out.SourceRef = v.SourceRef
	return out
}
func decodeReportingPoolObservation(m *pb.ReportingPoolObservation, d *reportingDecoder) reporting.PoolObservation {
	if m == nil {
		return reporting.PoolObservation{}
	}
	out := reporting.PoolObservation{}
	out.ID = m.Id
	out.AssetID = m.AssetId
	out.Kind = m.Kind
	out.SourceID = m.SourceId
	out.SourceAt = d.time(m.SourceAt)
	out.ReceivedAt = d.time(m.ReceivedAt)
	out.State = m.State
	out.Reason = m.Reason
	out.SourceRef = m.SourceRef
	return out
}
func reportingObligationIntervalDTO(v reporting.ObligationInterval) *pb.ReportingObligationInterval {
	out := &pb.ReportingObligationInterval{}
	out.Id = v.ID
	out.EpisodeId = v.EpisodeID
	out.AssetId = v.AssetID
	out.Kind = v.Kind
	out.SiteId = v.SiteID
	out.ContainerId = v.ContainerID
	out.OrderId = v.OrderID
	out.Start = reportingTimeDTO(v.Start)
	out.End = reportingTimeDTO(v.End)
	out.AsOf = reportingTimeDTO(v.AsOf)
	out.Severity = v.Severity
	out.TicketRef = v.TicketRef
	out.TicketAt = reportingTimeDTO(v.TicketAt)
	out.SubmittedAt = reportingTimeDTO(v.SubmittedAt)
	out.ReceivedAt = reportingTimeDTO(v.ReceivedAt)
	out.DiagnosedAt = reportingTimeDTO(v.DiagnosedAt)
	out.RespondedAt = reportingTimeDTO(v.RespondedAt)
	out.ApprovedAt = reportingTimeDTO(v.ApprovedAt)
	out.Deadline = reportingTimeDTO(v.Deadline)
	out.RestoredAt = reportingTimeDTO(v.RestoredAt)
	out.ClosedAt = reportingTimeDTO(v.ClosedAt)
	out.Qualified = v.Qualified
	out.SourceRef = v.SourceRef
	return out
}
func decodeReportingObligationInterval(m *pb.ReportingObligationInterval, d *reportingDecoder) reporting.ObligationInterval {
	if m == nil {
		return reporting.ObligationInterval{}
	}
	out := reporting.ObligationInterval{}
	out.ID = m.Id
	out.EpisodeID = m.EpisodeId
	out.AssetID = m.AssetId
	out.Kind = m.Kind
	out.SiteID = m.SiteId
	out.ContainerID = m.ContainerId
	out.OrderID = m.OrderId
	out.Start = d.time(m.Start)
	out.End = d.time(m.End)
	out.AsOf = d.time(m.AsOf)
	out.Severity = m.Severity
	out.TicketRef = m.TicketRef
	out.TicketAt = d.time(m.TicketAt)
	out.SubmittedAt = d.time(m.SubmittedAt)
	out.ReceivedAt = d.time(m.ReceivedAt)
	out.DiagnosedAt = d.time(m.DiagnosedAt)
	out.RespondedAt = d.time(m.RespondedAt)
	out.ApprovedAt = d.time(m.ApprovedAt)
	out.Deadline = d.time(m.Deadline)
	out.RestoredAt = d.time(m.RestoredAt)
	out.ClosedAt = d.time(m.ClosedAt)
	out.Qualified = m.Qualified
	out.SourceRef = m.SourceRef
	return out
}
func reportingContractResultDTO(v reporting.ContractResult) *pb.ReportingContractResult {
	out := &pb.ReportingContractResult{}
	out.Id = v.ID
	out.Metric = v.Metric
	out.SiteId = v.SiteID
	out.ContainerId = v.ContainerID
	out.OrderId = v.OrderID
	out.Start = reportingTimeDTO(v.Start)
	out.End = reportingTimeDTO(v.End)
	out.Value = v.Value
	out.Unit = v.Unit
	out.SourceRef = v.SourceRef
	out.CalculationRef = v.CalculationRef
	return out
}
func decodeReportingContractResult(m *pb.ReportingContractResult, d *reportingDecoder) reporting.ContractResult {
	if m == nil {
		return reporting.ContractResult{}
	}
	out := reporting.ContractResult{}
	out.ID = m.Id
	out.Metric = m.Metric
	out.SiteID = m.SiteId
	out.ContainerID = m.ContainerId
	out.OrderID = m.OrderId
	out.Start = d.time(m.Start)
	out.End = d.time(m.End)
	out.Value = m.Value
	out.Unit = m.Unit
	out.SourceRef = m.SourceRef
	out.CalculationRef = m.CalculationRef
	return out
}
func reportingMetricDTO(v reporting.Metric) *pb.ReportingMetric {
	out := &pb.ReportingMetric{}
	out.Status = string(v.Status)
	out.Value = v.Value
	out.Exact = v.Exact
	out.Unit = v.Unit
	for _, x := range v.Amounts {
		out.Amounts = append(out.Amounts, reportingAmountDTO(x))
	}
	out.Causes = append([]string(nil), v.Causes...)
	out.Sources = append([]string(nil), v.Sources...)
	out.Method = v.Method
	return out
}
func reportingAmountDTO(v reporting.Amount) *pb.ReportingAmount {
	out := &pb.ReportingAmount{}
	out.Currency = v.Currency
	out.Value = v.Value
	out.Exact = v.Exact
	return out
}
func reportingBreakdownDTO(v reporting.Breakdown) *pb.ReportingBreakdown {
	out := &pb.ReportingBreakdown{}
	out.SiteId = v.SiteID
	out.ContainerId = v.ContainerID
	out.OrderId = v.OrderID
	out.Metrics = make(map[string]*pb.ReportingMetric, len(v.Metrics))
	for k, x := range v.Metrics {
		out.Metrics[k] = reportingMetricDTO(x)
	}
	return out
}
func reportingReconciliationDTO(v reporting.Reconciliation) *pb.ReportingReconciliation {
	out := &pb.ReportingReconciliation{}
	out.Metric = v.Metric
	out.View = v.View
	out.ExactTotal = v.ExactTotal
	out.ExactParts = v.ExactParts
	out.Matches = v.Matches
	out.Status = string(v.Status)
	return out
}
func reportingInterruptionDTO(v reporting.Interruption) *pb.ReportingInterruption {
	out := &pb.ReportingInterruption{}
	out.Id = v.ID
	out.SiteId = v.SiteID
	out.ContainerId = v.ContainerID
	out.OrderId = v.OrderID
	out.Start = reportingTimeDTO(v.Start)
	out.End = reportingTimeDTO(v.End)
	out.ObservedThrough = reportingTimeDTO(v.ObservedThrough)
	out.Open = v.Open
	out.Category = v.Category
	out.Summary = v.Summary
	out.RootCause = v.RootCause
	out.ClaimedExclusion = v.ClaimedExclusion
	out.ExclusionStatus = string(v.ExclusionStatus)
	out.SourceRef = v.SourceRef
	return out
}
func reportingObligationDetailDTO(v reporting.ObligationDetail) *pb.ReportingObligationDetail {
	out := &pb.ReportingObligationDetail{}
	out.Id = v.ID
	out.EpisodeId = v.EpisodeID
	out.AssetId = v.AssetID
	out.SiteId = v.SiteID
	out.ContainerId = v.ContainerID
	out.OrderId = v.OrderID
	out.EpisodeStart = reportingTimeDTO(v.EpisodeStart)
	out.IntervalStart = reportingTimeDTO(v.IntervalStart)
	out.IntervalEnd = reportingTimeDTO(v.IntervalEnd)
	out.ObservedThrough = reportingTimeDTO(v.ObservedThrough)
	out.Severity = v.Severity
	out.TicketRef = v.TicketRef
	out.TicketAt = reportingTimeDTO(v.TicketAt)
	out.SubmittedAt = reportingTimeDTO(v.SubmittedAt)
	out.ReceivedAt = reportingTimeDTO(v.ReceivedAt)
	out.DiagnosedAt = reportingTimeDTO(v.DiagnosedAt)
	out.RespondedAt = reportingTimeDTO(v.RespondedAt)
	out.ApprovedAt = reportingTimeDTO(v.ApprovedAt)
	out.Deadline = reportingTimeDTO(v.Deadline)
	out.RestoredAt = reportingTimeDTO(v.RestoredAt)
	out.ClosedAt = reportingTimeDTO(v.ClosedAt)
	out.Qualified = v.Qualified
	out.SourceRef = v.SourceRef
	return out
}
func reportingTargetDTO(v reporting.Target) *pb.ReportingTarget {
	out := &pb.ReportingTarget{}
	out.Date = v.Date
	out.Status = string(v.Status)
	out.Causes = append([]string(nil), v.Causes...)
	out.Sources = append([]string(nil), v.Sources...)
	out.Basis = v.Basis
	out.ReasonableEfforts = v.ReasonableEfforts
	return out
}
func reportingDeadlinesDTO(v reporting.Deadlines) *pb.ReportingDeadlines {
	out := &pb.ReportingDeadlines{}
	out.Contract = reportingTargetDTO(v.Contract)
	out.Internal = reportingTargetDTO(v.Internal)
	return out
}
func reportingResultDTO(v reporting.Result) *pb.ReportingResult {
	out := &pb.ReportingResult{}
	out.Version = v.Version
	out.Period = reportingPeriodDTO(v.Period)
	out.Complete = v.Complete
	out.Metrics = make(map[string]*pb.ReportingMetric, len(v.Metrics))
	for k, x := range v.Metrics {
		out.Metrics[k] = reportingMetricDTO(x)
	}
	for _, x := range v.Containers {
		out.Containers = append(out.Containers, reportingBreakdownDTO(x))
	}
	for _, x := range v.Orders {
		out.Orders = append(out.Orders, reportingBreakdownDTO(x))
	}
	for _, x := range v.Reconciliations {
		out.Reconciliations = append(out.Reconciliations, reportingReconciliationDTO(x))
	}
	for _, x := range v.Interruptions {
		out.Interruptions = append(out.Interruptions, reportingInterruptionDTO(x))
	}
	for _, x := range v.Obligations {
		out.Obligations = append(out.Obligations, reportingObligationDetailDTO(x))
	}
	out.Deadlines = reportingDeadlinesDTO(v.Deadlines)
	out.Sources = append([]string(nil), v.Sources...)
	return out
}

func reportingAuthorityDTO(v domain.ReportingAuthority) *pb.HostReportingAuthority {
	return &pb.HostReportingAuthority{UserId: v.UserID, Username: v.Username, CanCreate: v.CanCreate, AllSites: v.AllSites, SiteIds: append([]int64(nil), v.SiteIDs...), Revision: v.Revision}
}
func reportingInputsMetadataDTO(v domain.ReportingInputsMetadata) *pb.HostReportingInputsMetadata {
	out := &pb.HostReportingInputsMetadata{Revision: v.Revision, Configuration: reportingConfigurationDTO(v.Configuration), SourceRef: v.SourceRef, UpdatedBy: v.UpdatedBy, UpdatedAt: reportingTimeDTO(v.UpdatedAt), SiteIds: append([]int64(nil), v.SiteIDs...)}
	for _, kind := range reportingFactKinds {
		out.RegisterCounts = append(out.RegisterCounts, &pb.ReportingRegisterCount{Kind: reportingFactKindDTO(kind), Count: v.RegisterCounts[kind]})
	}
	return out
}
func reportingSetupDTO(v domain.ReportingSetup) *pb.HostReportingSetup {
	out := &pb.HostReportingSetup{AccountId: v.AccountID, Name: v.Name, CanAdminister: v.CanAdminister, CanCreate: v.CanCreate, CanReadRequests: v.CanReadRequests, CanCreateRequests: v.CanCreateRequests, CanManageRepresentatives: v.CanManageRepresentatives}
	for _, owner := range v.Owners {
		out.Owners = append(out.Owners, &pb.HostReportingOwner{UserId: owner.UserID, Username: owner.Username})
	}
	for _, a := range v.Authorities {
		out.Authorities = append(out.Authorities, reportingAuthorityDTO(a))
	}
	if v.Input != nil {
		out.Input = reportingInputsMetadataDTO(*v.Input)
	}
	for _, c := range v.Capture {
		out.Capture = append(out.Capture, &pb.HostReportingCaptureStream{SourceId: int32(c.SourceID), Metric: int32(c.Metric), Expected: reportingInt32(c.Expected), Missing: reportingInt32(c.Missing), OldestCapture: reportingOptionalTimeDTO(c.OldestCapture), OldestSource: reportingOptionalTimeDTO(c.OldestSource)})
	}
	return out
}

func accountReportDTO(v domain.ReportRecord) *pb.AccountReport {
	if v.Status != "issued" {
		return nil
	}
	out := reportContentDTO(v)
	for _, obligation := range out.Result.Obligations {
		obligation.TicketRef = ""
	}
	return out
}
func reportContentDTO(v domain.ReportRecord) *pb.AccountReport {
	return &pb.AccountReport{ReportId: v.ReportID, SeriesId: v.SeriesID, AccountId: v.AccountID, AccountName: v.AccountName, Number: v.Number, Revision: v.Revision, Period: reportingPeriodDTO(v.Period), Result: reportingResultDTO(v.Result), Status: v.Status, IssuedAt: reportingOptionalTimeDTO(v.IssuedAt), CorrectionOf: v.CorrectionOf, CorrectionReason: v.CorrectionReason, Exception: v.Exception}
}
func reportSummaryDTO(v domain.ReportRecord) *pb.ReportSummary {
	out := &pb.ReportSummary{ReportId: v.ReportID, SeriesId: v.SeriesID, AccountId: v.AccountID, AccountName: v.AccountName, Number: v.Number, Revision: v.Revision, Period: reportingPeriodDTO(v.Period), Status: v.Status, Complete: v.Result.Complete, IssuedAt: reportingOptionalTimeDTO(v.IssuedAt), CorrectionOf: v.CorrectionOf, CorrectionReason: v.CorrectionReason, Exception: v.Exception, Deadlines: reportingDeadlinesDTO(v.Result.Deadlines)}
	if v.DeliveryStatus != "" {
		out.DeliveryStatus = v.DeliveryStatus
	} else if len(v.Deliveries) > 0 {
		out.DeliveryStatus = v.Deliveries[len(v.Deliveries)-1].Status
	}
	return out
}
func accountReportSummaryDTO(v domain.ReportRecord) *pb.ReportSummary {
	if v.Status != "issued" {
		return nil
	}
	return reportSummaryDTO(v)
}
func hostReportSummaryDTO(v domain.ReportRecord, setup domain.ReportingSetup) *pb.HostReportSummary {
	return &pb.HostReportSummary{Report: reportSummaryDTO(v), CreatedBy: v.CreatedBy, OwnerId: v.OwnerID, InputRevision: v.InputRevision, CanPublish: setup.CanCreate && v.Status == "draft"}
}
func hostReportDTO(v domain.ReportRecord, setup domain.ReportingSetup) *pb.HostReport {
	summary := hostReportSummaryDTO(v, setup)
	out := &pb.HostReport{Report: reportContentDTO(v), CreatedBy: v.CreatedBy, OwnerId: v.OwnerID, InputRevision: v.InputRevision, CanPublish: summary.CanPublish, IssuedBy: v.IssuedBy}
	for _, d := range v.Deliveries {
		out.Deliveries = append(out.Deliveries, &pb.HostReportDelivery{RequestId: d.RequestKey, Channel: d.Channel, Status: d.Status, SentAt: reportingOptionalTimeDTO(d.SentAt), ReceivedAt: reportingOptionalTimeDTO(d.ReceivedAt), SourceRef: d.SourceRef, Evidence: d.Evidence, FailureCategory: d.FailureCategory, ActorId: d.ActorID, CreatedAt: reportingTimeDTO(d.CreatedAt)})
	}
	return out
}

func hostReportRequestDTO(v domain.ReportRequest) *pb.HostReportRequest {
	out := &pb.HostReportRequest{ReceivedAt: reportingTimeDTO(v.ReceivedAt), SourceRef: v.SourceRef, Requester: v.Requester, Scope: v.Scope, OwnerUserId: v.OwnerUserID, TargetAt: reportingTimeDTO(v.TargetAt), Limitations: v.Limitations, CommunicationRef: v.CommunicationRef, DeliveryRef: v.DeliveryRef}
	out.RequestId = v.RequestID
	out.FulfilledAt = reportingOptionalTimeDTO(v.FulfilledAt)
	out.FulfillmentRef = v.FulfillmentRef
	return out
}

func reportingInt32(value int) int32 {
	if value > math.MaxInt32 {
		return math.MaxInt32
	}
	if value < math.MinInt32 {
		return math.MinInt32
	}
	return int32(value)
}
