package reporting

import (
	"context"
	"fmt"
	"math/big"
	"slices"
	"time"
)

// ValidateConfigurationContext validates the bounded account configuration without
// reading any registers. Missing certification continues to mean unavailable metrics.
func ValidateConfigurationContext(ctx context.Context, c Configuration) error {
	return (Inputs{Configuration: c}).ValidateContext(ctx)
}

// ValidateFactFields checks fields intrinsic to one typed fact. Relationships to
// other saved facts and retained physical evidence are validated transactionally.
func ValidateFactFields(value any) error {
	attributed := func(site, container int64, order string, start, end time.Time) bool {
		return site > 0 && container > 0 && text(order) && interval(start, end, true)
	}
	switch v := value.(type) {
	case Order:
		if !text(v.Name) || !text(v.SourceRef) || !interval(v.Start, v.End, true) {
			return fmt.Errorf("invalid hosting order")
		}
	case CoverageSegment:
		if v.AllocationIntervalID <= 0 || v.AssetID <= 0 || !reportingAssetKind(v.Kind) || !attributed(v.SiteID, v.ContainerID, v.OrderID, v.Start, v.End) || !text(v.SourceRef) {
			return fmt.Errorf("invalid commercial coverage")
		}
		r, e := Decimal(v.CapacityTHS, false)
		if e != nil || r.Sign() == 0 {
			return fmt.Errorf("coverage requires positive capacity")
		}
		if !v.CommissionedAt.IsZero() && v.Start.Before(v.CommissionedAt) {
			return fmt.Errorf("coverage precedes commissioning")
		}
	case ModuleCoverageSegment:
		if v.ModuleID <= 0 || v.End.IsZero() || !attributed(v.SiteID, v.ContainerID, v.OrderID, v.Start, v.End) || !text(v.SourceRef) || len(v.BoardCoverageIDs) < 1 || len(v.BoardCoverageIDs) > 3 || (v.WholeModule && len(v.BoardCoverageIDs) != 3) {
			return fmt.Errorf("invalid reviewed module coverage")
		}
		for i, id := range v.BoardCoverageIDs {
			if !text(id) || slices.Contains(v.BoardCoverageIDs[:i], id) {
				return fmt.Errorf("module coverage requires distinct board references")
			}
		}
	case MeterInterval:
		if !attributed(v.SiteID, v.ContainerID, v.OrderID, v.Start, v.End) || v.End.IsZero() || !text(v.MeterID) || !text(v.SourceID) || !text(v.SourceRef) || !slices.Contains([]string{"measured", "estimated", "missing", "reset"}, v.Quality) {
			return fmt.Errorf("invalid meter interval")
		}
		if v.Quality == "measured" || v.Quality == "estimated" {
			if _, e := Decimal(v.KWh, false); e != nil {
				return fmt.Errorf("invalid metered energy")
			}
		} else if v.KWh != "" {
			return fmt.Errorf("missing or reset meter interval must not supply consumption")
		}
		if v.AllocationFraction != "" {
			r, e := Decimal(v.AllocationFraction, false)
			if e != nil || r.Sign() <= 0 || r.Cmp(big.NewRat(1, 1)) > 0 {
				return fmt.Errorf("shared meter requires approved allocation evidence")
			}
		}
	case FeeLine:
		if !attributed(v.SiteID, v.ContainerID, v.OrderID, v.Start, v.End) || v.End.IsZero() || !text(v.SourceRef) || !currencyPattern.MatchString(v.Currency) {
			return fmt.Errorf("invalid accrued fee attribution")
		}
		if _, e := Decimal(v.Amount, true); e != nil {
			return fmt.Errorf("invalid accrued fee decimal")
		}
	case Incident:
		if !attributed(v.SiteID, v.ContainerID, v.OrderID, v.Start, v.End) || v.AsOf.Before(v.Start) || !text(v.Summary) || !text(v.SourceRef) || len(v.RootCause) > 4000 {
			return fmt.Errorf("invalid incident")
		}
	case HashrateInterval:
		if !attributed(v.SiteID, v.ContainerID, v.OrderID, v.Start, v.End) || v.End.IsZero() || v.AssetID <= 0 || !reportingAssetKind(v.Kind) || !text(v.SourceID) || !text(v.SourceRef) {
			return fmt.Errorf("invalid hashrate interval")
		}
		if _, e := Decimal(v.HashrateHS, false); e != nil {
			return e
		}
	case PoolObservation:
		if v.AssetID <= 0 || !reportingAssetKind(v.Kind) || v.SourceAt.IsZero() || v.ReceivedAt.Before(v.SourceAt) || !text(v.SourceID) || !text(v.SourceRef) || !slices.Contains([]string{"reporting", "failed", "unavailable", "unknown"}, v.State) {
			return fmt.Errorf("invalid pool observation")
		}
	case ObligationInterval:
		if v.Kind != AssetKindModule {
			return fmt.Errorf("reviewed repair obligations require an explicit module unit")
		}
		if !attributed(v.SiteID, v.ContainerID, v.OrderID, v.Start, v.End) || v.AssetID <= 0 || !text(v.EpisodeID) || !text(v.SourceRef) || v.AsOf.Before(v.Start) || v.ReceivedAt.IsZero() || !slices.Contains([]string{"", "S1", "S2", "S3"}, v.Severity) {
			return fmt.Errorf("invalid reviewed obligation")
		}
		if !v.RestoredAt.IsZero() && v.RestoredAt.Before(v.Start) {
			return fmt.Errorf("restoration precedes episode")
		}
	case ContractResult:
		if !interval(v.Start, v.End, false) || !text(v.Metric) || !text(v.Unit) || !text(v.SourceRef) || !text(v.CalculationRef) || v.SiteID <= 0 {
			return fmt.Errorf("invalid external result evidence")
		}
		if _, e := Decimal(v.Value, true); e != nil {
			return e
		}
	default:
		return fmt.Errorf("unsupported reporting fact type %T", value)
	}
	return nil
}

// SharedAllocationApproved reports whether an allocated meter can cite the
// configured reviewed evidence; whitespace is not evidence.
func (c Configuration) SharedAllocationApproved() bool { return text(c.SharedAllocationRef) }

// SupportsModule compares attribution and exact original windows. Distinct board
// identity and retained physical membership are checked by the caller.
func (c CoverageSegment) SupportsModule(m ModuleCoverageSegment) bool {
	return c.Kind == AssetKindHashboard && c.SiteID == m.SiteID && c.ContainerID == m.ContainerID && c.OrderID == m.OrderID && !m.Start.Before(c.Start) && (c.End.IsZero() || !m.End.After(c.End))
}

// SharesEvidenceWith permits allocation shares of the same physical reading.
// Callers separately enforce distinct orders and an exact total no greater than one.
func (m MeterInterval) SharesEvidenceWith(p MeterInterval) bool {
	return m.Start.Equal(p.Start) && m.End.Equal(p.End) && m.KWh == p.KWh && m.SourceID == p.SourceID && m.Quality == p.Quality && m.ContainerID == p.ContainerID && m.SiteID == p.SiteID && m.AllocationFraction != "" && p.AllocationFraction != ""
}
