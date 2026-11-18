package hostedaccounts

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
)

func loadAnalysisEnergyContext(ctx context.Context, q sqlc.Querier, org int64, e miningEstate, site int64, p reporting.Period) (reporting.Inputs, error) {
	ctx, cancel := context.WithTimeout(ctx, selectedReportContextTimeout)
	defer cancel()
	var out reporting.Inputs
	row, err := q.LockReportingInputsForShare(ctx, sqlc.LockReportingInputsForShareParams{OrganizationID: org, AccountID: e.account.ID})
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(row.Configuration, &out.Configuration); err != nil {
		return out, fmt.Errorf("decode energy reporting configuration: %w", err)
	}
	authority, err := e.physicalReadAuthority()
	if err != nil {
		return out, err
	}
	size, err := q.AccountAnalysisEnergyContextSize(ctx, sqlc.AccountAnalysisEnergyContextSizeParams{OrganizationID: org, AccountID: e.account.ID, SiteFilter: site, PolicyRestricted: e.policy.EffectiveRestricted, PolicySites: e.policy.EffectiveSiteIDs, HostAuthority: authority, PeriodStart: p.Start, PeriodEnd: p.End, StartSeconds: p.Start.Unix(), StartNanos: int64(p.Start.Nanosecond()), EndSeconds: p.End.Unix(), EndNanos: int64(p.End.Nanosecond())})
	if err != nil {
		return out, err
	}
	if size.PayloadBytes > selectedReportInputBytes || size.FactCount > (selectedReportInputHeapBytes-8*reporting.MaxReportingConfigurationBytes-8*size.PayloadBytes-6*size.IdentityBytes)/2048 {
		return out, fleeterror.NewFailedPreconditionError("selected energy input exceeds byte or heap budget")
	}
	args := sqlc.ListAccountAnalysisEnergyContextMetadataParams{OrganizationID: org, AccountID: e.account.ID, SiteFilter: site, PolicyRestricted: e.policy.EffectiveRestricted, PolicySites: e.policy.EffectiveSiteIDs, HostAuthority: authority, PeriodStart: p.Start, PeriodEnd: p.End, StartSeconds: p.Start.Unix(), StartNanos: int64(p.Start.Nanosecond()), EndSeconds: p.End.Unix(), EndNanos: int64(p.End.Nanosecond()), PageLimit: max(1, size.FactCount)}
	meta := make([]reportingFactMetadata, 0, int(size.FactCount))
	if err := ctx.Err(); err != nil {
		return out, fmt.Errorf("select energy context: %w", err)
	}
	rows, err := q.ListAccountAnalysisEnergyContextMetadata(ctx, args)
	if err != nil {
		return out, err
	}
	for _, r := range rows {
		meta = append(meta, reportingFactMetadata{r.RowID.Int64, reporting.FactKind(r.Kind), r.FactID, r.PayloadBytes.Int64})
	}
	if int64(len(meta)) != size.FactCount {
		return out, fmt.Errorf("energy context changed under revision lock")
	}
	err = streamReportingContextFacts(ctx, q, org, e.account.ID, meta, func(f reporting.Fact) error {
		_, value, err := reporting.DecodeFact(f.Kind, f.Payload)
		if err != nil {
			return err
		}
		switch value := value.(type) {
		case reporting.Order:
			out.Orders = append(out.Orders, value)
		case reporting.CoverageSegment:
			out.Coverage = append(out.Coverage, value)
		case reporting.MeterInterval:
			out.Meters = append(out.Meters, value)
		default:
			return fmt.Errorf("unexpected energy input kind %q", f.Kind)
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	// Recalls and placement changes can close an allocation while its coverage
	// stays open; report preparation rejects that state, and so must analysis.
	if err := validateCoverageAllocations(ctx, q, org, e.account.ID, out.Coverage); err != nil {
		if fleeterror.IsForbiddenError(err) || fleeterror.IsInvalidArgumentError(err) {
			return out, fleeterror.NewFailedPreconditionError("commercial coverage exceeds its evidenced allocation interval; coverage must be repaired before energy can be analyzed")
		}
		return out, err
	}
	return out, nil
}
