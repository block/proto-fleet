package reporting

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"math"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// evidenceSchemaVersion versions the public evidence archive. Its manifest and
// the media type stored on the issued artifact both name it. v4 applies the
// shared public field policy to hourly evidence; clients cannot download v3 or
// earlier archives or their standalone HTML.
const evidenceSchemaVersion = "hosted-report-evidence/v4"

// PublicEvidenceMediaType identifies archives that contain only client-attributed consumption.
const PublicEvidenceMediaType = `application/zip; profile="` + evidenceSchemaVersion + `"`

const curtailmentControlCategory = "curtailment_control"

func observedInPeriod(p Period, start, through time.Time) bool {
	_, ok := intersect(window{start, minTime(through, p.AsOf)}, periodWindow(p))
	return ok
}

// PublicResult applies the same field policy to client detail, written HTML and
// archive tables. Metric evidence and required current explanations remain;
// prior-only prose, private tickets and internal control identities do not.
// It leaves the stored, reviewed result untouched and is safe to apply again.
func PublicResult(result Result) Result {
	result.Obligations = slices.Clone(result.Obligations)
	for i := range result.Obligations {
		result.Obligations[i].TicketRef = ""
	}
	result.Interruptions = slices.Clone(result.Interruptions)
	controlRefs := map[string]string{}
	controlIDs := map[string]string{}
	controls := []Interruption{}
	for _, v := range result.Interruptions {
		if v.Category == curtailmentControlCategory && strings.HasPrefix(v.SourceRef, "curtailment-confirmation:") {
			controls = append(controls, v)
		}
	}
	sort.Slice(controls, func(i, j int) bool {
		if controls[i].SourceRef == controls[j].SourceRef {
			return controls[i].ID < controls[j].ID
		}
		return controls[i].SourceRef < controls[j].SourceRef
	})
	windows := map[string]int{}
	for _, v := range controls {
		ref, exists := controlRefs[v.SourceRef]
		if !exists {
			ref = fmt.Sprintf("operational-observation:%d", len(controlRefs)+1)
			controlRefs[v.SourceRef] = ref
		}
		windows[ref]++
		controlIDs[v.ID] = fmt.Sprintf("%s:window:%d", ref, windows[ref])
	}
	for i := range result.Interruptions {
		v := &result.Interruptions[i]
		if !observedInPeriod(result.Period, v.Start, v.ObservedThrough) {
			v.Summary, v.RootCause, v.ClaimedExclusion = "", "", ""
		}
		if v.Category == curtailmentControlCategory && strings.HasPrefix(v.SourceRef, "curtailment-confirmation:") {
			v.ID, v.SourceRef = controlIDs[v.ID], controlRefs[v.SourceRef]
		}
	}
	projectRefs := func(refs []string) []string {
		refs = slices.Clone(refs)
		for i, ref := range refs {
			if replacement, ok := controlRefs[ref]; ok {
				refs[i] = replacement
			}
		}
		return refs
	}
	projectMetrics := func(metrics map[string]Metric) map[string]Metric {
		if metrics == nil {
			return nil
		}
		out := make(map[string]Metric, len(metrics))
		for key, metric := range metrics {
			metric.Sources = projectRefs(metric.Sources)
			out[key] = metric
		}
		return out
	}
	result.Metrics = projectMetrics(result.Metrics)
	result.Containers = slices.Clone(result.Containers)
	for i := range result.Containers {
		result.Containers[i].Metrics = projectMetrics(result.Containers[i].Metrics)
	}
	result.Orders = slices.Clone(result.Orders)
	for i := range result.Orders {
		result.Orders[i].Metrics = projectMetrics(result.Orders[i].Metrics)
	}
	result.Deadlines.Contract.Sources = projectRefs(result.Deadlines.Contract.Sources)
	result.Deadlines.Internal.Sources = projectRefs(result.Deadlines.Internal.Sources)
	result.Sources = projectRefs(result.Sources)
	return result
}

// ErrPublicReference identifies rejected public artifact references.
var ErrPublicReference = errors.New("invalid public evidence reference")

// Identity describes the immutable issued version, never its preparer or reviewer.
type Identity struct {
	ReportID         string    `json:"reportId"`
	Version          int64     `json:"version"`
	AccountID        string    `json:"accountId"`
	AccountName      string    `json:"accountName"`
	IssuedAt         time.Time `json:"issuedAt"`
	CorrectionOf     string    `json:"correctionOf,omitempty"`
	CorrectionReason string    `json:"correctionReason,omitempty"`
	Exception        string    `json:"exception,omitempty"`
}

// MaxHourlyObservations is the most interval-hours a report's monitoring
// evidence exports by the hour. Larger reports export UTC-day rollups: an hourly
// row is about 280 bytes, so this keeps the hourly table near 140 MB of the
// evidence content budget. One fully allocated CT1 is about 207,000
// interval-hours a month and 2.5 million a year.
const MaxHourlyObservations = 500_000

// DailyObservations reports whether a report's monitoring evidence is exported
// as UTC-day rollups instead of hourly rows: when its covered allocation
// intervals times the hours of its period exceed MaxHourlyObservations.
func DailyObservations(p Period, coverage []CoverageSegment) bool {
	intervals := map[int64]bool{}
	for _, c := range coverage {
		if c.Start.Before(p.End) && (c.End.IsZero() || c.End.After(p.Start)) {
			intervals[c.AllocationIntervalID] = true
		}
	}
	hours := (p.End.Sub(p.Start) + time.Hour - 1) / time.Hour
	return int64(len(intervals))*int64(hours) > MaxHourlyObservations
}

// ObservationValues are the per-metric aggregates of hourly monitoring
// evidence. Report counts include samples without a value. A nil sum, latest
// value or completeness means no sample carried one. The source window spans
// every metric. Zero times are absent.
type ObservationValues struct {
	HashrateReports, HashrateCount, HashratePositive int64
	HashrateSum                                      *float64
	HashrateComplete                                 *bool
	PowerReports, PowerCount                         int64
	PowerSum                                         *float64
	PowerComplete                                    *bool
	TemperatureReports, TemperatureCount             int64
	TemperatureSum, TemperatureLatest                *float64
	TemperatureLatestAt                              time.Time
	StateReports, StateCount                         int64
	StateLatest                                      *float64
	StateLatestAt                                    time.Time
	SourceOldestAt, SourceNewestAt                   time.Time
}

// Observation is the client-safe projection of one allocated interval's frozen
// evidence for one UTC hour: the revision the report selected, or a gap hour
// (CaptureReason "gap", Revision 0) that the capture job never wrote.
type Observation struct {
	IntervalID, AssetID, SiteID int64
	Kind                        int32
	Bucket                      time.Time
	Revision                    int32
	CapturedAt                  time.Time
	CaptureReason, GapReason    string
	NoSourceData                bool
	WindowStart, WindowEnd      time.Time
	ObservationValues
}

// ObservationDay rolls one interval's hourly evidence up by UTC day. Hours
// counts evidence hours and GapHours gap hours; Revision and CapturedAt are the
// highest among the day's hours.
type ObservationDay struct {
	IntervalID, AssetID, SiteID                                           int64
	Kind                                                                  int32
	Day                                                                   time.Time
	Hours, GapHours, RevisedHours, UnattributableHours, NoSourceDataHours int32
	Revision                                                              int32
	CapturedAt                                                            time.Time
	WindowStart, WindowEnd                                                time.Time
	ObservationValues
}

// ObservationSource yields a report's frozen monitoring evidence once each, in
// strictly ascending (interval, time) order. Days is used when
// DailyObservations, otherwise Hours. A nil source is empty.
type ObservationSource struct {
	Hours func(yield func(Observation) error) error
	Days  func(yield func(ObservationDay) error) error
}

type ArtifactFile struct {
	Name, MediaType string
	Data            []byte
	SHA256          string
}

// BuildArtifacts is a convenience for small snapshots with hourly evidence.
// Publication of large snapshots uses WriteEvidence with a paged provider.
func BuildArtifacts(result Result, inputs Inputs, identity Identity, observations []Observation) ([]ArtifactFile, error) {
	written, err := BuildWrittenReport(result, identity)
	if err != nil {
		return nil, err
	}
	observations = slices.Clone(observations)
	sort.Slice(observations, func(i, j int) bool {
		if observations[i].IntervalID != observations[j].IntervalID {
			return observations[i].IntervalID < observations[j].IntervalID
		}
		return observations[i].Bucket.Before(observations[j].Bucket)
	})
	var evidence bytes.Buffer
	err = WriteEvidence(&evidence, result, inputs, identity, ObservationSource{Hours: func(yield func(Observation) error) error {
		for _, observation := range observations {
			if err := yield(observation); err != nil {
				return err
			}
		}
		return nil
	}})
	if err != nil {
		return nil, err
	}
	return []ArtifactFile{artifact("report.html", "text/html; charset=utf-8", written), artifact("evidence.zip", PublicEvidenceMediaType, evidence.Bytes())}, nil
}
func artifact(name, mediaType string, data []byte) ArtifactFile {
	digest := sha256.Sum256(data)
	return ArtifactFile{Name: name, MediaType: mediaType, Data: data, SHA256: hex.EncodeToString(digest[:])}
}
func exportInstant(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
func publicPeriod(p Period) Period {
	p.Start = p.Start.UTC()
	p.End = p.End.UTC()
	p.AsOf = p.AsOf.UTC()
	p.RequestReceivedAt = p.RequestReceivedAt.UTC()
	return p
}

// Public evidence references must already have undergone client-visibility
// review. This narrow check rejects credentials embedded in otherwise reviewed
// references; it is not a classifier for arbitrary narrative or private URLs.
func checkReference(ref string) error {
	lower := strings.ToLower(ref)
	for _, key := range []string{"password=", "passwd=", "token=", "secret=", "api_key=", "apikey=", "credential=", "authorization=", "signature="} {
		if strings.Contains(lower, key) {
			return fmt.Errorf("%w: credential-bearing evidence reference", ErrPublicReference)
		}
	}
	if !strings.Contains(ref, "://") && !strings.HasPrefix(ref, "//") {
		return nil
	}
	u, err := url.Parse(ref)
	if err != nil {
		return fmt.Errorf("%w: invalid evidence reference", ErrPublicReference)
	}
	if u.User != nil {
		return fmt.Errorf("%w: credential-bearing evidence reference", ErrPublicReference)
	}
	for key := range u.Query() {
		k := strings.ToLower(key)
		if strings.HasPrefix(k, "x-amz-") || slices.Contains([]string{"key", "token", "access_token", "auth", "password", "passwd", "secret", "credential", "signature", "api-key", "api_key", "apikey"}, k) {
			return fmt.Errorf("%w: credential-bearing evidence reference", ErrPublicReference)
		}
	}
	return nil
}

type evidenceColumn struct {
	name    string
	numeric bool
}
type evidenceTable struct {
	name    string
	columns []evidenceColumn
	rows    [][]any
}

func table(name string, columns string) evidenceTable {
	t := evidenceTable{name: name, rows: [][]any{}}
	for _, name := range strings.Fields(columns) {
		numeric := strings.HasSuffix(name, ":n")
		t.columns = append(t.columns, evidenceColumn{strings.TrimSuffix(name, ":n"), numeric})
	}
	return t
}
func (t *evidenceTable) add(values ...any) { t.rows = append(t.rows, values) }

// canonical orders rows by their JSON encoding, encoding each row once.
func (t *evidenceTable) canonical(ctx context.Context) error {
	type keyed struct {
		key []byte
		row []any
	}
	rows := make([]keyed, len(t.rows))
	for i, row := range t.rows {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("prepare report output: %w", err)
		}
		key, err := json.Marshal(row)
		if err != nil {
			return fmt.Errorf("encode %s row: %w", t.name, err)
		}
		rows[i] = keyed{key: key, row: row}
	}
	slices.SortFunc(rows, func(a, b keyed) int { return bytes.Compare(a.key, b.key) })
	for i := range rows {
		t.rows[i] = rows[i].row
	}
	return nil
}
func (t *evidenceTable) headers() []string {
	out := make([]string, len(t.columns))
	for i, c := range t.columns {
		out[i] = c.name
	}
	return out
}
func csvText(value string) string {
	trimmed := strings.TrimLeftFunc(value, unicode.IsSpace)
	if len(trimmed) > 0 && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	return value
}
func csvCell(value any, numeric bool) (string, error) {
	var s string
	switch v := value.(type) {
	case nil:
		return "", nil
	case string:
		s = v
	case time.Time:
		s = exportInstant(v)
	default:
		s = fmt.Sprint(v)
	}
	if numeric && s != "" {
		parts := strings.Split(s, "/")
		if len(parts) > 2 {
			return "", fmt.Errorf("invalid numeric evidence cell")
		}
		for _, p := range parts {
			if !decimalPattern.MatchString(p) {
				return "", fmt.Errorf("invalid numeric evidence cell")
			}
		}
		return s, nil
	}
	return csvText(s), nil
}
func (t *evidenceTable) writeCSV(w io.Writer) error {
	writer := csv.NewWriter(w)
	if err := writer.Write(t.headers()); err != nil {
		return fmt.Errorf("write evidence CSV headers: %w", err)
	}
	for _, row := range t.rows {
		cells := make([]string, len(row))
		for i, v := range row {
			s, e := csvCell(v, t.columns[i].numeric)
			if e != nil {
				return e
			}
			cells[i] = s
		}
		if e := writer.Write(cells); e != nil {
			return fmt.Errorf("write evidence CSV row: %w", e)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("flush evidence CSV: %w", err)
	}
	return nil
}
func validateTableReferences(ctx context.Context, t evidenceTable) error {
	for _, row := range t.rows {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("validate report references: %w", err)
		}
		for i, c := range t.columns {
			if strings.HasSuffix(c.name, "_ref") {
				if ref, ok := row[i].(string); ok {
					if err := checkReference(ref); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// publicEvidenceConfiguration projects configuration for the client archive,
// not for validation or calculation. The caller must still validate and
// authorize the complete input; this does not classify references as safe.
func publicEvidenceConfiguration(ctx context.Context, result Result, in Inputs) (Configuration, error) {
	p := result.Period
	deps, err := collectPublicationDependencies(ctx, result, in)
	if err != nil {
		return Configuration{}, err
	}
	original := in.Configuration
	// Configuration is an allowlist projection. Host owners, representatives,
	// delivery configuration and schedules never become public metadata.
	c := Configuration{ContractRef: original.ContractRef, Timezone: original.Timezone, ReportingYearMonth: original.ReportingYearMonth, ReportingYearDay: original.ReportingYearDay}

	var priorStart time.Time
	if p.Kind == "annual" {
		priorStart = p.Start.In(mustLocation(p.Timezone)).AddDate(-1, 0, 0)
	}
	for _, r := range in.Configuration.Registers {
		if err := ctx.Err(); err != nil {
			return Configuration{}, fmt.Errorf("project report configuration: %w", err)
		}
		current := slices.Contains([]string{"coverage", "fees", "incidents", "obligations"}, r.Kind) && overlaps(p.Start, p.End, r.Start, r.End)
		// The prior comparison uses energy, hashrate and maintenance. Its
		// coverage and incident completeness affect their status and sources.
		previous := !priorStart.IsZero() && (r.Kind == "coverage" || r.Kind == "incidents") && overlaps(priorStart, p.Start, r.Start, r.End)
		if current || previous {
			c.Registers = append(c.Registers, r)
		}
	}

	shared := false
	meterPeriods := []Period{p}
	if p.Kind == "annual" {
		prior := p
		prior.Kind = "ad_hoc"
		prior.Start = priorStart
		prior.End = p.Start
		meterPeriods = append(meterPeriods, prior)
	}
	for _, period := range meterPeriods {
		cs := coverage(period, in, scope{})
		for _, v := range in.Meters {
			if err := ctx.Err(); err != nil {
				return Configuration{}, fmt.Errorf("project report configuration: %w", err)
			}
			if (v.Quality != meterQualityMeasured && v.Quality != meterQualityEstimated) || v.Start.Before(period.Start) || v.End.After(period.End) || !meterAttributed(period, cs, v) {
				continue
			}
			shared = shared || v.AllocationFraction != ""
		}
	}
	for _, v := range in.Configuration.Sources {
		if err := ctx.Err(); err != nil {
			return Configuration{}, fmt.Errorf("project report configuration: %w", err)
		}
		if deps.sources[publicationSourceKey{v.ID, v.Metric}] {
			v.Certified = original.Certified(v.ID, v.Metric)
			if !v.Certified {
				v.SourceRef = ""
			}
			c.Sources = append(c.Sources, v)
		}
	}
	if shared {
		c.SharedAllocationRef = original.SharedAllocationRef
	}
	for _, g := range in.Configuration.Prerequisites {
		if deps.prerequisites[g.Key] {
			g.Ready = original.Ready(g.Key)
			g.Owner = ""
			if !g.Ready {
				g.SourceRef = ""
			}
			c.Prerequisites = append(c.Prerequisites, g)
		}
	}
	if p.Kind == "monthly" || p.Kind == reportKindQuarterly {
		last := p.End.In(mustLocation(p.Timezone)).AddDate(0, 0, -1)
		contract := addBusinessDays(last, 15, in.Configuration.Holidays).Format("2006-01-02")
		first := last.Format("2006-01-02")
		for _, h := range in.Configuration.Holidays {
			if err := ctx.Err(); err != nil {
				return Configuration{}, fmt.Errorf("project report configuration: %w", err)
			}
			day, err := time.Parse("2006-01-02", h)
			if err != nil {
				return Configuration{}, fmt.Errorf("project report configuration: invalid holiday: %w", err)
			}
			if h > first && h <= contract && day.Weekday() != time.Saturday && day.Weekday() != time.Sunday {
				c.Holidays = append(c.Holidays, h)
			}
		}
	}
	return c, nil
}

func publicEvidenceInputTables(ctx context.Context, result Result, in Inputs) ([]evidenceTable, error) {
	p := result.Period
	type meterWindow struct {
		name     string
		period   Period
		coverage []CoverageSegment
	}
	meterWindows := []meterWindow{{name: "current", period: p, coverage: coverage(p, in, scope{})}}
	if p.Kind == "annual" {
		prior := p
		prior.Kind = "ad_hoc"
		prior.Start = p.Start.In(mustLocation(p.Timezone)).AddDate(-1, 0, 0)
		prior.End = p.Start
		meterWindows = append(meterWindows, meterWindow{name: "prior_year", period: prior, coverage: coverage(prior, in, scope{})})
	}
	config := table("inputs/configuration", "contract_ref timezone reporting_year_month:n reporting_year_day:n shared_allocation_ref")
	c, err := publicEvidenceConfiguration(ctx, result, in)
	if err != nil {
		return nil, err
	}
	config.add(c.ContractRef, c.Timezone, c.ReportingYearMonth, c.ReportingYearDay, c.SharedAllocationRef)
	prerequisites := table("inputs/prerequisites", "key ready source_ref")
	for _, v := range c.Prerequisites {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prepare report output: %w", err)
		}
		prerequisites.add(v.Key, v.Ready, v.SourceRef)
	}
	calendar := table("inputs/holidays", "date")
	for _, v := range c.Holidays {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prepare report output: %w", err)
		}
		calendar.add(v)
	}
	sources := table("inputs/source_qualifications", "id metric certified source_ref")
	for _, v := range c.Sources {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prepare report output: %w", err)
		}
		sources.add(v.ID, v.Metric, v.Certified, v.SourceRef)
	}
	registers := table("inputs/register_completeness", "kind start end source_ref")
	for _, v := range c.Registers {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prepare report output: %w", err)
		}
		registers.add(v.Kind, exportInstant(v.Start), exportInstant(v.End), v.SourceRef)
	}
	orders := table("inputs/orders", "id name start end source_ref")
	for _, v := range in.Orders {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prepare report output: %w", err)
		}
		orders.add(v.ID, v.Name, exportInstant(v.Start), exportInstant(v.End), v.SourceRef)
	}
	coverage := table("inputs/coverage", "id allocation_interval_id:n kind:n asset_id:n site_id:n container_id:n order_id start end commissioned_at capacity_ths:n replacement_of source_ref")
	for _, v := range in.Coverage {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prepare report output: %w", err)
		}
		coverage.add(v.ID, v.AllocationIntervalID, v.Kind, v.AssetID, v.SiteID, v.ContainerID, v.OrderID, exportInstant(v.Start), exportInstant(v.End), exportInstant(v.CommissionedAt), v.CapacityTHS, v.ReplacementOf, v.SourceRef)
	}
	moduleCoverage := table("inputs/module_coverage", "id module_id:n site_id:n container_id:n order_id start end board_coverage_ids whole_module source_ref")
	for _, v := range in.ModuleCoverage {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prepare report output: %w", err)
		}
		moduleCoverage.add(v.ID, v.ModuleID, v.SiteID, v.ContainerID, v.OrderID, exportInstant(v.Start), exportInstant(v.End), strings.Join(v.BoardCoverageIDs, ";"), v.WholeModule, v.SourceRef)
	}
	meters := table("inputs/meters", "id meter_id source_id site_id:n container_id:n order_id start end attributed_kwh:n quality source_ref comparison_window")
	for _, v := range in.Meters {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prepare report output: %w", err)
		}
		for _, w := range meterWindows {
			if v.Start.Before(w.period.Start) || v.End.After(w.period.End) || !overlaps(w.period.Start, w.period.End, v.Start, v.End) || !covers(attributedWindows(w.period, w.coverage, v.SiteID, v.ContainerID, v.OrderID), window{v.Start, v.End}) {
				continue
			}
			amount, err := AttributedMeterEnergy(v)
			if err != nil {
				return nil, fmt.Errorf("attribute public meter interval: %w", err)
			}
			meters.add(v.ID, v.MeterID, v.SourceID, v.SiteID, v.ContainerID, v.OrderID, exportInstant(v.Start), exportInstant(v.End), amount, v.Quality, v.SourceRef, w.name)
			break
		}
	}
	fees := table("inputs/fees", "id site_id:n container_id:n order_id start end amount:n currency source_ref")
	for _, v := range in.Fees {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prepare report output: %w", err)
		}
		fees.add(v.ID, v.SiteID, v.ContainerID, v.OrderID, exportInstant(v.Start), exportInstant(v.End), v.Amount, v.Currency, v.SourceRef)
	}
	incidents := table("inputs/incidents", "id site_id:n container_id:n order_id true_start end as_of category summary root_cause exclusion_clause source_ref root_cause_recorded narrative_redacted")
	for _, v := range in.Incidents {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prepare report output: %w", err)
		}
		// Prior-only incidents can affect annual maintenance and its causes,
		// including whether a root cause was recorded. Their narrative and
		// exclusion claim are not used in that comparison. Do not change the
		// reviewed input used by the calculator or the frozen host artifact.
		redacted := !observedInPeriod(p, v.Start, boundedEnd(v.End, minTime(v.AsOf, p.AsOf)))
		summary, rootCause, exclusion := v.Summary, v.RootCause, v.ExclusionClause
		if redacted {
			summary, rootCause, exclusion = "", "", ""
		}
		incidents.add(v.ID, v.SiteID, v.ContainerID, v.OrderID, exportInstant(v.Start), exportInstant(v.End), exportInstant(v.AsOf), v.Category, summary, rootCause, exclusion, v.SourceRef, v.RootCause != "", redacted)
	}
	hashrates := table("inputs/hashrates", "id asset_id:n kind:n site_id:n container_id:n order_id source_id start end hashrate_hs:n complete source_ref")
	for _, v := range in.Hashrates {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prepare report output: %w", err)
		}
		hashrates.add(v.ID, v.AssetID, v.Kind, v.SiteID, v.ContainerID, v.OrderID, v.SourceID, exportInstant(v.Start), exportInstant(v.End), v.HashrateHS, v.Complete, v.SourceRef)
	}
	pool := table("inputs/pool_observations", "id asset_id:n kind:n source_id source_at received_at state source_ref")
	for _, v := range in.PoolObservations {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prepare report output: %w", err)
		}
		pool.add(v.ID, v.AssetID, v.Kind, v.SourceID, exportInstant(v.SourceAt), exportInstant(v.ReceivedAt), v.State, v.SourceRef)
	}
	obligations := table("inputs/obligations", "id episode_id asset_id:n kind:n site_id:n container_id:n order_id true_start end as_of severity ticket_at submitted_at received_at diagnosed_at responded_at approved_at deadline restored_at closed_at qualified source_ref")
	for _, v := range in.Obligations {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prepare report output: %w", err)
		}
		obligations.add(v.ID, v.EpisodeID, v.AssetID, v.Kind, v.SiteID, v.ContainerID, v.OrderID, exportInstant(v.Start), exportInstant(v.End), exportInstant(v.AsOf), v.Severity, exportInstant(v.TicketAt), exportInstant(v.SubmittedAt), exportInstant(v.ReceivedAt), exportInstant(v.DiagnosedAt), exportInstant(v.RespondedAt), exportInstant(v.ApprovedAt), exportInstant(v.Deadline), exportInstant(v.RestoredAt), exportInstant(v.ClosedAt), v.Qualified, v.SourceRef)
	}
	external := table("inputs/external_results", "id metric site_id:n container_id:n order_id start end value:n unit source_ref calculation_ref")
	for _, v := range in.ExternalResults {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prepare report output: %w", err)
		}
		external.add(v.ID, v.Metric, v.SiteID, v.ContainerID, v.OrderID, exportInstant(v.Start), exportInstant(v.End), v.Value, v.Unit, v.SourceRef, v.CalculationRef)
	}
	tables := []evidenceTable{config, prerequisites, calendar, sources, registers, orders, coverage, moduleCoverage, meters, fees, incidents, hashrates, pool, obligations, external}
	for i := range tables {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prepare report output: %w", err)
		}
		if err := tables[i].canonical(ctx); err != nil {
			return nil, err
		}
		if err := validateTableReferences(ctx, tables[i]); err != nil {
			return nil, err
		}
	}
	return tables, nil
}

func metricTables(ctx context.Context, r Result) (evidenceTable, error) {
	t := table("metrics", "view site_id:n container_id:n order_id metric label status value:n exact:n unit currency causes method sources")
	add := func(view string, b Breakdown) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("prepare report metrics: %w", err)
		}
		keys := make([]string, 0, len(b.Metrics))
		for key := range b.Metrics {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("prepare report metrics: %w", err)
			}
			v := b.Metrics[key]
			for _, ref := range v.Sources {
				if e := checkReference(ref); e != nil {
					return e
				}
			}
			causes := strings.Join(uniqueStrings(v.Causes), "; ")
			sources := strings.Join(uniqueStrings(v.Sources), "; ")
			value, exact := v.Value, v.Exact
			if v.Status == Unavailable {
				value = ""
				exact = ""
			}
			if len(v.Amounts) == 0 || v.Status == Unavailable {
				t.add(view, b.SiteID, b.ContainerID, b.OrderID, key, humanLabel(key), v.Status, value, exact, v.Unit, "", causes, v.Method, sources)
			} else {
				amounts := slices.Clone(v.Amounts)
				sort.Slice(amounts, func(i, j int) bool { return amounts[i].Currency < amounts[j].Currency })
				for _, a := range amounts {
					t.add(view, b.SiteID, b.ContainerID, b.OrderID, key, humanLabel(key), v.Status, a.Value, a.Exact, v.Unit, a.Currency, causes, v.Method, sources)
				}
			}
		}
		return nil
	}
	if e := add("total", Breakdown{Metrics: r.Metrics}); e != nil {
		return t, e
	}
	for _, b := range r.Containers {
		if e := add("container", b); e != nil {
			return t, e
		}
	}
	for _, b := range r.Orders {
		if e := add("order", b); e != nil {
			return t, e
		}
	}
	return t, t.canonical(ctx)
}
func resultTables(ctx context.Context, r Result) ([]evidenceTable, error) {
	metrics, err := metricTables(ctx, r)
	if err != nil {
		return nil, err
	}
	interruptions := table("interruptions", "id site_id:n container_id:n order_id true_start period_clipped_start period_clipped_end end observed_through open category summary root_cause claimed_exclusion exclusion_status source_ref")
	for _, v := range r.Interruptions {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prepare report output: %w", err)
		}
		interruptions.add(v.ID, v.SiteID, v.ContainerID, v.OrderID, exportInstant(v.Start), exportInstant(maxTime(v.Start, r.Period.Start)), exportInstant(maxTime(maxTime(v.Start, r.Period.Start), minTime(v.ObservedThrough, r.Period.End))), exportInstant(v.End), exportInstant(v.ObservedThrough), v.Open, v.Category, v.Summary, v.RootCause, v.ClaimedExclusion, v.ExclusionStatus, v.SourceRef)
	}
	obligations := table("obligations", "id episode_id asset_id:n site_id:n container_id:n order_id true_episode_start interval_start period_clipped_start period_clipped_end interval_end observed_through severity ticket_at submitted_at received_at diagnosed_at responded_at approved_at deadline restored_at closed_at qualified source_ref")
	for _, v := range r.Obligations {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prepare report output: %w", err)
		}
		obligations.add(v.ID, v.EpisodeID, v.AssetID, v.SiteID, v.ContainerID, v.OrderID, exportInstant(v.EpisodeStart), exportInstant(v.IntervalStart), exportInstant(maxTime(v.IntervalStart, r.Period.Start)), exportInstant(minTime(v.ObservedThrough, r.Period.End)), exportInstant(v.IntervalEnd), exportInstant(v.ObservedThrough), v.Severity, exportInstant(v.TicketAt), exportInstant(v.SubmittedAt), exportInstant(v.ReceivedAt), exportInstant(v.DiagnosedAt), exportInstant(v.RespondedAt), exportInstant(v.ApprovedAt), exportInstant(v.Deadline), exportInstant(v.RestoredAt), exportInstant(v.ClosedAt), v.Qualified, v.SourceRef)
	}
	reconciliations := table("reconciliation", "metric view exact_total:n exact_parts:n matches status")
	for _, v := range r.Reconciliations {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prepare report output: %w", err)
		}
		reconciliations.add(v.Metric, v.View, v.ExactTotal, v.ExactParts, v.Matches, v.Status)
	}
	deadlines := table("deadlines", "kind date status causes sources basis reasonable_efforts")
	for _, v := range []struct {
		kind   string
		target Target
	}{{"contract", r.Deadlines.Contract}, {"internal", r.Deadlines.Internal}} {
		for _, ref := range v.target.Sources {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("prepare report output: %w", err)
			}
			if err := checkReference(ref); err != nil {
				return nil, err
			}
		}
		deadlines.add(v.kind, v.target.Date, v.target.Status, strings.Join(uniqueStrings(v.target.Causes), "; "), strings.Join(uniqueStrings(v.target.Sources), "; "), v.target.Basis, v.target.ReasonableEfforts)
	}
	out := []evidenceTable{metrics, interruptions, obligations, reconciliations, deadlines}
	for i := range out {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prepare report output: %w", err)
		}
		if err := out[i].canonical(ctx); err != nil {
			return nil, err
		}
		if err := validateTableReferences(ctx, out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

const observationValueColumns = " hashrate_reports:n hashrate_count:n hashrate_positive:n hashrate_sum_hs:n hashrate_complete power_reports:n power_count:n power_sum_w:n power_complete temperature_reports:n temperature_count:n temperature_sum_c:n temperature_latest_c:n temperature_latest_at state_reports:n state_count:n state_latest:n state_latest_at source_oldest_at source_newest_at"

var observationTable = table("observations", "interval_id:n asset_id:n site_id:n kind:n bucket revision:n captured_at capture_reason gap_reason no_source_data window_start window_end"+observationValueColumns)
var observationDayTable = table("observation_days", "interval_id:n asset_id:n site_id:n kind:n day hours:n gap_hours:n revised_hours:n unattributable_hours:n no_source_data_hours:n revision:n captured_at window_start window_end"+observationValueColumns)

func observationIdentity(interval, asset, site int64, kind int32) []string {
	return []string{strconv.FormatInt(interval, 10), strconv.FormatInt(asset, 10), strconv.FormatInt(site, 10), strconv.FormatInt(int64(kind), 10)}
}
func (v ObservationValues) cells() ([]string, error) {
	number := func(value *float64) (string, error) {
		if value == nil {
			return "", nil
		}
		if math.IsNaN(*value) || math.IsInf(*value, 0) {
			return "", fmt.Errorf("non-finite monitoring value")
		}
		return strconv.FormatFloat(*value, 'f', -1, 64), nil
	}
	flag := func(value *bool) string {
		if value == nil {
			return ""
		}
		return strconv.FormatBool(*value)
	}
	count := func(value int64) string { return strconv.FormatInt(value, 10) }
	numbers := make([]string, 0, 5)
	for _, value := range []*float64{v.HashrateSum, v.PowerSum, v.TemperatureSum, v.TemperatureLatest, v.StateLatest} {
		cell, err := number(value)
		if err != nil {
			return nil, err
		}
		numbers = append(numbers, cell)
	}
	return []string{count(v.HashrateReports), count(v.HashrateCount), count(v.HashratePositive), numbers[0], flag(v.HashrateComplete),
		count(v.PowerReports), count(v.PowerCount), numbers[1], flag(v.PowerComplete),
		count(v.TemperatureReports), count(v.TemperatureCount), numbers[2], numbers[3], exportInstant(v.TemperatureLatestAt),
		count(v.StateReports), count(v.StateCount), numbers[4], exportInstant(v.StateLatestAt),
		exportInstant(v.SourceOldestAt), exportInstant(v.SourceNewestAt)}, nil
}
func observationRow(v Observation) ([]string, error) {
	gap := v.CaptureReason == "gap"
	if !slices.Contains([]string{"closed", "unattributable", "gap"}, v.CaptureReason) || gap != (v.Revision == 0) || gap != (v.GapReason != "") || v.Revision < 0 || v.Bucket.IsZero() || v.CapturedAt.IsZero() {
		return nil, fmt.Errorf("invalid hourly monitoring evidence")
	}
	values, err := v.cells()
	if err != nil {
		return nil, err
	}
	row := append(observationIdentity(v.IntervalID, v.AssetID, v.SiteID, v.Kind), exportInstant(v.Bucket), strconv.FormatInt(int64(v.Revision), 10), exportInstant(v.CapturedAt), v.CaptureReason, csvText(v.GapReason), strconv.FormatBool(v.NoSourceData), exportInstant(v.WindowStart), exportInstant(v.WindowEnd))
	return append(row, values...), nil
}
func observationDayRow(v ObservationDay) ([]string, error) {
	if v.Hours < 0 || v.GapHours < 0 || v.Hours+v.GapHours < 1 || v.Hours+v.GapHours > 24 || v.Revision < 0 || v.Day.IsZero() || v.CapturedAt.IsZero() {
		return nil, fmt.Errorf("invalid daily monitoring evidence")
	}
	values, err := v.cells()
	if err != nil {
		return nil, err
	}
	count := func(value int32) string { return strconv.FormatInt(int64(value), 10) }
	row := append(observationIdentity(v.IntervalID, v.AssetID, v.SiteID, v.Kind), exportInstant(v.Day), count(v.Hours), count(v.GapHours), count(v.RevisedHours), count(v.UnattributableHours), count(v.NoSourceDataHours), count(v.Revision), exportInstant(v.CapturedAt), exportInstant(v.WindowStart), exportInstant(v.WindowEnd))
	return append(row, values...), nil
}

type manifestMember struct {
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
	SHA256    string `json:"sha256"`
	Bytes     int64  `json:"bytes"`
	Rows      int64  `json:"rows,omitempty"`
}
type evidenceManifest struct {
	SchemaVersion      string           `json:"schemaVersion"`
	InputVersion       string           `json:"inputVersion"`
	CalculationVersion string           `json:"calculationVersion"`
	Identity           Identity         `json:"identity"`
	Period             Period           `json:"period"`
	Status             Status           `json:"status"`
	Complete           bool             `json:"complete"`
	Sites              []int64          `json:"sites"`
	CoverageRows       int              `json:"coverageRows"`
	Sources            []string         `json:"sources"`
	Files              []manifestMember `json:"files"`
	Limitations        []string         `json:"limitations"`
}
type countWriter struct {
	w io.Writer
	n int64
}

func (w *countWriter) Write(p []byte) (int, error) {
	n, e := w.w.Write(p)
	w.n += int64(n)
	if e != nil {
		return n, fmt.Errorf("write evidence bytes: %w", e)
	}
	return n, nil
}

// WriteEvidence writes bounded entries directly to w. The source must yield
// frozen monitoring evidence once each, in strictly ascending (interval, time) order.
// Errors invalidate the partial output; callers must roll back its persistence.
// The manifest excludes its own digest and the enclosing ZIP digest. The
// publication transaction stores that outer digest alongside the frozen bytes.
func WriteEvidence(w io.Writer, result Result, inputs Inputs, identity Identity, observations ObservationSource) error {
	return WriteEvidenceContext(context.Background(), w, result, inputs, identity, observations)
}

// PreflightArtifacts checks the public input projection and bounded HTML before
// a draft is retained. Monitoring evidence stays streamed at publication.
func PreflightArtifacts(ctx context.Context, result Result, inputs Inputs, identity Identity) error {
	result = PublicResult(result)
	for _, ref := range result.Sources {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("preflight report artifacts: %w", err)
		}
		if err := checkReference(ref); err != nil {
			return err
		}
	}
	selected, err := SelectPublicEvidence(ctx, result.Period, inputs, ObservationSource{})
	if err != nil {
		return err
	}
	if _, err := publicEvidenceInputTables(ctx, result, selected); err != nil {
		return err
	}
	return WriteWrittenReport(ctx, io.Discard, result, identity)
}

// evidenceObservationLimitation is frozen into every published manifest.
const evidenceObservationLimitation = "Monitoring evidence is one row per allocated interval and UTC hour, at the highest revision captured by the report's evidence cutoff, which is no later than its as-of time; gap rows mark hours recorded as never captured. Reports over 500,000 interval-hours (covered allocation intervals times period hours) carry UTC-day rollups in " + evidenceObservationDaysFile + " instead of " + evidenceObservationsFile + "."

const (
	evidenceObservationsFile    = "observations.csv"
	evidenceObservationDaysFile = "observation_days.csv"
)

func WriteEvidenceContext(ctx context.Context, w io.Writer, result Result, inputs Inputs, identity Identity, observations ObservationSource) error {
	_, err := WriteSelectedEvidenceContext(ctx, w, result, inputs, identity, observations)
	return err
}

// WriteSelectedEvidenceContext writes the evidence archive and returns the
// public evidence it selected while streaming the observations once. Callers
// verify that selection rather than reading the observations a second time.
func WriteSelectedEvidenceContext(ctx context.Context, w io.Writer, result Result, inputs Inputs, identity Identity, observations ObservationSource) (Inputs, error) {
	var selected Inputs
	err := writeEvidenceContext(ctx, w, result, inputs, identity, observations, MaxEvidenceZIPBytes, MaxEvidenceContentBytes, &selected)
	return selected, err
}

func writeEvidenceContext(ctx context.Context, w io.Writer, result Result, inputs Inputs, identity Identity, observations ObservationSource, archiveBytes, contentBytes int64, selected *Inputs) error {
	if err := resultLimits(ctx, result); err != nil {
		return err
	}
	result = PublicResult(result)
	w = &outputWriter{checkCtx: ctx.Err, writer: w, remaining: archiveBytes, bounded: true}
	calculated, err := resultTables(ctx, result)
	if err != nil {
		return err
	}
	manifest := evidenceManifest{SchemaVersion: evidenceSchemaVersion, InputVersion: "hosted-report-inputs/v1", CalculationVersion: result.Version, Identity: identity, Period: publicPeriod(result.Period), Complete: result.Complete, Status: Provisional, Sources: uniqueStrings(result.Sources), Files: []manifestMember{}, Limitations: []string{
		"Container and order views are alternative breakdowns; each reconciles to its own report total.",
		"Private tickets, representatives, internal ownership, delivery configuration and raw infrastructure diagnostics are omitted. Control references use report-local labels; frozen monitoring interval identities remain available.",
		"Meter rows contain only attributed consumption; raw readings and allocation fractions remain in host-reviewed inputs. Intervals crossing report or commercial coverage boundaries are omitted without estimation. Annual comparison windows are labeled current and prior_year.",
		"Prior-only annual incident narratives and exclusion claims are omitted. Incident prose requires an observed interval within the report period and as-of boundary; stale-open gaps retain evidence without earlier prose. root_cause_recorded preserves the consumed predicate and narrative_redacted identifies the omission.",
		"Current incident and operational control explanations, deadline request references and frozen observations are independent required report evidence. Source qualifications identify actual calculator consultations and effective certification in the original reviewed configuration.",
		"Source references and narrative must be checked for client visibility before publication.",
		"CSV text cells with formula prefixes are apostrophe-prefixed; numeric columns retain their exact values.",
		"Manifest hashes cover uncompressed member bytes; the archive digest is stored by the issuing service.",
		evidenceObservationLimitation,
	}}
	if result.Complete {
		manifest.Status = Authoritative
	}
	manifest.Identity.IssuedAt = identity.IssuedAt.UTC()
	for _, ref := range manifest.Sources {
		if err := checkReference(ref); err != nil {
			return err
		}
	}
	archive := zip.NewWriter(w)
	write := func(name, media string, rows int64, body func(io.Writer) error) error {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0644)
		member, err := archive.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("create evidence member %s: %w", name, err)
		}
		digest := sha256.New()
		counter := countWriter{w: io.MultiWriter(member, digest)}
		if err := body(&outputWriter{checkCtx: ctx.Err, writer: &counter, remaining: contentBytes, bounded: true}); err != nil {
			return err
		}
		contentBytes -= counter.n
		manifest.Files = append(manifest.Files, manifestMember{Name: name, MediaType: media, SHA256: hex.EncodeToString(digest.Sum(nil)), Bytes: counter.n, Rows: rows})
		return nil
	}
	scopeSites := map[int64]bool{}
	count := int64(0)
	// Each row is checked and written as it arrives. The first failure is kept,
	// because a source may swallow the error its yield returned.
	var csvWriter *csv.Writer
	var failure error
	lastInterval, lastAt := int64(0), time.Time{}
	add := func(interval, site int64, at time.Time, row []string, err error) error {
		switch {
		case failure != nil:
		case ctx.Err() != nil:
			failure = fmt.Errorf("write report observations: %w", ctx.Err())
		case err != nil:
			failure = err
		case interval < lastInterval || (interval == lastInterval && !at.After(lastAt)):
			failure = fmt.Errorf("monitoring evidence must ascend by interval and time without repeats")
		default:
			if err := csvWriter.Write(row); err != nil {
				failure = fmt.Errorf("write observation CSV row: %w", err)
				break
			}
			lastInterval, lastAt = interval, at
			if site > 0 {
				scopeSites[site] = true
			}
			count++
		}
		return failure
	}
	daily := DailyObservations(result.Period, inputs.Coverage)
	observationFile, headers := evidenceObservationsFile, observationTable.headers()
	if daily {
		observationFile, headers = evidenceObservationDaysFile, observationDayTable.headers()
	}
	err = write(observationFile, "text/csv; charset=utf-8", 0, func(w io.Writer) error {
		csvWriter = csv.NewWriter(w)
		if e := csvWriter.Write(headers); e != nil {
			return fmt.Errorf("write observation CSV headers: %w", e)
		}
		// Select and stream the same frozen membership in one pass. Rows are
		// checked, selected and written as they arrive; none are buffered.
		source := ObservationSource{}
		if daily {
			if observations.Days != nil {
				source.Days = func(yield func(ObservationDay) error) error {
					return observations.Days(func(v ObservationDay) error {
						if err := yield(v); err != nil {
							return err
						}
						row, err := observationDayRow(v)
						return add(v.IntervalID, v.SiteID, v.Day, row, err)
					})
				}
			}
		} else if observations.Hours != nil {
			source.Hours = func(yield func(Observation) error) error {
				return observations.Hours(func(v Observation) error {
					if err := yield(v); err != nil {
						return err
					}
					row, err := observationRow(v)
					return add(v.IntervalID, v.SiteID, v.Bucket, row, err)
				})
			}
		}
		var selectErr error
		inputs, selectErr = SelectPublicEvidence(ctx, result.Period, inputs, source)
		if failure != nil {
			return failure
		}
		if selectErr != nil {
			return selectErr
		}
		csvWriter.Flush()
		if err := csvWriter.Error(); err != nil {
			return fmt.Errorf("flush observation CSV: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	manifest.Files[len(manifest.Files)-1].Rows = count
	manifest.CoverageRows = len(inputs.Coverage)
	for _, site := range inputs.Sites() {
		scopeSites[site] = true
	}
	input, err := publicEvidenceInputTables(ctx, result, inputs)
	if err != nil {
		return err
	}
	for _, t := range append(calculated, input...) {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("write report evidence: %w", err)
		}
		if err := write(t.name+".csv", "text/csv; charset=utf-8", int64(len(t.rows)), t.writeCSV); err != nil {
			return err
		}
	}
	manifest.Sites = make([]int64, 0, len(scopeSites))
	for site := range scopeSites {
		manifest.Sites = append(manifest.Sites, site)
	}
	slices.Sort(manifest.Sites)
	header := &zip.FileHeader{Name: "manifest.json", Method: zip.Deflate}
	header.SetMode(0644)
	member, err := archive.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create evidence manifest: %w", err)
	}
	encoder := json.NewEncoder(&outputWriter{checkCtx: ctx.Err, writer: member, remaining: contentBytes, bounded: true})
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(manifest); err != nil {
		return fmt.Errorf("encode evidence manifest: %w", err)
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("close evidence archive: %w", err)
	}
	if selected != nil {
		*selected = inputs
	}
	return nil
}

type writtenMetric struct{ Label, Value, Unit, Status, Causes, Method, Sources string }
type writtenBreakdown struct {
	Scope   string
	Metrics []writtenMetric
}
type writtenField struct{ Label, Value string }
type writtenTable struct {
	Title string
	Rows  [][]writtenField
}
type writtenReport struct {
	CalculationVersion string
	Identity           Identity
	Period             Period
	CSP, State         string
	Metrics            []writtenMetric
	Containers, Orders []writtenBreakdown
	Tables             []writtenTable
}

func humanLabel(key string) string {
	replacements := map[string]string{"metered_energy_kwh": "Metered energy (kWh)", "hashrate_performance_hs": "Hashrate performance (H/s)", "hashrate_performance_percent": "Hashrate performance (%)", "open_S3_items": "Open S3 items", "missing_G1": "Missing G1", "missing_G2": "Missing G2", "missing_G3": "Missing G3", "missing_G4": "Missing G4", "missing_G5": "Missing G5"}
	if value, ok := replacements[key]; ok {
		return value
	}
	s := strings.ReplaceAll(key, "_", " ")
	if s == "" {
		return ""
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
func writtenMetrics(ctx context.Context, metrics map[string]Metric) ([]writtenMetric, error) {
	keys := make([]string, 0, len(metrics))
	for key := range metrics {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prepare report output: %w", err)
		}
		keys = append(keys, key)
	}
	slices.Sort(keys)
	out := []writtenMetric{}
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prepare report output: %w", err)
		}
		v := metrics[key]
		for _, ref := range v.Sources {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("prepare report output: %w", err)
			}
			if err := checkReference(ref); err != nil {
				return nil, err
			}
		}
		value := v.Value
		if v.Status == Unavailable {
			value = "Unavailable"
		} else if len(v.Amounts) > 0 {
			parts := []string{}
			amounts := slices.Clone(v.Amounts)
			sort.Slice(amounts, func(i, j int) bool { return amounts[i].Currency < amounts[j].Currency })
			for _, a := range amounts {
				if err := ctx.Err(); err != nil {
					return nil, fmt.Errorf("prepare report output: %w", err)
				}
				parts = append(parts, a.Value+" "+a.Currency)
			}
			value = strings.Join(parts, "; ")
		}
		causes := []string{}
		for _, cause := range uniqueStrings(v.Causes) {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("prepare report output: %w", err)
			}
			causes = append(causes, humanLabel(cause))
		}
		out = append(out, writtenMetric{humanLabel(key), value, v.Unit, humanLabel(string(v.Status)), strings.Join(causes, "; "), humanLabel(v.Method), strings.Join(uniqueStrings(v.Sources), "; ")})
	}
	return out, nil
}

// BuildWrittenReport renders a printable, script-free document. All imported
// text passes through html/template contextual escaping; there are no active
// resources, inline handlers, or trusted-HTML conversions.
func BuildWrittenReport(result Result, identity Identity) ([]byte, error) {
	return BuildWrittenReportContext(context.Background(), result, identity)
}
func BuildWrittenReportContext(ctx context.Context, result Result, identity Identity) ([]byte, error) {
	var out bytes.Buffer
	if err := WriteWrittenReport(ctx, &out, result, identity); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// WriteWrittenReport renders to bounded output without holding the full HTML in memory.
func WriteWrittenReport(ctx context.Context, w io.Writer, result Result, identity Identity) error {
	result = PublicResult(result)
	if err := resultLimits(ctx, result); err != nil {
		return err
	}
	if err := checkReference(result.Period.RequestRef); err != nil {
		return err
	}
	metrics, err := writtenMetrics(ctx, result.Metrics)
	if err != nil {
		return err
	}
	data := writtenReport{CalculationVersion: result.Version, Identity: identity, Period: publicPeriod(result.Period), CSP: reportCSP(), State: "Incomplete / provisional", Metrics: metrics}
	if result.Complete {
		data.State = "Complete"
	}
	containers := slices.Clone(result.Containers)
	sort.Slice(containers, func(i, j int) bool {
		if containers[i].SiteID == containers[j].SiteID {
			return containers[i].ContainerID < containers[j].ContainerID
		}
		return containers[i].SiteID < containers[j].SiteID
	})
	orders := slices.Clone(result.Orders)
	sort.Slice(orders, func(i, j int) bool { return orders[i].OrderID < orders[j].OrderID })
	for _, v := range containers {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("render written report: %w", err)
		}
		m, e := writtenMetrics(ctx, v.Metrics)
		if e != nil {
			return e
		}
		data.Containers = append(data.Containers, writtenBreakdown{fmt.Sprintf("Site %d / container %d", v.SiteID, v.ContainerID), m})
	}
	for _, v := range orders {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("render written report: %w", err)
		}
		m, e := writtenMetrics(ctx, v.Metrics)
		if e != nil {
			return e
		}
		data.Orders = append(data.Orders, writtenBreakdown{"Order " + v.OrderID, m})
	}
	tables, err := resultTables(ctx, result)
	if err != nil {
		return err
	}
	for _, t := range tables[1:] {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("render written report: %w", err)
		}
		wt := writtenTable{Title: humanLabel(t.name), Rows: [][]writtenField{}}
		for _, row := range t.rows {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("render written report: %w", err)
			}
			cells := []writtenField{}
			for i, v := range row {
				if err := ctx.Err(); err != nil {
					return fmt.Errorf("render written report: %w", err)
				}
				label := humanLabel(t.columns[i].name)
				switch t.columns[i].name {
				case "true_episode_start":
					label = "True episode start"
				case "period_clipped_start":
					label = "Period-clipped start"
				case "period_clipped_end":
					label = "Period-clipped end"
				}
				value := fmt.Sprint(v)
				switch t.columns[i].name {
				case "metric", "causes", "status", "exclusion_status", "category", "basis":
					value = humanLabel(value)
				}
				cells = append(cells, writtenField{label, value})
			}
			wt.Rows = append(wt.Rows, cells)
		}
		data.Tables = append(data.Tables, wt)
	}

	out := &outputWriter{checkCtx: ctx.Err, writer: w, remaining: MaxWrittenReportBytes, bounded: true}
	if err := reportTemplate.Execute(out, data); err != nil {
		return fmt.Errorf("render written report: %w", err)
	}
	return nil
}

const reportCSS = `body{font:15px/1.45 system-ui,sans-serif;max-width:1100px;margin:2rem auto;padding:0 1rem;color:#172026}table{border-collapse:collapse;width:100%;margin:1rem 0}th,td{border:1px solid #bbb;padding:.4rem;text-align:left;vertical-align:top;overflow-wrap:anywhere}h2{margin-top:2rem}dl{display:grid;grid-template-columns:minmax(9rem,1fr) 3fr;gap:.3rem 1rem}dt{font-weight:600}dd{margin:0;overflow-wrap:anywhere}article{border-top:1px solid #bbb}footer{margin-top:2rem;font-size:.9em}@media print{@page{margin:15mm}body{font-size:9pt;max-width:none;margin:0;padding:0}tr,article{break-inside:avoid}thead{display:table-header-group}}`

func reportCSP() string {
	digest := sha256.Sum256([]byte(reportCSS))
	return "default-src 'none'; base-uri 'none'; form-action 'none'; style-src 'sha256-" + base64.StdEncoding.EncodeToString(digest[:]) + "'"
}

var reportTemplate = template.Must(template.New("written-report").Funcs(template.FuncMap{"instant": exportInstant}).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="{{.CSP}}"><meta name="referrer" content="no-referrer"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Identity.AccountName}} — hosted performance report</title><style>` + reportCSS + `</style></head><body>
<header><h1>{{.Identity.AccountName}} — hosted performance report</h1><p>Report {{.Identity.ReportID}} · Version {{.Identity.Version}} · Account {{.Identity.AccountID}}</p><p>{{.Period.Kind}} · {{instant .Period.Start}} (inclusive) to {{instant .Period.End}} (exclusive) · Timezone {{.Period.Timezone}}</p><p>As of {{instant .Period.AsOf}} · Issued {{instant .Identity.IssuedAt}} · {{.State}}</p><p>Use your browser’s Print command to print or save this document.</p></header>
{{if .Identity.CorrectionOf}}<section><h2>Correction</h2><p>Corrects {{.Identity.CorrectionOf}}. {{.Identity.CorrectionReason}}</p></section>{{end}}
{{if .Identity.Exception}}<section><h2>Incomplete issue exception</h2><p>{{.Identity.Exception}}</p></section>{{end}}
<section><h2>Report totals</h2>{{template "metrics" .Metrics}}</section>
<section><h2>Container breakdown</h2><p>This is an alternative view of the same report total. Do not add it to the order breakdown.</p>{{range .Containers}}<h3>{{.Scope}} — own totals</h3>{{template "metrics" .Metrics}}{{else}}<p>No attributed container breakdown is available.</p>{{end}}</section>
<section><h2>Order breakdown</h2><p>Each order contributes to this view’s own total. Container and order views must reconcile independently.</p>{{range .Orders}}<h3>{{.Scope}} — own totals</h3>{{template "metrics" .Metrics}}{{else}}<p>No attributed order breakdown is available.</p>{{end}}</section>
{{range .Tables}}<section><h2>{{.Title}}</h2>{{range .Rows}}<article><dl>{{range .}}<dt>{{.Label}}</dt><dd>{{.Value}}</dd>{{end}}</dl></article>{{else}}<p>No supporting rows are available.</p>{{end}}</section>{{end}}
<footer><p>Hashrate performance is distinct from contractual uptime and connectivity. Accrued fees do not establish invoicing or payment. Unavailable fields have no numeric value.</p><p>Calculation version: {{.CalculationVersion}} · Input schema: hosted-report-inputs/v1</p><p>Original issued artifacts remain immutable; corrections are separately linked versions.</p></footer></body></html>
{{define "metrics"}}<table border="1"><thead><tr><th scope="col">Metric</th><th scope="col">Value</th><th scope="col">Unit</th><th scope="col">Status</th><th scope="col">Causes / limitations</th><th scope="col">Method</th><th scope="col">Evidence references</th></tr></thead><tbody>{{range .}}<tr><th scope="row">{{.Label}}</th><td>{{.Value}}</td><td>{{.Unit}}</td><td>{{.Status}}</td><td>{{.Causes}}</td><td>{{.Method}}</td><td>{{.Sources}}</td></tr>{{end}}</tbody></table>{{end}}`))
