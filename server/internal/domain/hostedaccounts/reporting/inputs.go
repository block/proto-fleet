// Package reporting calculates hosted reports from explicit, reviewed facts.
// It has no persistence, network or wall-clock dependency.
package reporting

import (
	"cmp"
	"context"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strings"
	"time"
)

const meterQualityEstimated = "estimated"

const reportKindQuarterly = "quarterly"

// Asset kinds match the mining contract without coupling pure calculations to
// protobuf types. Module history remains distinct from stable board ownership.
const (
	AssetKindMiner     int32 = 5
	AssetKindModule    int32 = 6
	AssetKindHashboard int32 = 7
)

type Period struct {
	RequestReceivedAt time.Time `json:"requestReceivedAt,omitempty"`
	RequestRef        string    `json:"requestRef,omitempty"`
	Kind              string    `json:"kind"`
	Start             time.Time `json:"start"`
	End               time.Time `json:"end"`
	AsOf              time.Time `json:"asOf"`
	Timezone          string    `json:"timezone"`
	YearStart         time.Time `json:"yearStart,omitempty"`
}

type Prerequisite struct {
	Key       string `json:"key"`
	Owner     string `json:"owner"`
	Ready     bool   `json:"ready"`
	SourceRef string `json:"sourceRef"`
}
type Source struct {
	ID        string `json:"id"`
	Metric    string `json:"metric"`
	Certified bool   `json:"certified"`
	SourceRef string `json:"sourceRef"`
}
type RegisterCompleteness struct {
	Kind      string    `json:"kind"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	SourceRef string    `json:"sourceRef"`
}
type Configuration struct {
	Registers           []RegisterCompleteness `json:"registers"`
	ContractRef         string                 `json:"contractRef"`
	Timezone            string                 `json:"timezone"`
	ReportingYearMonth  int                    `json:"reportingYearMonth"`
	ReportingYearDay    int                    `json:"reportingYearDay"`
	Holidays            []string               `json:"holidays"`
	Prerequisites       []Prerequisite         `json:"prerequisites"`
	Sources             []Source               `json:"sources"`
	DeliveryChannel     string                 `json:"deliveryChannel"`
	SharedAllocationRef string                 `json:"sharedAllocationRef"`
	Representatives     []Representative       `json:"representatives"`
}
type Representative struct {
	UserID    string `json:"userId"`
	CanSubmit bool   `json:"canSubmit"`
	SourceRef string `json:"sourceRef"`
}

func (c Configuration) Ready(key string) bool {
	for _, g := range c.Prerequisites {
		if g.Key == key {
			return g.Ready && strings.TrimSpace(g.Owner) != "" && strings.TrimSpace(g.SourceRef) != ""
		}
	}
	return false
}
func (c Configuration) Certified(sourceID, metric string) bool {
	for _, s := range c.Sources {
		if s.ID == sourceID && s.Metric == metric && s.Certified && strings.TrimSpace(s.SourceRef) != "" {
			return c.Ready("G2")
		}
	}
	return false
}

type Order struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end,omitempty"`
	SourceRef string    `json:"sourceRef"`
}
type CoverageSegment struct {
	ID                   string    `json:"id"`
	AllocationIntervalID int64     `json:"allocationIntervalId"`
	Kind                 int32     `json:"kind"`
	AssetID              int64     `json:"assetId"`
	SiteID               int64     `json:"siteId"`
	ContainerID          int64     `json:"containerId"` // Host device: standalone miner or module/board controller.
	OrderID              string    `json:"orderId"`
	Start                time.Time `json:"start"`
	End                  time.Time `json:"end,omitempty"`
	CommissionedAt       time.Time `json:"commissionedAt,omitempty"`
	CapacityTHS          string    `json:"capacityThs"`
	ReplacementOf        string    `json:"replacementOf,omitempty"`
	SourceRef            string    `json:"sourceRef"`
}

// ModuleCoverageSegment associates reviewed module repair facts with owned boards.
// Partial associations support ticket history; WholeModule additionally supports
// module counts. It never adds performance capacity or establishes an outage.
type ModuleCoverageSegment struct {
	ID               string    `json:"id"`
	ModuleID         int64     `json:"moduleId"`
	SiteID           int64     `json:"siteId"`
	ContainerID      int64     `json:"containerId"`
	OrderID          string    `json:"orderId"`
	Start            time.Time `json:"start"`
	End              time.Time `json:"end"`
	BoardCoverageIDs []string  `json:"boardCoverageIds"`
	WholeModule      bool      `json:"wholeModule"`
	SourceRef        string    `json:"sourceRef"`
}

type MeterInterval struct {
	ID                 string    `json:"id"`
	MeterID            string    `json:"meterId"`
	SourceID           string    `json:"sourceId"`
	SiteID             int64     `json:"siteId"`
	ContainerID        int64     `json:"containerId"`
	OrderID            string    `json:"orderId"`
	Start              time.Time `json:"start"`
	End                time.Time `json:"end"`
	KWh                string    `json:"kwh,omitempty"`
	Quality            string    `json:"quality"`
	SourceRef          string    `json:"sourceRef"`
	AllocationFraction string    `json:"allocationFraction,omitempty"`
}
type FeeLine struct {
	ID          string    `json:"id"`
	SiteID      int64     `json:"siteId"`
	ContainerID int64     `json:"containerId"`
	OrderID     string    `json:"orderId"`
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	Amount      string    `json:"amount"`
	Currency    string    `json:"currency"`
	SourceRef   string    `json:"sourceRef"`
}
type Incident struct {
	ID              string    `json:"id"`
	SiteID          int64     `json:"siteId"`
	ContainerID     int64     `json:"containerId"`
	OrderID         string    `json:"orderId"`
	Start           time.Time `json:"start"`
	End             time.Time `json:"end,omitempty"`
	AsOf            time.Time `json:"asOf"`
	Category        string    `json:"category"`
	Summary         string    `json:"summary"`
	RootCause       string    `json:"rootCause"`
	ExclusionClause string    `json:"exclusionClause,omitempty"`
	SourceRef       string    `json:"sourceRef"`
}
type HashrateInterval struct {
	ID          string    `json:"id"`
	AssetID     int64     `json:"assetId"`
	Kind        int32     `json:"kind"`
	SiteID      int64     `json:"siteId"`
	ContainerID int64     `json:"containerId"`
	OrderID     string    `json:"orderId"`
	SourceID    string    `json:"sourceId"`
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	HashrateHS  string    `json:"hashrateHs"`
	Complete    bool      `json:"complete"`
	SourceRef   string    `json:"sourceRef"`
}
type PoolObservation struct {
	ID         string    `json:"id"`
	AssetID    int64     `json:"assetId"`
	Kind       int32     `json:"kind"`
	SourceID   string    `json:"sourceId"`
	SourceAt   time.Time `json:"sourceAt"`
	ReceivedAt time.Time `json:"receivedAt"`
	State      string    `json:"state"`
	Reason     string    `json:"reason"`
	SourceRef  string    `json:"sourceRef"`
}
type ObligationInterval struct {
	ID          string    `json:"id"`
	EpisodeID   string    `json:"episodeId"`
	AssetID     int64     `json:"assetId"`
	Kind        int32     `json:"kind"`
	SiteID      int64     `json:"siteId"`
	ContainerID int64     `json:"containerId"`
	OrderID     string    `json:"orderId"`
	Start       time.Time `json:"start"`
	End         time.Time `json:"end,omitempty"`
	AsOf        time.Time `json:"asOf"`
	Severity    string    `json:"severity"`
	TicketRef   string    `json:"ticketRef"`
	TicketAt    time.Time `json:"ticketAt,omitempty"`
	SubmittedAt time.Time `json:"submittedAt,omitempty"`
	ReceivedAt  time.Time `json:"receivedAt"`
	DiagnosedAt time.Time `json:"diagnosedAt,omitempty"`
	RespondedAt time.Time `json:"respondedAt,omitempty"`
	ApprovedAt  time.Time `json:"approvedAt,omitempty"`
	Deadline    time.Time `json:"deadline,omitempty"`
	RestoredAt  time.Time `json:"restoredAt,omitempty"`
	ClosedAt    time.Time `json:"closedAt,omitempty"`
	Qualified   bool      `json:"qualified"`
	SourceRef   string    `json:"sourceRef"`
}
type ContractResult struct {
	ID             string    `json:"id"`
	Metric         string    `json:"metric"`
	SiteID         int64     `json:"siteId"`
	ContainerID    int64     `json:"containerId,omitempty"`
	OrderID        string    `json:"orderId,omitempty"`
	Start          time.Time `json:"start"`
	End            time.Time `json:"end"`
	Value          string    `json:"value"`
	Unit           string    `json:"unit"`
	SourceRef      string    `json:"sourceRef"`
	CalculationRef string    `json:"calculationRef"`
}
type Inputs struct {
	ModuleCoverage   []ModuleCoverageSegment `json:"moduleCoverage,omitempty"`
	Configuration    Configuration           `json:"configuration"`
	Orders           []Order                 `json:"orders"`
	Coverage         []CoverageSegment       `json:"coverage"`
	Meters           []MeterInterval         `json:"meters"`
	Fees             []FeeLine               `json:"fees"`
	Incidents        []Incident              `json:"incidents"`
	Hashrates        []HashrateInterval      `json:"hashrates"`
	PoolObservations []PoolObservation       `json:"poolObservations"`
	Obligations      []ObligationInterval    `json:"obligations"`
	ExternalResults  []ContractResult        `json:"externalResults"`
}

func reportingAssetKind(kind int32) bool {
	return kind == AssetKindMiner || kind == AssetKindModule || kind == AssetKindHashboard
}

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

var decimalPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)

// Decimal accepts finite base-ten input only; rational/exponent syntax is not an import format.
func Decimal(text string, signed bool) (*big.Rat, error) {
	if len(text) > 80 || !decimalPattern.MatchString(text) {
		return nil, fmt.Errorf("invalid decimal")
	}
	r, ok := new(big.Rat).SetString(text)
	if !ok || (!signed && r.Sign() < 0) {
		return nil, fmt.Errorf("invalid decimal")
	}
	return r, nil
}
func interval(start, end time.Time, open bool) bool {
	return !start.IsZero() && ((open && end.IsZero()) || end.After(start))
}
func overlaps(a, b, c, d time.Time) bool {
	return (b.IsZero() || c.Before(b)) && (d.IsZero() || a.Before(d))
}
func text(value string) bool { return strings.TrimSpace(value) != "" && len(value) <= 2000 }
func (p Period) Validate() error {
	loc, err := time.LoadLocation(p.Timezone)
	if err != nil || p.Timezone == "" || p.Timezone == "Local" {
		return fmt.Errorf("invalid reporting timezone")
	}
	if !interval(p.Start, p.End, false) || p.AsOf.IsZero() || p.AsOf.Before(p.End) || p.End.Sub(p.Start) > 367*24*time.Hour {
		return fmt.Errorf("invalid reporting period")
	}
	start, end := p.Start.In(loc), p.End.In(loc)
	midnight := func(t time.Time) bool {
		return t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0
	}
	if p.Kind != "ad_hoc" && (!p.RequestReceivedAt.IsZero() || p.RequestRef != "") {
		return fmt.Errorf("request receipt is only used for ad hoc reports")
	}
	if !p.RequestReceivedAt.IsZero() && (p.RequestReceivedAt.After(p.AsOf) || !text(p.RequestRef)) {
		return fmt.Errorf("ad hoc request receipt needs supporting evidence")
	}
	switch p.Kind {
	case "monthly":
		if start.Day() != 1 || !midnight(start) || !end.Equal(start.AddDate(0, 1, 0)) {
			return fmt.Errorf("monthly period must cover a local calendar month")
		}
	case reportKindQuarterly:
		if start.Day() != 1 || !midnight(start) || !end.Equal(start.AddDate(0, 3, 0)) {
			return fmt.Errorf("quarterly period must cover three local calendar months")
		}
	case "annual":
		if !midnight(start) || !end.Equal(start.AddDate(1, 0, 0)) {
			return fmt.Errorf("annual period must cover a reporting year")
		}
	case "ad_hoc":
	default:
		return fmt.Errorf("invalid report kind")
	}
	return nil
}
func (in Inputs) Validate() error {
	return in.ValidateContext(context.Background())
}

// ValidateContext bounds import validation by the caller's cancellation.
func (in Inputs) ValidateContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("validate reporting inputs: %w", err)
	}
	c := in.Configuration
	if c.Timezone == "" {
		return fmt.Errorf("reporting timezone is required")
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil || c.Timezone == "Local" {
		return fmt.Errorf("invalid reporting timezone")
	}
	if c.ReportingYearMonth < 1 || c.ReportingYearMonth > 12 || c.ReportingYearDay < 1 || c.ReportingYearDay > 31 {
		return fmt.Errorf("reporting year start must be a valid annual date")
	}
	if time.Date(2000, time.Month(c.ReportingYearMonth), c.ReportingYearDay, 0, 0, 0, 0, time.UTC).Month() != time.Month(c.ReportingYearMonth) {
		return fmt.Errorf("invalid reporting year start date")
	}
	if c.ReportingYearMonth == 2 && c.ReportingYearDay == 29 {
		return fmt.Errorf("February 29 reporting year requires an approved non-leap-year boundary rule")
	}
	if len(c.Registers) > 1000 {
		return fmt.Errorf("too many register completeness intervals")
	}
	for _, r := range c.Registers {
		if !slices.Contains([]string{"incidents", "obligations", "fees", "pool", "hashrate", "coverage"}, r.Kind) || !interval(r.Start, r.End, false) || !text(r.SourceRef) {
			return fmt.Errorf("invalid register completeness evidence")
		}
	}
	if len(c.Prerequisites) > 5 || len(c.Sources) > 100 || len(c.Holidays) > 2000 || len(c.Representatives) > 200 {
		return fmt.Errorf("reporting configuration is too large")
	}
	seen := map[string]bool{}
	unique := func(kind, id string) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("validate reporting inputs: %w", err)
		}
		key := kind + ":" + id
		if !text(id) || seen[key] {
			return fmt.Errorf("invalid or duplicate %s identity", kind)
		}
		seen[key] = true
		return nil
	}
	for _, g := range c.Prerequisites {
		if !slices.Contains([]string{"G1", "G2", "G3", "G4", "G5"}, g.Key) {
			return fmt.Errorf("unknown prerequisite")
		}
		if err := unique("prerequisite", g.Key); err != nil {
			return err
		}
		if g.Ready && (!text(g.Owner) || !text(g.SourceRef)) {
			return fmt.Errorf("approved prerequisite requires owner and evidence")
		}
	}
	for _, s := range c.Sources {
		if err := unique("source", s.ID); err != nil {
			return err
		}
		if !slices.Contains([]string{"meter", "pool", "hashrate", "host"}, s.Metric) || (s.Certified && !text(s.SourceRef)) {
			return fmt.Errorf("invalid source certification")
		}
	}
	for _, h := range c.Holidays {
		if _, err := time.Parse("2006-01-02", h); err != nil {
			return fmt.Errorf("invalid holiday")
		}
	}
	for _, r := range c.Representatives {
		if err := unique("representative", r.UserID); err != nil {
			return err
		}
		if !text(r.SourceRef) {
			return fmt.Errorf("representative evidence required")
		}
	}
	orders := map[string]Order{}
	for _, o := range in.Orders {
		if err := ValidateFactFields(o); err != nil {
			return err
		}
		if err := unique("order", o.ID); err != nil {
			return err
		}
		orders[o.ID] = o
	}
	attributed := func(site, container int64, order string, start, end time.Time) bool {
		o, ok := orders[order]
		if !ok || site <= 0 || container <= 0 || !interval(start, end, true) || start.Before(o.Start) {
			return false
		}
		return o.End.IsZero() || (!end.IsZero() && !end.After(o.End))
	}
	for _, s := range in.Coverage {
		if err := ValidateFactFields(s); err != nil {
			return err
		}
		if err := unique("coverage", s.ID); err != nil {
			return err
		}
		if !attributed(s.SiteID, s.ContainerID, s.OrderID, s.Start, s.End) {
			return fmt.Errorf("invalid commercial coverage")
		}
	}
	coverage := slices.Clone(in.Coverage)
	slices.SortFunc(coverage, func(a, b CoverageSegment) int {
		if a.Kind != b.Kind {
			return cmp.Compare(a.Kind, b.Kind)
		}
		if a.AssetID != b.AssetID {
			return cmp.Compare(a.AssetID, b.AssetID)
		}
		return a.Start.Compare(b.Start)
	})
	for i := 1; i < len(coverage); i++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("validate reporting inputs: %w", err)
		}
		old, current := coverage[i-1], coverage[i]
		if old.Kind == current.Kind && old.AssetID == current.AssetID && overlaps(old.Start, old.End, current.Start, current.End) {
			return fmt.Errorf("overlapping coverage")
		}
	}
	legacyModules := map[int64][]CoverageSegment{}
	for _, c := range coverage {
		if c.Kind == AssetKindModule {
			legacyModules[c.AssetID] = append(legacyModules[c.AssetID], c)
		}
	}
	coverageByID := make(map[string]CoverageSegment, len(in.Coverage))
	for _, c := range in.Coverage {
		coverageByID[c.ID] = c
	}
	modules := slices.Clone(in.ModuleCoverage)
	slices.SortFunc(modules, func(a, b ModuleCoverageSegment) int {
		if a.ModuleID != b.ModuleID {
			return cmp.Compare(a.ModuleID, b.ModuleID)
		}
		return a.Start.Compare(b.Start)
	})
	for i, m := range modules {
		if err := ValidateFactFields(m); err != nil {
			return err
		}
		if err := unique("module coverage", m.ID); err != nil {
			return err
		}
		if !attributed(m.SiteID, m.ContainerID, m.OrderID, m.Start, m.End) {
			return fmt.Errorf("invalid reviewed module coverage")
		}
		if i > 0 && modules[i-1].ModuleID == m.ModuleID && overlaps(modules[i-1].Start, modules[i-1].End, m.Start, m.End) {
			return fmt.Errorf("overlapping module coverage")
		}
		boards := map[int64]bool{}
		for _, id := range m.BoardCoverageIDs {
			c, ok := coverageByID[id]
			if !ok || boards[c.AssetID] || !c.SupportsModule(m) {
				return fmt.Errorf("module coverage exceeds its distinct attributed board coverage")
			}
			boards[c.AssetID] = true
		}
		legacy := legacyModules[m.ModuleID]
		next, _ := slices.BinarySearchFunc(legacy, m.End, func(c CoverageSegment, end time.Time) int { return c.Start.Compare(end) })
		if next > 0 && overlaps(legacy[next-1].Start, legacy[next-1].End, m.Start, m.End) {
			return fmt.Errorf("module coverage overlaps legacy module coverage")
		}
	}
	type meterShare struct {
		MeterInterval
		fraction *big.Rat
	}
	meters := map[string][]meterShare{}
	for _, m := range in.Meters {
		if err := ValidateFactFields(m); err != nil {
			return err
		}
		if err := unique("meter", m.ID); err != nil {
			return err
		}
		if !attributed(m.SiteID, m.ContainerID, m.OrderID, m.Start, m.End) {
			return fmt.Errorf("invalid meter interval")
		}
		fraction := big.NewRat(1, 1)
		if m.AllocationFraction != "" {
			var err error
			fraction, err = Decimal(m.AllocationFraction, false)
			if err != nil || !c.SharedAllocationApproved() {
				return fmt.Errorf("shared meter requires approved allocation evidence")
			}
		}
		meters[m.MeterID] = append(meters[m.MeterID], meterShare{m, fraction})
	}
	for _, readings := range meters {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("validate reporting inputs: %w", err)
		}
		slices.SortFunc(readings, func(a, b meterShare) int { return a.Start.Compare(b.Start) })
		var previous MeterInterval
		var total *big.Rat
		orders := map[string]bool{}
		for _, reading := range readings {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("validate reporting inputs: %w", err)
			}
			m := reading.MeterInterval
			if total == nil || !overlaps(previous.Start, previous.End, m.Start, m.End) {
				previous = m
				total = new(big.Rat).Set(reading.fraction)
				orders = map[string]bool{}
			} else {
				if orders[m.OrderID] || !previous.SharesEvidenceWith(m) {
					return fmt.Errorf("overlapping meter intervals require matching evidence and distinct reviewed allocation shares")
				}
				total.Add(total, reading.fraction)
			}
			orders[m.OrderID] = true
			if total.Cmp(big.NewRat(1, 1)) > 0 {
				return fmt.Errorf("shared meter allocation exceeds one")
			}
		}
	}
	for _, f := range in.Fees {
		if err := ValidateFactFields(f); err != nil {
			return err
		}
		if err := unique("fee", f.ID); err != nil {
			return err
		}
		if !attributed(f.SiteID, f.ContainerID, f.OrderID, f.Start, f.End) {
			return fmt.Errorf("invalid accrued fee attribution")
		}
	}
	for _, i := range in.Incidents {
		if err := ValidateFactFields(i); err != nil {
			return err
		}
		if err := unique("incident", i.ID); err != nil {
			return err
		}
		if !attributed(i.SiteID, i.ContainerID, i.OrderID, i.Start, i.End) {
			return fmt.Errorf("invalid incident")
		}
	}
	for _, h := range in.Hashrates {
		if err := ValidateFactFields(h); err != nil {
			return err
		}
		if err := unique("hashrate", h.ID); err != nil {
			return err
		}
		if !attributed(h.SiteID, h.ContainerID, h.OrderID, h.Start, h.End) {
			return fmt.Errorf("invalid hashrate interval")
		}
	}
	for _, o := range in.PoolObservations {
		if err := ValidateFactFields(o); err != nil {
			return err
		}
		if err := unique("pool", o.ID); err != nil {
			return err
		}

	}
	for _, o := range in.Obligations {
		if err := ValidateFactFields(o); err != nil {
			return err
		}
		if err := unique("obligation", o.ID); err != nil {
			return err
		}
		if !attributed(o.SiteID, o.ContainerID, o.OrderID, o.Start, o.End) {
			return fmt.Errorf("invalid reviewed obligation")
		}
	}
	for _, r := range in.ExternalResults {
		if err := ValidateFactFields(r); err != nil {
			return err
		}
		if err := unique("result", r.ID); err != nil {
			return err
		}

		if r.OrderID != "" {
			if _, ok := orders[r.OrderID]; !ok {
				return fmt.Errorf("external result order unavailable")
			}
		}
	}
	return nil
}

// Sites is the complete artifact scope; immutable reports are read all-or-deny.
func (in Inputs) Sites() []int64 {
	set := map[int64]bool{}
	for _, s := range in.ModuleCoverage {
		set[s.SiteID] = true
	}
	for _, s := range in.Coverage {
		set[s.SiteID] = true
	}
	for _, s := range in.Meters {
		set[s.SiteID] = true
	}
	for _, s := range in.Fees {
		set[s.SiteID] = true
	}
	for _, s := range in.Incidents {
		set[s.SiteID] = true
	}
	for _, s := range in.Hashrates {
		set[s.SiteID] = true
	}
	for _, s := range in.Obligations {
		set[s.SiteID] = true
	}
	for _, s := range in.ExternalResults {
		set[s.SiteID] = true
	}
	out := make([]int64, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}
