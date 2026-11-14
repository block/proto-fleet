package reporting

import "time"

type Status string

const (
	Authoritative Status = "authoritative"
	Provisional   Status = "provisional"
	Unavailable   Status = "unavailable"
)

// Metric preserves the exact rational as well as its decimal presentation. A
// nonterminating decimal is presented to 18 places; Exact remains lossless.
// Unavailable metrics carry no numeric value, including currency amounts.
type Metric struct {
	Status  Status   `json:"status"`
	Value   string   `json:"value,omitempty"`
	Exact   string   `json:"exact,omitempty"`
	Unit    string   `json:"unit"`
	Amounts []Amount `json:"amounts,omitempty"`
	Causes  []string `json:"causes,omitempty"`
	Sources []string `json:"sources,omitempty"`
	Method  string   `json:"method,omitempty"`
}
type Amount struct {
	Currency string `json:"currency"`
	Value    string `json:"value"`
	Exact    string `json:"exact"`
}
type Breakdown struct {
	SiteID      int64             `json:"siteId,omitempty"`
	ContainerID int64             `json:"containerId,omitempty"`
	OrderID     string            `json:"orderId,omitempty"`
	Metrics     map[string]Metric `json:"metrics"`
}

// Reconciliation compares an additive metric against either alternative view.
// Container and order views describe the same estate and must not be added.
type Reconciliation struct {
	Metric     string `json:"metric"`
	View       string `json:"view"`
	ExactTotal string `json:"exactTotal"`
	ExactParts string `json:"exactParts"`
	Matches    bool   `json:"matches"`
	Status     Status `json:"status"`
}
type Interruption struct {
	ID               string    `json:"id"`
	SiteID           int64     `json:"siteId"`
	ContainerID      int64     `json:"containerId"`
	OrderID          string    `json:"orderId"`
	Start            time.Time `json:"start"`
	End              time.Time `json:"end,omitempty"`
	ObservedThrough  time.Time `json:"observedThrough"`
	Open             bool      `json:"open"`
	Category         string    `json:"category"`
	Summary          string    `json:"summary"`
	RootCause        string    `json:"rootCause"`
	ClaimedExclusion string    `json:"claimedExclusion,omitempty"`
	ExclusionStatus  Status    `json:"exclusionStatus"`
	SourceRef        string    `json:"sourceRef"`
}
type ObligationDetail struct {
	ID              string    `json:"id"`
	EpisodeID       string    `json:"episodeId"`
	AssetID         int64     `json:"assetId"`
	SiteID          int64     `json:"siteId"`
	ContainerID     int64     `json:"containerId"`
	OrderID         string    `json:"orderId"`
	EpisodeStart    time.Time `json:"episodeStart"`
	IntervalStart   time.Time `json:"intervalStart"`
	IntervalEnd     time.Time `json:"intervalEnd,omitempty"`
	ObservedThrough time.Time `json:"observedThrough"`
	Severity        string    `json:"severity"`
	TicketRef       string    `json:"ticketRef,omitempty"`
	TicketAt        time.Time `json:"ticketAt,omitempty"`
	SubmittedAt     time.Time `json:"submittedAt,omitempty"`
	ReceivedAt      time.Time `json:"receivedAt"`
	DiagnosedAt     time.Time `json:"diagnosedAt,omitempty"`
	RespondedAt     time.Time `json:"respondedAt,omitempty"`
	ApprovedAt      time.Time `json:"approvedAt,omitempty"`
	Deadline        time.Time `json:"deadline,omitempty"`
	RestoredAt      time.Time `json:"restoredAt,omitempty"`
	ClosedAt        time.Time `json:"closedAt,omitempty"`
	Qualified       bool      `json:"qualified"`
	SourceRef       string    `json:"sourceRef"`
}
type Target struct {
	Date              string   `json:"date,omitempty"`
	Status            Status   `json:"status"`
	Causes            []string `json:"causes,omitempty"`
	Sources           []string `json:"sources,omitempty"`
	Basis             string   `json:"basis"`
	ReasonableEfforts bool     `json:"reasonableEfforts"`
}
type Deadlines struct {
	Contract Target `json:"contract"`
	Internal Target `json:"internal"`
}
type Result struct {
	Version         string             `json:"version"`
	Period          Period             `json:"period"`
	Complete        bool               `json:"complete"`
	Metrics         map[string]Metric  `json:"metrics"`
	Containers      []Breakdown        `json:"containers"`
	Orders          []Breakdown        `json:"orders"`
	Reconciliations []Reconciliation   `json:"reconciliations"`
	Interruptions   []Interruption     `json:"interruptions"`
	Obligations     []ObligationDetail `json:"obligations"`
	Deadlines       Deadlines          `json:"deadlines"`
	Sources         []string           `json:"sources"`
}

// RequiredMetricKeys returns a fresh list so callers cannot change the report
// field contract. Every report and every alternative view contains these keys.
func RequiredMetricKeys() []string {
	keys := []string{
		"monthly_uptime", "capacity_weighted_site_uptime", "annual_uptime", "annual_energy_shortfall", "annual_service_credit", "rolling_three_month_uptime", "interim_credit_threshold_status", "exclusion_allowance_usage", "exclusion_allowance_section_4_a", "exclusion_allowance_section_4_b", "exclusion_allowance_section_4_e", "repair_credits_section_8_1", "termination_section_5_6",
		"period_uptime_hours", "period_downtime_hours", "metered_energy_kwh", "hashrate_performance_hs", "hashrate_performance_percent", "operational_units", "offline_units", "covered_units", "pool_reporting_units", "pool_failed_units", "pool_unavailable_units", "pool_unknown_units",
		"interruption_duration_hours", "interruption_exclusions", "scheduled_maintenance_hours", "unscheduled_maintenance_hours", "maintenance_hours", "incidents", "outages", "security_events", "root_cause_analysis", "curtailment_duration_hours", "curtailment_impact", "accrued_hosting_fees", "open_S3_items", "ticket_history", "year_over_year_uptime", "year_over_year_energy", "year_over_year_hashrate", "year_over_year_maintenance",
	}
	for _, category := range backlogCategories() {
		for _, measure := range []string{"month_end", "daily_average"} {
			keys = append(keys, "backlog_"+category+"_"+measure)
		}
	}
	return keys
}
func backlogCategories() []string {
	return []string{"offline", "ticket_S1", "ticket_S2", "ticket_S3", "ticket_unclassified", "age_over_7_days", "age_over_30_days", "age_over_60_days", "beyond_deadline", "unknown_coverage"}
}
