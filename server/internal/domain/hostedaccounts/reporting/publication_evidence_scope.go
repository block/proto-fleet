package reporting

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// SelectPublicEvidence selects reviewed facts relevant to the issued period or
// its required historical context. It does not replace authoritative validation,
// report authorization or the revision lock; publication must establish those
// before calling it. Original values and intervals are retained without clipping.
//
// observations must enumerate the report's frozen monitoring evidence in
// strictly ascending (interval, time) order. The writer's granularity applies:
// UTC days when DailyObservations, otherwise hours. It is consumed once here to
// retain the commercial coverage that explains each row; publication must replay
// the same frozen stream to the ZIP writer under the same transaction and
// authorization. It is deliberately not a fresh telemetry query. SQL's
// microsecond timestamp rounding is conservatively widened when matching rows.
func SelectPublicEvidence(ctx context.Context, p Period, in Inputs, observations ObservationSource) (Inputs, error) {
	if err := p.Validate(); err != nil {
		return Inputs{}, err
	}
	start := p.Start
	if p.Kind == "annual" {
		comparison := p
		comparison.Kind = "ad_hoc"
		comparison.Start = p.Start.In(mustLocation(p.Timezone)).AddDate(-1, 0, 0)
		comparison.End = p.Start
		start = comparison.Start
	}
	relevant := func(a, b time.Time) bool { return overlaps(start, p.End, a, b) }
	out := Inputs{Configuration: in.Configuration}
	type episodeKey struct {
		kind  int32
		asset int64
		id    string
	}
	key := func(v ObligationInterval) episodeKey { return episodeKey{v.Kind, v.AssetID, v.EpisodeID} }
	episodes := map[episodeKey]bool{}
	for _, v := range in.Obligations {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		if v.ReceivedAt.After(p.AsOf) || !v.Start.Before(p.End) {
			continue
		}
		if overlaps(p.Start, p.End, v.Start, v.End) {
			episodes[key(v)] = true
		}
	}
	// Preserve both an open ticket and the globally later state that suppresses
	// it, even when both predate the period or refer to different scopes. This
	// leaves unrelated closed histories out while making suppression auditable.
	couldBeOpen := func(v ObligationInterval, end time.Time) bool {
		probe := end.Add(-time.Nanosecond)
		if !v.Start.Before(end) || v.Severity != "S3" || !v.Qualified || v.TicketRef == "" || (!v.ClosedAt.IsZero() && !v.ClosedAt.After(probe)) {
			return false
		}
		ticket := v.TicketAt
		if ticket.IsZero() || (!v.SubmittedAt.IsZero() && v.SubmittedAt.Before(ticket)) {
			ticket = v.SubmittedAt
		}
		return !ticket.IsZero() && !ticket.After(probe)
	}
	for _, v := range in.Obligations {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		if !v.ReceivedAt.After(p.AsOf) && couldBeOpen(v, p.End) {
			episodes[key(v)] = true
		}
	}
	for _, v := range in.Obligations {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		if episodes[key(v)] && v.Start.Before(p.End) && !v.ReceivedAt.After(p.AsOf) {
			out.Obligations = append(out.Obligations, v)
		}
	}
	for _, v := range in.Meters {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		if relevant(v.Start, v.End) {
			out.Meters = append(out.Meters, v)
		}
	}
	for _, v := range in.Fees {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		// Year-over-year comparisons do not use fees from the prior year.
		if overlaps(p.Start, p.End, v.Start, v.End) {
			out.Fees = append(out.Fees, v)
		}
	}
	for _, v := range in.Incidents {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		if relevant(v.Start, v.End) {
			out.Incidents = append(out.Incidents, v)
		}
	}
	for _, v := range in.Hashrates {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		if relevant(v.Start, v.End) {
			out.Hashrates = append(out.Hashrates, v)
		}
	}
	for _, v := range in.ExternalResults {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		if overlaps(p.Start, p.End, v.Start, v.End) {
			out.ExternalResults = append(out.ExternalResults, v)
		}
	}

	// Index the selected full intervals by exact attribution. Merge overlapping
	// windows once so selecting supporting coverage is not a cross-product.
	type attribution struct {
		kind                   int32
		asset, site, container int64
		order                  string
	}
	type span struct{ start, end time.Time }
	needed := map[attribution][]span{}
	addNeeded := func(k attribution, a, b time.Time) {
		if b.After(a) {
			needed[k] = append(needed[k], span{a, b})
		}
	}
	for _, h := range out.Hashrates {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		addNeeded(attribution{h.Kind, h.AssetID, h.SiteID, h.ContainerID, h.OrderID}, h.Start, h.End)
	}
	for _, o := range out.Obligations {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		through := boundedEnd(o.End, o.AsOf)
		addNeeded(attribution{o.Kind, o.AssetID, o.SiteID, o.ContainerID, o.OrderID}, o.Start, through)
	}
	for k, windows := range needed {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		sort.Slice(windows, func(i, j int) bool { return windows[i].start.Before(windows[j].start) })
		merged := windows[:0]
		for _, w := range windows {
			if err := ctx.Err(); err != nil {
				return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
			}
			if len(merged) == 0 || w.start.After(merged[len(merged)-1].end) {
				merged = append(merged, w)
				continue
			}
			if w.end.After(merged[len(merged)-1].end) {
				merged[len(merged)-1].end = w.end
			}
		}
		needed[k] = merged
	}
	intersectsNeeded := func(k attribution, a, b time.Time) bool {
		windows := needed[k]
		i := sort.Search(len(windows), func(i int) bool { return windows[i].end.After(a) })
		return i < len(windows) && (b.IsZero() || windows[i].start.Before(b))
	}

	// Current module mappings contribute to repair metrics. Mappings needed
	// for selected obligation intervals retain their exact board references.
	boards := map[string]bool{}
	for _, m := range in.ModuleCoverage {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		keep := overlaps(p.Start, p.End, m.Start, m.End) || intersectsNeeded(attribution{6, m.ModuleID, m.SiteID, m.ContainerID, m.OrderID}, m.Start, m.End)
		if keep {
			out.ModuleCoverage = append(out.ModuleCoverage, m)
			for _, id := range m.BoardCoverageIDs {
				boards[id] = true
			}
		}
	}
	// Each frozen row exists because its hour overlaps a commercial segment of
	// its interval within the period. Preserve the segments that explain it.
	capturedCoverage := map[string]bool{}
	// Validated coverage segments on one allocation are non-overlapping. A
	// successor index visits each marked segment at most once across all rows.
	type capturedSegments struct {
		segments []CoverageSegment
		next     []int
	}
	byInterval := map[int64]*capturedSegments{}
	for _, c := range in.Coverage {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		group := byInterval[c.AllocationIntervalID]
		if group == nil {
			group = &capturedSegments{}
			byInterval[c.AllocationIntervalID] = group
		}
		group.segments = append(group.segments, c)
	}
	for _, group := range byInterval {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		sort.Slice(group.segments, func(i, j int) bool { return group.segments[i].Start.Before(group.segments[j].Start) })
		group.next = make([]int, len(group.segments)+1)
		for i := range group.next {
			group.next[i] = i
		}
	}
	find := func(next []int, n int) int {
		root := n
		for next[root] != root {
			root = next[root]
		}
		for next[n] != n {
			old := n
			n = next[n]
			next[old] = root
		}
		return root
	}
	lastInterval, lastAt := int64(0), time.Time{}
	var yieldErr error
	visit := func(interval, asset, site int64, kind int32, at time.Time, span time.Duration) error {
		if yieldErr != nil {
			return yieldErr
		}
		if err := ctx.Err(); err != nil {
			yieldErr = fmt.Errorf("select public report evidence: %w", err)
			return yieldErr
		}
		if interval < lastInterval || (interval == lastInterval && !at.After(lastAt)) {
			yieldErr = fmt.Errorf("monitoring evidence must ascend by interval and time without repeats")
			return yieldErr
		}
		lastInterval, lastAt = interval, at
		group := byInterval[interval]
		if group == nil || len(group.segments) == 0 || group.segments[0].Kind != kind || group.segments[0].AssetID != asset || group.segments[0].SiteID != site {
			yieldErr = fmt.Errorf("monitoring evidence has no supporting commercial coverage")
			return yieldErr
		}
		oldest, newest := maxTime(at, p.Start).Add(-time.Microsecond), minTime(at.Add(span), p.End).Add(time.Microsecond)
		first := sort.Search(len(group.segments), func(i int) bool { c := group.segments[i]; return c.End.IsZero() || c.End.After(oldest) })
		last := sort.Search(len(group.segments), func(i int) bool { return !group.segments[i].Start.Before(newest) })
		if first >= last {
			yieldErr = fmt.Errorf("monitoring evidence has no supporting commercial coverage")
			return yieldErr
		}
		for i := find(group.next, first); i < last; i = find(group.next, i) {
			if err := ctx.Err(); err != nil {
				yieldErr = fmt.Errorf("select public report evidence: %w", err)
				return yieldErr
			}
			capturedCoverage[group.segments[i].ID] = true
			group.next[i] = find(group.next, i+1)
		}
		return nil
	}
	var sourceErr error
	if DailyObservations(p, in.Coverage) {
		if observations.Days != nil {
			sourceErr = observations.Days(func(v ObservationDay) error {
				return visit(v.IntervalID, v.AssetID, v.SiteID, v.Kind, v.Day, 24*time.Hour)
			})
		}
	} else if observations.Hours != nil {
		sourceErr = observations.Hours(func(v Observation) error {
			return visit(v.IntervalID, v.AssetID, v.SiteID, v.Kind, v.Bucket, time.Hour)
		})
	}
	if yieldErr != nil {
		return Inputs{}, yieldErr
	}
	if sourceErr != nil {
		return Inputs{}, sourceErr
	}
	for _, c := range in.Coverage {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		keep := relevant(c.Start, c.End) || boards[c.ID] || capturedCoverage[c.ID] || intersectsNeeded(attribution{c.Kind, c.AssetID, c.SiteID, c.ContainerID, c.OrderID}, c.Start, c.End)
		if keep {
			out.Coverage = append(out.Coverage, c)
		}
	}
	// Current pool counts use the latest timely observation at period end.
	// Include all ties so conflicting states stay visible. Prior pool counts do
	// not contribute to the annual energy, hashrate or maintenance comparisons.
	type asset struct {
		kind int32
		id   int64
	}
	type poolMoment struct {
		asset asset
		at    time.Time
	}
	wanted := map[poolMoment]bool{}
	for _, end := range []time.Time{p.End} {
		probe := end.Add(-time.Nanosecond)
		active := map[asset]CoverageSegment{}
		for _, c := range in.Coverage {
			if err := ctx.Err(); err != nil {
				return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
			}
			if !c.Start.After(probe) && (c.End.IsZero() || c.End.After(probe)) {
				active[asset{c.Kind, c.AssetID}] = c
			}
		}
		latestPool := map[asset]time.Time{}
		for _, v := range in.PoolObservations {
			if err := ctx.Err(); err != nil {
				return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
			}
			a := asset{v.Kind, v.AssetID}
			c, ok := active[a]
			if !ok || v.SourceAt.Before(c.Start) || v.SourceAt.After(probe) || v.ReceivedAt.After(p.AsOf) {
				continue
			}
			if old, exists := latestPool[a]; !exists || v.SourceAt.After(old) {
				latestPool[a] = v.SourceAt
			}
		}
		for a, at := range latestPool {
			wanted[poolMoment{a, at.UTC()}] = true
		}
	}
	for _, v := range in.PoolObservations {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		if !v.ReceivedAt.After(p.AsOf) && wanted[poolMoment{asset{v.Kind, v.AssetID}, v.SourceAt.UTC()}] {
			out.PoolObservations = append(out.PoolObservations, v)
		}
	}
	orders := map[string]bool{}
	for _, v := range out.Coverage {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		orders[v.OrderID] = true
	}
	for _, v := range out.ModuleCoverage {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		orders[v.OrderID] = true
	}
	for _, v := range out.Meters {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		orders[v.OrderID] = true
	}
	for _, v := range out.Fees {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		orders[v.OrderID] = true
	}
	for _, v := range out.Incidents {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		orders[v.OrderID] = true
	}
	for _, v := range out.Hashrates {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		orders[v.OrderID] = true
	}
	for _, v := range out.Obligations {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		orders[v.OrderID] = true
	}
	for _, v := range out.ExternalResults {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		orders[v.OrderID] = true
	}
	for _, v := range in.Orders {
		if err := ctx.Err(); err != nil {
			return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
		}
		if orders[v.ID] || overlaps(p.Start, p.End, v.Start, v.End) {
			out.Orders = append(out.Orders, v)
		}
	}
	if err := ctx.Err(); err != nil {
		return Inputs{}, fmt.Errorf("select public report evidence: %w", err)
	}
	return out, nil
}

// VerifyPublicEvidenceCalculation fails closed when the narrowed reviewed facts
// cannot reproduce the complete reviewed report-context calculation. This checks
// dependencies; it does not validate frozen event membership or public privacy.
func VerifyPublicEvidenceCalculation(ctx context.Context, p Period, in Inputs, expected Result) error {
	actual, err := CalculateStoredContext(ctx, p, in)
	if err != nil {
		return fmt.Errorf("recalculate selected public evidence: %w", err)
	}
	return compareReviewedResults(actual, expected)
}
