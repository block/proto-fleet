package reporting

import (
	"context"
	"fmt"
	"math/big"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

const backlogBeyondDeadline = "beyond_deadline"

type window struct{ start, end time.Time }
type scope struct {
	site, container int64
	order           string
}

func (s scope) matches(site, container int64, order string) bool {
	return (s.site == 0 || s.site == site) && (s.container == 0 || s.container == container) && (s.order == "" || s.order == order)
}
func assetKey(kind int32, id int64) string {
	return strconv.Itoa(int(kind)) + ":" + strconv.FormatInt(id, 10)
}
func uniqueStrings(ss []string) []string {
	ss = slices.Clone(ss)
	slices.Sort(ss)
	return slices.Compact(ss)
}
func rational(text string) *big.Rat { r, _ := new(big.Rat).SetString(text); return r }
func ratString(r *big.Rat) string {
	digits, exact := r.FloatPrec()
	if !exact {
		digits = 18
	}
	v := r.FloatString(digits)
	if strings.Contains(v, ".") {
		v = strings.TrimRight(strings.TrimRight(v, "0"), ".")
	}
	if v == "-0" {
		return "0"
	}
	return v
}
func unavailable(unit, cause string) Metric {
	return Metric{Status: Unavailable, Unit: unit, Causes: []string{cause}}
}
func number(v *big.Rat, unit, method string, causes, sources []string) Metric {
	s := Authoritative
	if len(causes) > 0 {
		s = Provisional
	}
	return Metric{Status: s, Value: ratString(v), Exact: v.RatString(), Unit: unit, Method: method, Causes: uniqueStrings(causes), Sources: uniqueStrings(sources)}
}
func count(n int, unit, method string, causes, sources []string) Metric {
	return number(big.NewRat(int64(n), 1), unit, method, causes, sources)
}
func gate(c Configuration, key string, causes, sources *[]string) {
	for _, g := range c.Prerequisites {
		if g.Key == key && c.Ready(key) {
			*sources = append(*sources, g.SourceRef)
			return
		}
	}
	*causes = append(*causes, "missing_"+key)
}
func certify(c Configuration, id, metric string, causes, sources *[]string) {
	if !c.Certified(id, metric) {
		*causes = append(*causes, "uncertified_"+metric+"_source")
		return
	}
	for _, v := range c.Sources {
		if v.ID == id && v.Metric == metric {
			*sources = append(*sources, v.SourceRef)
		}
	}
	gate(c, "G2", causes, sources)
}
func intersect(a, b window) (window, bool) {
	w := window{start: maxTime(a.start, b.start), end: minTime(a.end, b.end)}
	return w, w.end.After(w.start)
}
func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func boundedEnd(end, asOf time.Time) time.Time {
	if end.IsZero() || end.After(asOf) {
		return asOf
	}
	return end
}
func union(ws []window) []window {
	ws = slices.Clone(ws)
	sort.Slice(ws, func(i, j int) bool {
		if ws[i].start.Equal(ws[j].start) {
			return ws[i].end.Before(ws[j].end)
		}
		return ws[i].start.Before(ws[j].start)
	})
	out := []window{}
	for _, w := range ws {
		if !w.end.After(w.start) {
			continue
		}
		if len(out) == 0 || w.start.After(out[len(out)-1].end) {
			out = append(out, w)
		} else if w.end.After(out[len(out)-1].end) {
			out[len(out)-1].end = w.end
		}
	}
	return out
}
func duration(ws []window) *big.Rat {
	n := new(big.Int)
	for _, w := range union(ws) {
		n.Add(n, big.NewInt(w.end.Sub(w.start).Nanoseconds()))
	}
	return new(big.Rat).SetInt(n)
}
func covers(ws []window, w window) bool {
	for _, v := range union(ws) {
		if !v.start.After(w.start) && !v.end.Before(w.end) {
			return true
		}
	}
	return false
}
func periodWindow(p Period) window { return window{p.Start, p.End} }
func coverage(p Period, in Inputs, s scope) []CoverageSegment {
	out := []CoverageSegment{}
	for _, c := range in.Coverage {
		if s.matches(c.SiteID, c.ContainerID, c.OrderID) {
			if _, ok := intersect(window{c.Start, boundedEnd(c.End, p.End)}, periodWindow(p)); ok {
				out = append(out, c)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func attributedWindows(p Period, cs []CoverageSegment, site, container int64, order string) []window {
	out := []window{}
	for _, c := range cs {
		if c.SiteID == site && c.ContainerID == container && c.OrderID == order {
			if w, ok := intersect(window{c.Start, boundedEnd(c.End, p.End)}, periodWindow(p)); ok {
				out = append(out, w)
			}
		}
	}
	return union(out)
}
func scopeSources(cs []CoverageSegment) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.SourceRef)
	}
	return out
}

// Calculate uses only its explicit, validated as-of facts. It does not derive
// contractual uptime, remedies or restoration clocks from approval flags.
func Calculate(p Period, in Inputs) (Result, error) {
	return CalculateContext(context.Background(), p, in)
}

// CalculateContext bounds both breakdown allocation and work after cancellation.
func CalculateContext(ctx context.Context, p Period, in Inputs) (Result, error) {
	if err := in.ValidateContext(ctx); err != nil {
		return Result{}, err
	}
	return CalculateStoredContext(ctx, p, in)
}

// CalculateStoredContext calculates a selected context whose fact relationships
// were validated transactionally in authoritative storage. Supporting intervals
// may extend beyond the report and need not form a complete account register.
func CalculateStoredContext(ctx context.Context, p Period, in Inputs) (Result, error) {
	if err := p.Validate(); err != nil {
		return Result{}, err
	}
	if err := ValidateConfigurationContext(ctx, in.Configuration); err != nil {
		return Result{}, err
	}
	if p.Timezone != in.Configuration.Timezone {
		return Result{}, fmt.Errorf("period and configuration timezone differ")
	}
	loc, _ := time.LoadLocation(p.Timezone)
	if p.Kind == "annual" {
		start := p.Start.In(loc)
		if int(start.Month()) != in.Configuration.ReportingYearMonth || start.Day() != in.Configuration.ReportingYearDay {
			return Result{}, fmt.Errorf("annual period differs from configured reporting year")
		}
	}

	containers := map[scope]bool{}
	orders := map[string]bool{}
	add := func(site, container int64, order string) {
		if len(containers)+len(orders) > MaxReportBreakdowns {
			return
		}
		if site > 0 && container > 0 {
			containers[scope{site: site, container: container}] = true
		}
		if order != "" {
			orders[order] = true
		}
	}
	for _, c := range in.Coverage {
		if err := ctx.Err(); err != nil {
			return Result{}, fmt.Errorf("calculate report: %w", err)
		}
		if overlaps(p.Start, p.End, c.Start, c.End) {
			add(c.SiteID, c.ContainerID, c.OrderID)
		}
	}
	for _, v := range in.Meters {
		if err := ctx.Err(); err != nil {
			return Result{}, fmt.Errorf("calculate report: %w", err)
		}
		if overlaps(p.Start, p.End, v.Start, v.End) {
			add(v.SiteID, v.ContainerID, v.OrderID)
		}
	}
	for _, v := range in.Fees {
		if err := ctx.Err(); err != nil {
			return Result{}, fmt.Errorf("calculate report: %w", err)
		}
		if overlaps(p.Start, p.End, v.Start, v.End) {
			add(v.SiteID, v.ContainerID, v.OrderID)
		}
	}
	for _, v := range in.Hashrates {
		if err := ctx.Err(); err != nil {
			return Result{}, fmt.Errorf("calculate report: %w", err)
		}
		if overlaps(p.Start, p.End, v.Start, v.End) {
			add(v.SiteID, v.ContainerID, v.OrderID)
		}
	}
	for _, v := range in.Incidents {
		if err := ctx.Err(); err != nil {
			return Result{}, fmt.Errorf("calculate report: %w", err)
		}
		if overlaps(p.Start, p.End, v.Start, v.End) {
			add(v.SiteID, v.ContainerID, v.OrderID)
		}
	}
	for _, v := range in.Obligations {
		if err := ctx.Err(); err != nil {
			return Result{}, fmt.Errorf("calculate report: %w", err)
		}
		if !v.ReceivedAt.After(p.AsOf) && overlaps(p.Start, p.End, v.Start, v.End) {
			add(v.SiteID, v.ContainerID, v.OrderID)
		}
	}
	for _, v := range in.ExternalResults {
		if err := ctx.Err(); err != nil {
			return Result{}, fmt.Errorf("calculate report: %w", err)
		}
		if overlaps(p.Start, p.End, v.Start, v.End) {
			add(v.SiteID, v.ContainerID, v.OrderID)
		}
	}
	for _, o := range in.Orders {
		if err := ctx.Err(); err != nil {
			return Result{}, fmt.Errorf("calculate report: %w", err)
		}
		if overlaps(p.Start, p.End, o.Start, o.End) {
			add(0, 0, o.ID)
		}
	}
	if err := ctx.Err(); err != nil {
		return Result{}, fmt.Errorf("calculate report: %w", err)
	}
	if len(containers)+len(orders) > MaxReportBreakdowns {
		return Result{}, fmt.Errorf("report exceeds %d supported breakdowns", MaxReportBreakdowns)
	}
	r := Result{Version: "hosted-report-calculation/v1", Period: p, Metrics: calculateScope(ctx, p, in, scope{}), Containers: []Breakdown{}, Orders: []Breakdown{}, Reconciliations: []Reconciliation{}, Interruptions: interruptions(p, in), Obligations: obligationDetails(p, in), Deadlines: deadlines(p, in.Configuration)}
	remaining := int64(MaxResultJSONBytes)
	if err := jsonBudget(ctx, reflect.ValueOf(r), &remaining); err != nil {
		return Result{}, err
	}
	seenSources := map[string]bool{}
	addSource := func(source string) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("collect report sources: %w", err)
		}
		if seenSources[source] {
			return nil
		}
		remaining-- // Array member separator; the initial result already budgets the array.
		if err := jsonBudget(ctx, reflect.ValueOf(source), &remaining); err != nil {
			return err
		}
		seenSources[source] = true
		r.Sources = append(r.Sources, source)
		return nil
	}
	addMetricSources := func(metrics map[string]Metric) error {
		for _, metric := range metrics {
			for _, source := range metric.Sources {
				if err := addSource(source); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := addMetricSources(r.Metrics); err != nil {
		return Result{}, err
	}
	retainBreakdown := func(breakdown Breakdown) error {
		remaining--
		if err := jsonBudget(ctx, reflect.ValueOf(breakdown), &remaining); err != nil {
			return err
		}
		return addMetricSources(breakdown.Metrics)
	}
	for s := range containers {
		if err := ctx.Err(); err != nil {
			return Result{}, fmt.Errorf("calculate report: %w", err)
		}
		breakdown := Breakdown{SiteID: s.site, ContainerID: s.container, Metrics: calculateScope(ctx, p, in, s)}
		if err := retainBreakdown(breakdown); err != nil {
			return Result{}, err
		}
		r.Containers = append(r.Containers, breakdown)
	}
	sort.Slice(r.Containers, func(i, j int) bool {
		a, b := r.Containers[i], r.Containers[j]
		if a.SiteID == b.SiteID {
			return a.ContainerID < b.ContainerID
		}
		return a.SiteID < b.SiteID
	})
	for order := range orders {
		if err := ctx.Err(); err != nil {
			return Result{}, fmt.Errorf("calculate report: %w", err)
		}
		breakdown := Breakdown{OrderID: order, Metrics: calculateScope(ctx, p, in, scope{order: order})}
		if err := retainBreakdown(breakdown); err != nil {
			return Result{}, err
		}
		r.Orders = append(r.Orders, breakdown)
	}
	sort.Slice(r.Orders, func(i, j int) bool { return r.Orders[i].OrderID < r.Orders[j].OrderID })
	r.Reconciliations = reconcile(ctx, r)
	if err := jsonBudget(ctx, reflect.ValueOf(r.Reconciliations), &remaining); err != nil {
		return Result{}, err
	}
	r.Complete = true
	for _, m := range r.Metrics {
		if err := ctx.Err(); err != nil {
			return Result{}, fmt.Errorf("calculate report: %w", err)
		}
		if m.Status != Authoritative {
			r.Complete = false
		}
	}
	for _, v := range r.Interruptions {
		if err := addSource(v.SourceRef); err != nil {
			return Result{}, err
		}
	}
	for _, v := range r.Obligations {
		if err := addSource(v.SourceRef); err != nil {
			return Result{}, err
		}
	}
	if in.Configuration.ContractRef != "" {
		if err := addSource(in.Configuration.ContractRef); err != nil {
			return Result{}, err
		}
	}
	for _, target := range []Target{r.Deadlines.Contract, r.Deadlines.Internal} {
		for _, source := range target.Sources {
			if err := addSource(source); err != nil {
				return Result{}, err
			}
		}
	}
	slices.Sort(r.Sources)
	if err := resultLimits(ctx, r); err != nil {
		return Result{}, err
	}
	return r, nil
}
func contractMetricKeys() []string {
	return []string{"monthly_uptime", "capacity_weighted_site_uptime", "annual_uptime", "annual_energy_shortfall", "annual_service_credit", "rolling_three_month_uptime", "interim_credit_threshold_status", "exclusion_allowance_usage", "exclusion_allowance_section_4_a", "exclusion_allowance_section_4_b", "exclusion_allowance_section_4_e", "repair_credits_section_8_1", "termination_section_5_6", "period_uptime_hours", "period_downtime_hours", "curtailment_impact"}
}

func calculateScope(ctx context.Context, p Period, in Inputs, s scope) map[string]Metric {
	m := map[string]Metric{}
	for _, key := range RequiredMetricKeys() {
		if ctx.Err() != nil {
			return nil
		}
		m[key] = unavailable("", "required_evidence_unavailable")
	}
	for _, k := range contractMetricKeys() {
		if ctx.Err() != nil {
			return nil
		}
		m[k] = unavailable("", "approved_contract_formula_unimplemented")
		if !in.Configuration.Ready("G1") {
			v := m[k]
			v.Causes = append(v.Causes, "missing_G1")
			m[k] = v
		}
	}
	cs := coverage(p, in, s)
	if len(cs) == 0 {
		for key, v := range m {
			if ctx.Err() != nil {
				return nil
			}
			v.Causes = uniqueStrings(append(v.Causes, "commercial_coverage_unavailable"))
			m[key] = v
		}
		return m
	}
	meters(ctx, p, in, s, cs, m)
	fees(ctx, p, in, s, cs, m)
	hashrates(ctx, p, in, s, cs, m)
	incidentMetrics(ctx, p, in, s, cs, m)
	backlog(ctx, p, in, s, cs, m)
	reportingCounts(ctx, p, in, cs, m)
	externalMetrics(ctx, p, in, s, cs, m)
	if p.Kind == "annual" {
		annualComparisons(ctx, p, in, s, m)
	} else {
		for _, k := range []string{"year_over_year_uptime", "year_over_year_energy", "year_over_year_hashrate", "year_over_year_maintenance"} {
			if ctx.Err() != nil {
				return nil
			}
			m[k] = unavailable("", "annual_report_required")
		}
	}
	complete, refs := registerEvidenceCoversPeriod(p, in.Configuration, "coverage")
	for key, v := range m {
		if ctx.Err() != nil {
			return nil
		}
		if v.Status != Unavailable {
			v.Sources = uniqueStrings(append(v.Sources, refs...))
			if !complete {
				v.Status = Provisional
				v.Causes = uniqueStrings(append(v.Causes, "commercial_coverage_register_completeness_unverified"))
			}
			m[key] = v
		}
	}
	return m
}
func meters(ctx context.Context, p Period, in Inputs, s scope, cs []CoverageSegment, m map[string]Metric) {
	total := new(big.Rat)
	supported := 0
	causes := []string{}
	sources := scopeSources(cs)
	windows := map[string][]window{}
	key := func(site, container int64, order string) string {
		return fmt.Sprintf("%d:%d:%s", site, container, order)
	}
	for _, v := range in.Meters {
		if ctx.Err() != nil {
			return
		}
		if !s.matches(v.SiteID, v.ContainerID, v.OrderID) || !overlaps(p.Start, p.End, v.Start, v.End) {
			continue
		}
		sources = append(sources, v.SourceRef)
		if v.Quality == "missing" || v.Quality == "reset" {
			causes = append(causes, "meter_"+v.Quality)
			continue
		}
		if v.Start.Before(p.Start) || v.End.After(p.End) {
			causes = append(causes, "meter_period_boundary_without_interval_evidence")
			continue
		}
		if !meterAttributed(p, cs, v) {
			causes = append(causes, "meter_attribution_gap")
			continue
		}
		amount := rational(v.KWh)
		if v.AllocationFraction != "" {
			amount.Mul(amount, rational(v.AllocationFraction))
			sources = append(sources, in.Configuration.SharedAllocationRef)
		}
		total.Add(total, amount)
		supported++
		windows[key(v.SiteID, v.ContainerID, v.OrderID)] = append(windows[key(v.SiteID, v.ContainerID, v.OrderID)], window{v.Start, v.End})
		if v.Quality == meterQualityEstimated {
			causes = append(causes, "estimated_meter_energy")
		}
		certify(in.Configuration, v.SourceID, "meter", &causes, &sources)
	}
	for _, c := range cs {
		if ctx.Err() != nil {
			return
		}
		w, _ := intersect(window{c.Start, boundedEnd(c.End, p.End)}, periodWindow(p))
		if !covers(windows[key(c.SiteID, c.ContainerID, c.OrderID)], w) {
			causes = append(causes, "meter_interval_coverage_gap")
		}
	}
	gate(in.Configuration, "G3", &causes, &sources)
	if supported == 0 {
		v := unavailable("kWh", "meter_energy_unavailable")
		v.Causes = uniqueStrings(append(v.Causes, causes...))
		v.Sources = uniqueStrings(sources)
		m["metered_energy_kwh"] = v
		return
	}
	m["metered_energy_kwh"] = number(total, "kWh", "sum_meter_intervals_with_explicit_allocation", causes, sources)
}

func meterAttributed(p Period, cs []CoverageSegment, v MeterInterval) bool {
	return covers(attributedWindows(p, cs, v.SiteID, v.ContainerID, v.OrderID), window{v.Start, v.End})
}

// SupportedMeterIntervals returns whole measured or estimated intervals whose
// complete consumption window is covered by the account's commercial evidence.
func SupportedMeterIntervals(ctx context.Context, p Period, in Inputs) ([]MeterInterval, error) {
	cs := coverage(p, in, scope{})
	out := []MeterInterval{}
	for _, v := range in.Meters {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("select supported meter intervals: %w", err)
		}
		if (v.Quality == meterQualityMeasured || v.Quality == meterQualityEstimated) && !v.Start.Before(p.Start) && !v.End.After(p.End) && meterAttributed(p, cs, v) {
			out = append(out, v)
		}
	}
	return out, nil
}
func fees(ctx context.Context, p Period, in Inputs, s scope, cs []CoverageSegment, m map[string]Metric) {
	totals := map[string]*big.Rat{}
	windows := map[string][]window{}
	causes := []string{}
	complete, refs := registerEvidenceCoversPeriod(p, in.Configuration, "fees")
	if !complete {
		causes = append(causes, "fee_register_completeness_unverified")
	}
	sources := append(scopeSources(cs), refs...)
	key := func(site, container int64, order string) string {
		return fmt.Sprintf("%d:%d:%s", site, container, order)
	}
	for _, v := range in.Fees {
		if ctx.Err() != nil {
			return
		}
		if !s.matches(v.SiteID, v.ContainerID, v.OrderID) || !overlaps(p.Start, p.End, v.Start, v.End) {
			continue
		}
		sources = append(sources, v.SourceRef)
		if v.Start.Before(p.Start) || v.End.After(p.End) {
			causes = append(causes, "fee_period_boundary_without_accrual_evidence")
			continue
		}
		if !covers(attributedWindows(p, cs, v.SiteID, v.ContainerID, v.OrderID), window{v.Start, v.End}) {
			causes = append(causes, "fee_attribution_gap")
			continue
		}
		if totals[v.Currency] == nil {
			totals[v.Currency] = new(big.Rat)
		}
		totals[v.Currency].Add(totals[v.Currency], rational(v.Amount))
		windows[key(v.SiteID, v.ContainerID, v.OrderID)] = append(windows[key(v.SiteID, v.ContainerID, v.OrderID)], window{v.Start, v.End})
	}
	for _, c := range cs {
		if ctx.Err() != nil {
			return
		}
		w, _ := intersect(window{c.Start, boundedEnd(c.End, p.End)}, periodWindow(p))
		if !complete && !covers(windows[key(c.SiteID, c.ContainerID, c.OrderID)], w) {
			causes = append(causes, "fee_accrual_coverage_gap")
		}
	}
	gate(in.Configuration, "G3", &causes, &sources)
	if len(totals) == 0 {
		if complete && len(causes) == 0 {
			m["accrued_hosting_fees"] = number(new(big.Rat), "currency", "no_accruals_in_complete_register; no_currency_assumed", causes, sources)
			return
		}
		v := unavailable("currency", "accrued_fee_lines_unavailable")
		v.Causes = uniqueStrings(append(v.Causes, causes...))
		v.Sources = uniqueStrings(sources)
		m["accrued_hosting_fees"] = v
		return
	}
	v := Metric{Status: Authoritative, Unit: "currency", Method: "sum_finance_accruals_per_currency", Causes: uniqueStrings(causes), Sources: uniqueStrings(sources)}
	if len(causes) > 0 {
		v.Status = Provisional
	}
	for currency, sum := range totals {
		if ctx.Err() != nil {
			return
		}
		v.Amounts = append(v.Amounts, Amount{Currency: currency, Value: ratString(sum), Exact: sum.RatString()})
	}
	sort.Slice(v.Amounts, func(i, j int) bool { return v.Amounts[i].Currency < v.Amounts[j].Currency })
	m["accrued_hosting_fees"] = v
}
func hashrates(ctx context.Context, p Period, in Inputs, s scope, cs []CoverageSegment, m map[string]Metric) {
	integral, capacity := new(big.Rat), new(big.Rat)
	supported := false
	causes := []string{}
	sources := scopeSources(cs)
	for _, c := range cs {
		if ctx.Err() != nil {
			return
		}
		cw, _ := intersect(window{c.Start, boundedEnd(c.End, p.End)}, periodWindow(p))
		capacityHS := rational(c.CapacityTHS)
		capacityHS.Mul(capacityHS, big.NewRat(1000000000000, 1))
		capacity.Add(capacity, new(big.Rat).Mul(capacityHS, big.NewRat(cw.end.Sub(cw.start).Nanoseconds(), 1)))
		candidates := []HashrateInterval{}
		points := []time.Time{cw.start, cw.end}
		for _, h := range in.Hashrates {
			if ctx.Err() != nil {
				return
			}
			if h.AssetID != c.AssetID || h.Kind != c.Kind || h.SiteID != c.SiteID || h.ContainerID != c.ContainerID || h.OrderID != c.OrderID {
				continue
			}
			if w, ok := intersect(window{h.Start, h.End}, cw); ok {
				candidates = append(candidates, h)
				points = append(points, w.start, w.end)
			}
		}
		sort.Slice(points, func(i, j int) bool { return points[i].Before(points[j]) })
		points = slices.CompactFunc(points, func(a, b time.Time) bool { return a.Equal(b) })
		for i := range len(points) - 1 {
			if ctx.Err() != nil {
				return
			}
			a, b := points[i], points[i+1]
			active := []HashrateInterval{}
			for _, h := range candidates {
				if ctx.Err() != nil {
					return
				}
				if !h.Start.After(a) && !h.End.Before(b) {
					active = append(active, h)
				}
			}
			if len(active) != 1 {
				if len(active) > 1 {
					causes = append(causes, "overlapping_hashrate_evidence")
				} else {
					causes = append(causes, "hashrate_interval_coverage_gap")
				}
				continue
			}
			h := active[0]
			supported = true
			integral.Add(integral, new(big.Rat).Mul(rational(h.HashrateHS), big.NewRat(b.Sub(a).Nanoseconds(), 1)))
			sources = append(sources, h.SourceRef)
			certify(in.Configuration, h.SourceID, "hashrate", &causes, &sources)
			if !h.Complete {
				causes = append(causes, "incomplete_hashrate_interval")
			}
		}
	}
	gate(in.Configuration, "G3", &causes, &sources)
	if !supported {
		for _, key := range []string{"hashrate_performance_hs", "hashrate_performance_percent"} {
			if ctx.Err() != nil {
				return
			}
			v := unavailable("", "hashrate_intervals_unavailable")
			v.Causes = uniqueStrings(append(v.Causes, causes...))
			v.Sources = uniqueStrings(sources)
			m[key] = v
		}
		return
	}
	if slices.Contains(causes, "hashrate_interval_coverage_gap") || slices.Contains(causes, "overlapping_hashrate_evidence") {
		causes = append(causes, "partial_hashrate_contribution_not_complete_window_average")
	}
	m["hashrate_performance_hs"] = number(new(big.Rat).Quo(integral, big.NewRat(p.End.Sub(p.Start).Nanoseconds(), 1)), "H/s", "time_weighted_aggregate_over_report_window", causes, sources)
	ratio := new(big.Rat).Quo(integral, capacity)
	ratio.Mul(ratio, big.NewRat(100, 1))
	m["hashrate_performance_percent"] = number(ratio, "percent", "hashrate_integral_over_covered_capacity_time", causes, sources)
}

func interruptions(p Period, in Inputs) []Interruption {
	out := []Interruption{}
	for _, v := range in.Incidents {
		through := boundedEnd(v.End, minTime(v.AsOf, p.AsOf))
		if _, ok := intersect(window{v.Start, through}, periodWindow(p)); !ok && !unresolvedIncidentOverlaps(p, v) {
			continue
		}
		out = append(out, Interruption{ID: v.ID, SiteID: v.SiteID, ContainerID: v.ContainerID, OrderID: v.OrderID, Start: v.Start, End: v.End, ObservedThrough: through, Open: v.End.IsZero(), Category: v.Category, Summary: v.Summary, RootCause: v.RootCause, ClaimedExclusion: v.ExclusionClause, ExclusionStatus: Provisional, SourceRef: v.SourceRef})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Start.Equal(out[j].Start) {
			return out[i].ID < out[j].ID
		}
		return out[i].Start.Before(out[j].Start)
	})
	return out
}

// An open observation leaves the interval after its last observation unknown.
func unresolvedIncidentOverlaps(p Period, v Incident) bool {
	return v.End.IsZero() && v.AsOf.Before(p.End) && v.Start.Before(p.End)
}
func incidentMetrics(ctx context.Context, p Period, in Inputs, s scope, cs []CoverageSegment, m map[string]Metric) {
	windows := map[string][]window{}
	counts := map[string]int{}
	complete, refs := registerEvidenceCoversPeriod(p, in.Configuration, "incidents")
	sources := append(scopeSources(cs), refs...)
	causes := []string{}
	if !complete {
		causes = append(causes, "incident_register_completeness_unverified")
	}
	records := 0
	rootCauses := 0
	for _, v := range in.Incidents {
		if ctx.Err() != nil {
			return
		}
		if !s.matches(v.SiteID, v.ContainerID, v.OrderID) {
			continue
		}
		through := boundedEnd(v.End, minTime(v.AsOf, p.AsOf))
		aw := attributedWindows(p, cs, v.SiteID, v.ContainerID, v.OrderID)
		if unresolvedIncidentOverlaps(p, v) {
			for _, a := range aw {
				if ctx.Err() != nil {
					return
				}
				if _, ok := intersect(window{maxTime(v.Start, v.AsOf), p.End}, a); ok {
					causes = append(causes, "incident_observation_gap")
					sources = append(sources, v.SourceRef)
					break
				}
			}
		}
		w, ok := intersect(window{v.Start, through}, periodWindow(p))
		if !ok {
			continue
		}
		sources = append(sources, v.SourceRef)
		if !covers(aw, w) {
			causes = append(causes, "incident_attribution_gap")
		}
		supported := false
		for _, a := range aw {
			if ctx.Err() != nil {
				return
			}
			if x, ok := intersect(w, a); ok {
				supported = true
				windows["all"] = append(windows["all"], x)
				windows[v.Category] = append(windows[v.Category], x)
				if v.Category == "scheduled_maintenance" || v.Category == "unscheduled_maintenance" {
					windows["maintenance"] = append(windows["maintenance"], x)
				}
			}
		}
		if !supported {
			continue
		}
		records++
		counts[v.Category]++
		if v.RootCause != "" {
			rootCauses++
		} else {
			causes = append(causes, "root_cause_missing")
		}
		if v.End.IsZero() {
			causes = append(causes, "open_ended_interruption")
		}
	}
	if records == 0 && !complete {
		return
	}
	gate(in.Configuration, "G3", &causes, &sources)
	for key, category := range map[string]string{"interruption_duration_hours": "all", "scheduled_maintenance_hours": "scheduled_maintenance", "unscheduled_maintenance_hours": "unscheduled_maintenance", "maintenance_hours": "maintenance", "curtailment_duration_hours": "curtailment"} {
		if ctx.Err() != nil {
			return
		}
		m[key] = number(new(big.Rat).Quo(duration(windows[category]), big.NewRat(int64(time.Hour), 1)), "hours", "union_recorded_intervals", causes, sources)
	}
	for key, n := range map[string]int{"incidents": records, "outages": counts["outage"], "security_events": counts["security"], "root_cause_analysis": rootCauses} {
		if ctx.Err() != nil {
			return
		}
		m[key] = count(n, "records", "recorded_incident_details", causes, sources)
	}
	if records == 0 {
		m["interruption_exclusions"] = count(0, "records", "no_interruptions_in_complete_register", causes, sources)
		return
	}
	m["interruption_exclusions"] = Metric{Status: Unavailable, Unit: "classification", Causes: []string{"independent_exclusion_review_unavailable"}, Sources: uniqueStrings(sources)}
}
func episodeStarts(p Period, in Inputs) map[string]time.Time {
	out := map[string]time.Time{}
	for _, v := range in.Obligations {
		if v.ReceivedAt.After(p.AsOf) {
			continue
		}
		key := assetKey(v.Kind, v.AssetID) + ":" + v.EpisodeID
		old, ok := out[key]
		if !ok || v.Start.Before(old) {
			out[key] = v.Start
		}
	}
	return out
}
func obligationDetails(p Period, in Inputs) []ObligationDetail {
	starts := episodeStarts(p, in)
	out := []ObligationDetail{}
	for _, v := range in.Obligations {
		through := boundedEnd(v.End, minTime(v.AsOf, p.AsOf))
		if v.ReceivedAt.After(p.AsOf) || !overlaps(p.Start, p.End, v.Start, through) {
			continue
		}
		out = append(out, ObligationDetail{ID: v.ID, EpisodeID: v.EpisodeID, AssetID: v.AssetID, SiteID: v.SiteID, ContainerID: v.ContainerID, OrderID: v.OrderID, EpisodeStart: starts[assetKey(v.Kind, v.AssetID)+":"+v.EpisodeID], IntervalStart: v.Start, IntervalEnd: v.End, ObservedThrough: through, Severity: v.Severity, TicketRef: v.TicketRef, TicketAt: v.TicketAt, SubmittedAt: v.SubmittedAt, ReceivedAt: v.ReceivedAt, DiagnosedAt: v.DiagnosedAt, RespondedAt: v.RespondedAt, ApprovedAt: v.ApprovedAt, Deadline: v.Deadline, RestoredAt: v.RestoredAt, ClosedAt: v.ClosedAt, Qualified: v.Qualified, SourceRef: v.SourceRef})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].EpisodeStart.Equal(out[j].EpisodeStart) {
			return out[i].ID < out[j].ID
		}
		return out[i].EpisodeStart.Before(out[j].EpisodeStart)
	})
	return out
}

// Mappings establish repair scope only; performance continues to use board coverage.
func moduleRepairCoverage(p Period, in Inputs, s scope, cs []CoverageSegment) ([]CoverageSegment, bool) {
	modules := []CoverageSegment{}
	mappedBoards := map[string][]window{}
	for _, c := range cs {
		if c.Kind == 6 {
			modules = append(modules, c)
		}
	}
	for _, m := range in.ModuleCoverage {
		if !m.WholeModule || !s.matches(m.SiteID, m.ContainerID, m.OrderID) {
			continue
		}
		w, ok := intersect(window{m.Start, m.End}, periodWindow(p))
		if !ok {
			continue
		}
		modules = append(modules, CoverageSegment{ID: m.ID, Kind: 6, AssetID: m.ModuleID, SiteID: m.SiteID, ContainerID: m.ContainerID, OrderID: m.OrderID, Start: m.Start, End: m.End, SourceRef: m.SourceRef})
		for _, id := range m.BoardCoverageIDs {
			mappedBoards[id] = append(mappedBoards[id], w)
		}
	}
	incomplete := false
	for _, c := range cs {
		if c.Kind != 7 {
			continue
		}
		w, ok := intersect(window{c.Start, boundedEnd(c.End, p.End)}, periodWindow(p))
		if ok && !covers(mappedBoards[c.ID], w) {
			incomplete = true
		}
	}
	return modules, incomplete
}

// Ticket/episode facts do not require whole-module ownership. Their independent
// completeness and observation qualifications must not disappear with module counts.
func ticketMetrics(ctx context.Context, p Period, in Inputs, s scope, m map[string]Metric) {
	complete, sources := registerEvidenceCoversPeriod(p, in.Configuration, "obligations")
	causes := []string{}
	if !complete {
		causes = append(causes, "obligation_register_completeness_unverified")
	}
	episodes := map[string]bool{}
	for _, v := range in.Obligations {
		if ctx.Err() != nil {
			return
		}
		if !s.matches(v.SiteID, v.ContainerID, v.OrderID) || v.ReceivedAt.After(p.AsOf) {
			continue
		}
		through := boundedEnd(v.End, minTime(v.AsOf, p.AsOf))
		if _, ok := intersect(window{v.Start, through}, periodWindow(p)); !ok {
			continue
		}
		episodes[assetKey(v.Kind, v.AssetID)+":"+v.EpisodeID] = true
		sources = append(sources, v.SourceRef)
		if !v.Qualified {
			causes = append(causes, "unqualified_offline_episode")
		}
	}
	open, ticketSources, ticketCauses := openS3TicketsAtPeriodEnd(ctx, p, in, s)
	if !complete && len(episodes) == 0 && len(ticketSources) == 0 {
		return
	}
	gate(in.Configuration, "G1", &causes, &sources)
	gate(in.Configuration, "G3", &causes, &sources)
	m["ticket_history"] = count(len(episodes), "episodes", "reviewed_ticket_timestamp_details", causes, sources)
	m["open_S3_items"] = count(open, "tickets", "reviewed_open_S3_at_period_end", append(slices.Clone(causes), ticketCauses...), append(slices.Clone(sources), ticketSources...))
}

func backlog(ctx context.Context, p Period, in Inputs, s scope, cs []CoverageSegment, m map[string]Metric) {
	ticketMetrics(ctx, p, in, s, m)
	originalCoverage := cs
	cs, incompleteMapping := moduleRepairCoverage(p, in, s, cs)
	if incompleteMapping && len(cs) == 0 {
		for _, category := range backlogCategories() {
			for _, measure := range []string{"month_end", "daily_average"} {
				v := unavailable("modules", "module_coverage_incomplete")
				v.Sources = scopeSources(originalCoverage)
				m["backlog_"+category+"_"+measure] = v
			}
		}
		m["offline_units"] = m["backlog_offline_month_end"]
		return
	}
	starts := episodeStarts(p, in)
	complete, registerRefs := registerEvidenceCoversPeriod(p, in.Configuration, "obligations")
	byCategory := map[string]map[string][]window{}
	for _, category := range backlogCategories() {
		if ctx.Err() != nil {
			return
		}
		byCategory[category] = map[string][]window{}
	}
	causes := []string{}
	if incompleteMapping {
		causes = append(causes, "module_coverage_incomplete")
	}
	if !complete {
		causes = append(causes, "obligation_register_completeness_unverified")
	}
	deadlineCauses := []string{}
	sources := append(append(scopeSources(originalCoverage), scopeSources(cs)...), registerRefs...)
	supported := false
	deadlineKnown := false
	hasRecords := false
	seenAssets := map[string]bool{}
	add := func(category, asset string, w window) {
		if x, ok := intersect(w, periodWindow(p)); ok {
			byCategory[category][asset] = append(byCategory[category][asset], x)
		}
	}
	for _, v := range in.Obligations {
		if ctx.Err() != nil {
			return
		}
		if !s.matches(v.SiteID, v.ContainerID, v.OrderID) || v.ReceivedAt.After(p.AsOf) {
			continue
		}
		through := boundedEnd(v.End, minTime(v.AsOf, p.AsOf))
		asset := assetKey(v.Kind, v.AssetID)
		aws := []window{}
		for _, c := range cs {
			if ctx.Err() != nil {
				return
			}
			if c.Kind == v.Kind && c.AssetID == v.AssetID && c.SiteID == v.SiteID && c.ContainerID == v.ContainerID && c.OrderID == v.OrderID {
				if w, ok := intersect(window{c.Start, boundedEnd(c.End, p.End)}, periodWindow(p)); ok {
					aws = append(aws, w)
				}
			}
		}
		if len(aws) == 0 {
			continue
		}
		// A stale open observation can leave the entire reporting window
		// unknown even when its last observed interval ended before the period.
		if v.AsOf.Before(p.End) && v.End.IsZero() && v.RestoredAt.IsZero() {
			for _, a := range aws {
				if ctx.Err() != nil {
					return
				}
				if w, ok := intersect(window{maxTime(v.Start, v.AsOf), p.End}, a); ok {
					causes = append(causes, "obligation_observation_gap")
					sources = append(sources, v.SourceRef)
					add("unknown_coverage", asset, w)
				}
			}
		}
		vw, overlap := intersect(window{v.Start, through}, periodWindow(p))
		if !overlap {
			continue
		}
		hasRecords = true
		sources = append(sources, v.SourceRef)
		seenAssets[asset] = true
		if !covers(aws, vw) {
			causes = append(causes, "obligation_attribution_gap")
		}
		for _, aw := range aws {
			if ctx.Err() != nil {
				return
			}
			w, ok := intersect(vw, aw)
			if !ok {
				continue
			}
			if !v.Qualified {
				add("unknown_coverage", asset, w)
				causes = append(causes, "unqualified_offline_episode")
				continue
			}
			supported = true
			end := w.end
			if !v.RestoredAt.IsZero() {
				end = minTime(end, v.RestoredAt)
			}
			offline := window{w.start, end}
			add("offline", asset, offline)
			original := starts[asset+":"+v.EpisodeID]
			for _, days := range []int{7, 30, 60} {
				if ctx.Err() != nil {
					return
				}
				add(fmt.Sprintf("age_over_%d_days", days), asset, window{maxTime(offline.start, original.Add(time.Duration(days)*24*time.Hour)), offline.end})
			}
			ticketAt := v.TicketAt
			if ticketAt.IsZero() || (!v.SubmittedAt.IsZero() && v.SubmittedAt.Before(ticketAt)) {
				ticketAt = v.SubmittedAt
			}
			ticketEnd := offline.end
			if !v.ClosedAt.IsZero() {
				ticketEnd = minTime(ticketEnd, v.ClosedAt)
			}
			if !ticketAt.IsZero() && v.TicketRef != "" {
				category := "ticket_" + v.Severity
				if v.Severity == "" {
					category = "ticket_unclassified"
				}
				add(category, asset, window{maxTime(offline.start, ticketAt), ticketEnd})
			}
			if !v.Deadline.IsZero() {
				deadlineKnown = true
				add(backlogBeyondDeadline, asset, window{maxTime(offline.start, v.Deadline), offline.end})
			} else {
				deadlineCauses = append(deadlineCauses, "restoration_deadline_missing")
			}

		}
	}
	if !hasRecords && !complete {
		return
	}
	if complete && !hasRecords {
		supported = true
		deadlineKnown = true
	}
	for _, c := range cs {
		if ctx.Err() != nil {
			return
		}
		if !complete && c.Kind == 6 && !seenAssets[assetKey(c.Kind, c.AssetID)] {
			causes = append(causes, "obligation_history_coverage_unavailable")
		}
	}
	gate(in.Configuration, "G1", &causes, &sources)
	gate(in.Configuration, "G3", &causes, &sources)
	for _, category := range backlogCategories() {
		if ctx.Err() != nil {
			return
		}
		categoryCauses := slices.Clone(causes)
		// The number of unknown modules cannot be inferred from unmapped boards.
		if incompleteMapping && category == "unknown_coverage" {
			for _, measure := range []string{"month_end", "daily_average"} {
				v := unavailable("modules", "module_coverage_incomplete")
				v.Causes = uniqueStrings(append(v.Causes, categoryCauses...))
				v.Sources = uniqueStrings(sources)
				m["backlog_"+category+"_"+measure] = v
			}
			continue
		}
		if category == backlogBeyondDeadline {
			categoryCauses = append(categoryCauses, deadlineCauses...)
			gate(in.Configuration, "G5", &categoryCauses, &sources)
		}
		if !supported && category != "unknown_coverage" {
			continue
		}
		if category == backlogBeyondDeadline && !deadlineKnown {
			for _, measure := range []string{"month_end", "daily_average"} {
				if ctx.Err() != nil {
					return
				}
				v := unavailable("modules", "restoration_deadline_unavailable")
				v.Causes = uniqueStrings(append(v.Causes, categoryCauses...))
				v.Sources = uniqueStrings(sources)
				m["backlog_"+category+"_"+measure] = v
			}
			continue
		}
		n := 0
		probe := p.End.Add(-time.Nanosecond)
		for _, ws := range byCategory[category] {
			if ctx.Err() != nil {
				return
			}
			for _, w := range union(ws) {
				atStart := probe.Equal(w.start)
				strict := strings.HasPrefix(category, "age_over_") || category == backlogBeyondDeadline
				if !probe.Before(w.start) && probe.Before(w.end) && !(atStart && strict) {
					n++
					break
				}
			}
		}
		method := "reviewed_episode_count_last_instant"
		m["backlog_"+category+"_month_end"] = count(n, "modules", method, categoryCauses, sources)
		avg := dailyAverage(ctx, p, byCategory[category])
		m["backlog_"+category+"_daily_average"] = number(avg, "modules", "mean_local_daily_duration_integrals; age_days_are_24_elapsed_hours", categoryCauses, sources)
	}
	if supported {
		m["offline_units"] = m["backlog_offline_month_end"]
	}
}
func dailyAverage(ctx context.Context, p Period, byAsset map[string][]window) *big.Rat {
	loc, _ := time.LoadLocation(p.Timezone)
	start := p.Start.In(loc)
	day := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
	sum := new(big.Rat)
	days := 0
	normalized := make([][]window, 0, len(byAsset))
	for _, ws := range byAsset {
		if ctx.Err() != nil {
			return new(big.Rat)
		}
		normalized = append(normalized, union(ws))
	}
	for day.Before(p.End) {
		if ctx.Err() != nil {
			return new(big.Rat)
		}
		next := day.AddDate(0, 0, 1)
		dw, ok := intersect(window{day, next}, periodWindow(p))
		if ok {
			n := new(big.Rat)
			for _, ws := range normalized {
				if ctx.Err() != nil {
					return new(big.Rat)
				}
				clipped := []window{}
				for _, w := range ws {
					if ctx.Err() != nil {
						return new(big.Rat)
					}
					if c, ok := intersect(w, dw); ok {
						clipped = append(clipped, c)
					}
				}
				n.Add(n, duration(clipped))
			}
			sum.Add(sum, new(big.Rat).Quo(n, big.NewRat(dw.end.Sub(dw.start).Nanoseconds(), 1)))
			days++
		}
		day = next
	}
	if days > 0 {
		sum.Quo(sum, big.NewRat(int64(days), 1))
	}
	return sum
}
func reportingCounts(ctx context.Context, p Period, in Inputs, cs []CoverageSegment, m map[string]Metric) {
	probe := p.End.Add(-time.Nanosecond)
	active := map[string]CoverageSegment{}
	for _, c := range cs {
		if ctx.Err() != nil {
			return
		}
		if !c.Start.After(probe) && (c.End.IsZero() || c.End.After(probe)) {
			active[assetKey(c.Kind, c.AssetID)] = c
		}
	}
	if len(active) == 0 {
		return
	}
	sources := scopeSources(cs)
	causes := []string{}
	gate(in.Configuration, "G3", &causes, &sources)
	m["covered_units"] = count(len(active), "units", "commercial_coverage_at_period_end", causes, sources)
	states := map[string]int{}
	causes = append(causes, "pool_observation_freshness_rule_unimplemented")
	for asset, covered := range active {
		if ctx.Err() != nil {
			return
		}
		var latest *PoolObservation
		for _, v := range in.PoolObservations {
			if ctx.Err() != nil {
				return
			}
			if assetKey(v.Kind, v.AssetID) != asset || v.SourceAt.Before(covered.Start) || v.SourceAt.After(probe) || v.ReceivedAt.After(p.AsOf) {
				continue
			}
			if latest == nil || v.SourceAt.After(latest.SourceAt) || (v.SourceAt.Equal(latest.SourceAt) && v.ID < latest.ID) {
				observation := v
				latest = &observation
			}
		}
		if latest == nil {
			states["unknown"]++
			causes = append(causes, "pool_observation_missing")
			continue
		}
		conflict := false
		for _, v := range in.PoolObservations {
			if ctx.Err() != nil {
				return
			}
			if assetKey(v.Kind, v.AssetID) == asset && v.SourceAt.Equal(latest.SourceAt) && !v.ReceivedAt.After(p.AsOf) {
				sources = append(sources, v.SourceRef)
				certify(in.Configuration, v.SourceID, "pool", &causes, &sources)
				if v.State != latest.State {
					conflict = true
				}
			}
		}
		if conflict {
			states["unknown"]++
			causes = append(causes, "conflicting_pool_observations")
		} else {
			states[latest.State]++
		}
	}
	for _, state := range []string{"reporting", "failed", "unavailable", "unknown"} {
		if ctx.Err() != nil {
			return
		}
		m["pool_"+state+"_units"] = count(states[state], "units", "latest_pool_observation_not_qualified_connectivity", causes, sources)
	}
	m["operational_units"] = unavailable("units", "qualified_host_and_pool_reporting_state_unavailable")
}
func externalMetrics(ctx context.Context, p Period, in Inputs, s scope, cs []CoverageSegment, m map[string]Metric) {
	keys := append(contractMetricKeys(), "year_over_year_uptime")
	sites := map[int64]bool{}
	for _, c := range cs {
		if ctx.Err() != nil {
			return
		}
		sites[c.SiteID] = true
	}
	for _, key := range keys {
		if ctx.Err() != nil {
			return
		}
		matches := []ContractResult{}
		for _, r := range in.ExternalResults {
			if ctx.Err() != nil {
				return
			}
			if r.Metric != key || !r.Start.Equal(externalWindowStart(p, in.Configuration, key)) || !r.End.Equal(p.End) || !sites[r.SiteID] {
				continue
			}
			if s.container != 0 {
				if r.ContainerID != s.container || r.OrderID != "" {
					continue
				}
			} else if s.order != "" {
				if len(sites) != 1 || r.OrderID != s.order || r.ContainerID != 0 {
					continue
				}
			} else {
				if len(sites) != 1 || r.ContainerID != 0 || r.OrderID != "" {
					continue
				}
			}
			matches = append(matches, r)
		}
		if len(matches) == 0 {
			continue
		}
		if len(matches) > 1 {
			m[key] = unavailable("", "ambiguous_external_results")
			continue
		}
		r := matches[0]
		causes := []string{"externally_calculated_formula_unimplemented"}
		sources := append(scopeSources(cs), r.SourceRef, r.CalculationRef)
		gate(in.Configuration, "G1", &causes, &sources)
		gate(in.Configuration, "G3", &causes, &sources)
		m[key] = number(rational(r.Value), r.Unit, "external", causes, sources)
	}
}
func annualComparisons(ctx context.Context, p Period, in Inputs, s scope, m map[string]Metric) {
	prior := p
	prior.Kind = "ad_hoc"
	prior.End = p.Start
	loc, _ := time.LoadLocation(p.Timezone)
	prior.Start = p.Start.In(loc).AddDate(-1, 0, 0)
	keys := map[string]string{"year_over_year_energy": "metered_energy_kwh", "year_over_year_hashrate": "hashrate_performance_hs", "year_over_year_maintenance": "maintenance_hours"}
	if len(coverage(prior, in, s)) == 0 {
		for _, key := range []string{"year_over_year_uptime", "year_over_year_energy", "year_over_year_hashrate", "year_over_year_maintenance"} {
			if ctx.Err() != nil {
				return
			}
			m[key] = unavailable("", "first_reporting_year_no_prior_evidence")
		}
		return
	}
	pm := calculateScope(ctx, prior, in, s)
	for key, base := range keys {
		if ctx.Err() != nil {
			return
		}
		a, b := m[base], pm[base]
		if a.Status == Unavailable || b.Status == Unavailable || a.Exact == "" || b.Exact == "" {
			m[key] = unavailable(a.Unit, "prior_or_current_year_metric_unavailable")
			continue
		}
		causes := append(slices.Clone(a.Causes), b.Causes...)
		causes = append(causes, "estate_and_capacity_comparability_unapproved")
		m[key] = number(new(big.Rat).Sub(rational(a.Exact), rational(b.Exact)), a.Unit, "absolute_year_over_year_change_without_normalization", causes, append(slices.Clone(a.Sources), b.Sources...))
	}
	if m["year_over_year_uptime"].Method != "external" {
		m["year_over_year_uptime"] = unavailable("", "approved_contract_formula_unimplemented")
	}
}
func deadlines(p Period, c Configuration) Deadlines {
	if p.Kind == "ad_hoc" {
		if !p.RequestReceivedAt.IsZero() {
			loc, _ := time.LoadLocation(p.Timezone)
			causes := []string{"reasonable_efforts_target"}
			sources := []string{p.RequestRef}
			gate(c, "G5", &causes, &sources)
			return Deadlines{Contract: Target{Date: p.RequestReceivedAt.In(loc).AddDate(0, 0, 30).Format("2006-01-02"), Status: Provisional, Causes: uniqueStrings(causes), Sources: uniqueStrings(sources), Basis: "30_calendar_days_after_request_receipt", ReasonableEfforts: true}, Internal: Target{Status: Unavailable, Causes: []string{"ad_hoc_internal_target_unconfigured"}, Basis: "operational_target_unconfigured", ReasonableEfforts: true}}
		}
		return Deadlines{Contract: Target{Status: Unavailable, Causes: []string{"ad_hoc_request_receipt_unavailable"}, Basis: "30_calendar_days_after_request_receipt", ReasonableEfforts: true}, Internal: Target{Status: Unavailable, Causes: []string{"ad_hoc_request_receipt_unavailable"}, Basis: "operational_target_unconfigured", ReasonableEfforts: true}}
	}
	loc, _ := time.LoadLocation(p.Timezone)
	last := p.End.In(loc).AddDate(0, 0, -1)
	basis := "calendar_days_after_period_last_date"
	contract, internal := last.AddDate(0, 0, 30), last.AddDate(0, 0, 20)
	if p.Kind == "monthly" || p.Kind == reportKindQuarterly {
		contract = addBusinessDays(last, 15, c.Holidays)
		internal = addBusinessDays(last, 10, c.Holidays)
		basis = "business_days_after_period_last_date"
	}
	causes, sources := []string{}, []string{}
	gate(c, "G5", &causes, &sources)
	status := Authoritative
	if len(causes) > 0 {
		status = Provisional
	}
	return Deadlines{Contract: Target{Date: contract.Format("2006-01-02"), Status: status, Causes: uniqueStrings(causes), Sources: uniqueStrings(sources), Basis: basis}, Internal: Target{Date: internal.Format("2006-01-02"), Status: Provisional, Causes: uniqueStrings(append(causes, "proposed_internal_delivery_target")), Sources: uniqueStrings(sources), Basis: basis}}
}
func addBusinessDays(start time.Time, n int, holidays []string) time.Time {
	set := map[string]bool{}
	for _, h := range holidays {
		set[h] = true
	}
	for n > 0 {
		start = start.AddDate(0, 0, 1)
		if start.Weekday() != time.Saturday && start.Weekday() != time.Sunday && !set[start.Format("2006-01-02")] {
			n--
		}
	}
	return start
}
func reconcile(ctx context.Context, r Result) []Reconciliation {
	out := []Reconciliation{}
	type additive struct {
		key   string
		total *big.Rat
	}
	metrics := []additive{}
	for _, key := range []string{"metered_energy_kwh", "hashrate_performance_hs"} {
		if ctx.Err() != nil {
			return nil
		}
		m := r.Metrics[key]
		if m.Status != Unavailable && m.Exact != "" {
			metrics = append(metrics, additive{key, rational(m.Exact)})
		}
	}
	for _, amount := range r.Metrics["accrued_hosting_fees"].Amounts {
		if ctx.Err() != nil {
			return nil
		}
		metrics = append(metrics, additive{"accrued_hosting_fees:" + amount.Currency, rational(amount.Exact)})
	}
	for _, view := range []string{"container", "hosting_order"} {
		if ctx.Err() != nil {
			return nil
		}
		parts := r.Containers
		if view == "hosting_order" {
			parts = r.Orders
		}
		for _, metric := range metrics {
			if ctx.Err() != nil {
				return nil
			}
			sum := new(big.Rat)
			status := Authoritative
			for _, part := range parts {
				if ctx.Err() != nil {
					return nil
				}
				key := metric.key
				if strings.HasPrefix(key, "accrued_hosting_fees:") {
					currency := strings.TrimPrefix(key, "accrued_hosting_fees:")
					v := part.Metrics["accrued_hosting_fees"]
					if v.Status != Authoritative {
						status = Provisional
					}
					for _, a := range v.Amounts {
						if ctx.Err() != nil {
							return nil
						}
						if a.Currency == currency {
							sum.Add(sum, rational(a.Exact))
						}
					}
				} else {
					v := part.Metrics[key]
					if v.Status != Authoritative {
						status = Provisional
					}
					if v.Exact != "" {
						sum.Add(sum, rational(v.Exact))
					}
				}
			}
			baseKey := strings.SplitN(metric.key, ":", 2)[0]
			if r.Metrics[baseKey].Status != Authoritative {
				status = Provisional
			}
			out = append(out, Reconciliation{Metric: metric.key, View: view, ExactTotal: metric.total.RatString(), ExactParts: sum.RatString(), Matches: sum.Cmp(metric.total) == 0, Status: status})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].View == out[j].View {
			return out[i].Metric < out[j].Metric
		}
		return out[i].View < out[j].View
	})
	return out
}

// Completeness is evidence for a register, not an assumption from an empty list.
func registerEvidenceCoversPeriod(p Period, c Configuration, kind string) (bool, []string) {
	ws := []window{}
	sources := []string{}
	for _, r := range c.Registers {
		if r.Kind == kind {
			if w, ok := intersect(window{r.Start, r.End}, periodWindow(p)); ok {
				ws = append(ws, w)
				sources = append(sources, r.SourceRef)
			}
		}
	}
	return covers(ws, periodWindow(p)), uniqueStrings(sources)
}
func externalWindowStart(p Period, c Configuration, key string) time.Time {
	switch key {
	case "annual_uptime", "annual_energy_shortfall", "annual_service_credit", "exclusion_allowance_usage", "exclusion_allowance_section_4_a", "exclusion_allowance_section_4_b", "exclusion_allowance_section_4_e":
		loc, _ := time.LoadLocation(p.Timezone)
		last := p.End.Add(-time.Nanosecond).In(loc)
		start := time.Date(last.Year(), time.Month(c.ReportingYearMonth), c.ReportingYearDay, 0, 0, 0, 0, loc)
		if start.After(last) {
			start = start.AddDate(-1, 0, 0)
		}
		return start
	case "rolling_three_month_uptime", "interim_credit_threshold_status":
		return p.End.In(mustLocation(p.Timezone)).AddDate(0, -3, 0)
	default:
		return p.Start
	}
}
func mustLocation(name string) *time.Location { loc, _ := time.LoadLocation(name); return loc }

// A restored episode can retain an administratively open ticket. Count reviewed
// S3 ticket facts independently of the interval used for offline backlog.
func openS3TicketsAtPeriodEnd(ctx context.Context, p Period, in Inputs, s scope) (int, []string, []string) {
	probe := p.End.Add(-time.Nanosecond)
	latest := map[string]ObligationInterval{}
	for _, v := range in.Obligations {
		if ctx.Err() != nil {
			return 0, nil, nil
		}
		if v.ReceivedAt.After(p.AsOf) || v.Start.After(probe) {
			continue
		}
		key := assetKey(v.Kind, v.AssetID) + ":" + v.EpisodeID
		old, ok := latest[key]
		if !ok || v.Start.After(old.Start) || (v.Start.Equal(old.Start) && v.ID < old.ID) {
			latest[key] = v
		}
	}
	n := 0
	sources, causes := []string{}, []string{}
	for _, v := range latest {
		if ctx.Err() != nil {
			return 0, nil, nil
		}
		if !s.matches(v.SiteID, v.ContainerID, v.OrderID) || v.Severity != "S3" || !v.Qualified || v.TicketRef == "" || (!v.ClosedAt.IsZero() && !v.ClosedAt.After(probe)) {
			continue
		}
		ticket := v.TicketAt
		if ticket.IsZero() || (!v.SubmittedAt.IsZero() && v.SubmittedAt.Before(ticket)) {
			ticket = v.SubmittedAt
		}
		if ticket.IsZero() || ticket.After(probe) {
			continue
		}
		sources = append(sources, v.SourceRef)
		if v.AsOf.Before(probe) {
			causes = append(causes, "open_ticket_observation_gap")
			continue
		}
		n++
	}
	return n, uniqueStrings(sources), uniqueStrings(causes)
}

// MeteredEnergy applies the existing reviewed-evidence energy calculation to a
// self-service window without exposing the other host reporting inputs.
func MeteredEnergy(ctx context.Context, p Period, in Inputs) (Metric, error) {
	if err := p.Validate(); err != nil {
		return Metric{}, err
	}
	if err := in.ValidateContext(ctx); err != nil {
		return Metric{}, err
	}
	metrics := map[string]Metric{}
	cs := coverage(p, in, scope{})
	if err := ctx.Err(); err != nil {
		return Metric{}, fmt.Errorf("calculate metered energy: %w", err)
	}
	if len(cs) == 0 {
		return unavailable("kWh", "commercial_coverage_unavailable"), nil
	}
	meters(ctx, p, in, scope{}, cs, metrics)
	if err := ctx.Err(); err != nil {
		return Metric{}, fmt.Errorf("calculate metered energy: %w", err)
	}
	metric := metrics["metered_energy_kwh"]
	complete, _ := registerEvidenceCoversPeriod(p, in.Configuration, "coverage")
	if metric.Status != Unavailable && !complete {
		metric.Status = Provisional
		metric.Causes = uniqueStrings(append(metric.Causes, "commercial_coverage_register_completeness_unverified"))
	}
	return metric, nil
}

// AttributedMeterEnergy returns only the account's exact share of a reviewed
// interval. Missing and reset intervals have no consumption value.
func AttributedMeterEnergy(m MeterInterval) (string, error) {
	if m.Quality == "missing" || m.Quality == "reset" {
		return "", nil
	}
	amount, err := Decimal(m.KWh, false)
	if err != nil {
		return "", err
	}
	if m.AllocationFraction != "" {
		share, err := Decimal(m.AllocationFraction, false)
		if err != nil {
			return "", err
		}
		amount.Mul(amount, share)
	}
	return ratString(amount), nil
}
