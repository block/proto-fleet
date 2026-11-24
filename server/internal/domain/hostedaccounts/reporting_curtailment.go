package hostedaccounts

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
)

// sqlc buffers the complete query result. A conservative estimate for each fixed-
// width window includes slice growth and scan overhead; one extra row detects
// exhaustion without silently truncating the frozen cohort. This is a capture
// memory budget, independent of the stored input-register size.
const curtailmentWindowMemoryBytes = 16 << 20
const curtailmentWindowEstimatedBytes = 512
const maxCurtailmentWindows = curtailmentWindowMemoryBytes / curtailmentWindowEstimatedBytes

// appendCurtailmentEvidence adds operational control observations, never reviewed
// incidents or contractual calculations. Result serialization freezes this view.
func appendCurtailmentEvidence(ctx context.Context, q sqlc.Querier, a reportingAccess, result *reporting.Result) ([]int64, error) {
	ctx, cancel := context.WithTimeout(ctx, selectedReportContextTimeout)
	defer cancel()
	initial, err := reporting.MarshalResultContext(ctx, *result)
	if err != nil {
		return nil, err
	}
	resultBytes := len(initial)
	p := result.Period
	args := sqlc.ListHostedCurtailmentWindowsParams{
		OrganizationID: a.account.OrganizationID, AccountID: a.account.ID, PeriodStart: p.Start,
		PeriodEnd: p.End, AsOf: p.AsOf, RowLimit: maxCurtailmentWindows + 1,
	}
	// One statement gives every control window the same MVCC snapshot even when
	// an already-captured restoration transaction commits during this read.
	rows, err := q.ListHostedCurtailmentWindows(ctx, args)
	if err != nil {
		return nil, err
	}
	if len(rows) > maxCurtailmentWindows {
		return nil, fleeterror.NewFailedPreconditionError("curtailment capture exceeds report memory budget; use a shorter reporting period")
	}
	var sites []int64
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prepare curtailment evidence: %w", err)
		}
		// Retained unknown sites and revoked authority fail closed, just like other
		// report facts. Do not turn an incomplete operational view into a zero.
		if !a.sitesAllowed([]int64{row.SiteID}) {
			return nil, denied()
		}
		through := p.End
		if p.AsOf.Before(through) {
			through = p.AsOf
		}
		if !row.AttributionEnd.IsZero() && row.AttributionEnd.Before(through) {
			through = row.AttributionEnd
		}
		if !through.After(row.StartedAt) {
			continue
		}
		var end time.Time
		if !row.RestoredAt.IsZero() && !row.RestoredAt.After(through) {
			end = row.RestoredAt
			through = end
		}
		asset := "Miner"
		if row.Kind == 7 {
			asset = "Hashboard"
		}
		source := "curtailment-confirmation:" + row.EvidenceID.String()
		basis := "Confirmed curtailment control observation"
		if !end.IsZero() {
			basis += " and matching restoration confirmation"
		}
		basis += ". These endpoints do not prove uninterrupted physical curtailment. Evidence is limited to the reporting period, as-of time, original allocation/installation and any later curtailment confirmation. No contractual exclusion, fee or credit is inferred; earlier activity is not backfilled."
		id := source + ":" + strconv.FormatInt(row.IntervalID, 10)
		observation := reporting.Interruption{
			ID: id, SiteID: row.SiteID, Start: row.StartedAt, End: end, ObservedThrough: through, Open: end.IsZero(),
			Category: "curtailment_control", Summary: fmt.Sprintf("%s %d curtailment", asset, row.AssetID),
			RootCause: basis, ExclusionStatus: reporting.Unavailable, SourceRef: source,
		}
		encoded, err := json.Marshal(observation)
		if err != nil {
			return nil, fmt.Errorf("encode curtailment observation: %w", err)
		}
		resultBytes += len(encoded) + len(source) + 4
		if resultBytes > reporting.MaxResultJSONBytes {
			return nil, fleeterror.NewFailedPreconditionError("curtailment evidence exceeds report byte budget; use a shorter reporting period")
		}
		result.Interruptions = append(result.Interruptions, observation)
		result.Sources = append(result.Sources, source)
		sites = append(sites, row.SiteID)
	}

	slices.Sort(sites)
	sites = slices.Compact(sites)
	slices.Sort(result.Sources)
	result.Sources = slices.Compact(result.Sources)
	return sites, nil
}
