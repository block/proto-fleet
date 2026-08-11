// Package alerts is the domain layer translating Proto Fleet's channel/silence concepts to Grafana's APIs.
package alerts

import "time"

type ChannelKind string

const (
	ChannelKindWebhook ChannelKind = "webhook"
	ChannelKindSlack   ChannelKind = "slack"
)

type ValidationState string

const (
	ValidationPending ValidationState = "pending"
	ValidationOK      ValidationState = "ok"
	ValidationFailed  ValidationState = "failed"
)

// BearerHeader is zeroed on reads (see Channel.HasSecret).
type WebhookConfig struct {
	URL          string
	BearerHeader string
	// ClearBearer is a write-only update flag: revoke the stored bearer even when the destination is unchanged.
	ClearBearer bool
}

// WebhookURL is the secret and reads return it empty.
type SlackConfig struct {
	WebhookURL string
}

type Channel struct {
	ID              string
	OrganizationID  int64
	Name            string
	Kind            ChannelKind
	Webhook         *WebhookConfig
	Slack           *SlackConfig
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ValidatedAt     *time.Time
	ValidationState ValidationState
	ValidationError string
	HasSecret       bool
}

type RuleTemplate string

const (
	RuleTemplateOffline        RuleTemplate = "offline"
	RuleTemplateHashrate       RuleTemplate = "hashrate"
	RuleTemplateTemperature    RuleTemplate = "temperature"
	RuleTemplatePool           RuleTemplate = "pool"
	RuleTemplateCommandFailure RuleTemplate = "command_failure"
	RuleTemplateTelemetryPoll  RuleTemplate = "telemetry-poll"

	// The MQTT curtailment templates: "curtailment engaged" and "signal
	// source disconnected". Values must match the template labels in
	// proto-fleet-rules.yaml.
	RuleTemplateMQTTCurtailment  RuleTemplate = "mqtt-curtailment"
	RuleTemplateMQTTDisconnected RuleTemplate = "mqtt-disconnected"
)

// Origin decides mutability: only user rules accept UpdateRule/DeleteRule.
type RuleOrigin string

const (
	RuleOriginProvisioned RuleOrigin = "provisioned"
	RuleOriginUser        RuleOrigin = "user"
)

type HashrateMode string

const (
	HashrateModePctExpected HashrateMode = "pct_expected"
	HashrateModeAbsolute    HashrateMode = "absolute"
)

type HashrateUnit string

const (
	HashrateUnitTerahash HashrateUnit = "TH"
	HashrateUnitPetahash HashrateUnit = "PH"
)

type OfflineRuleConfig struct{}

type HashrateRuleConfig struct {
	Mode HashrateMode `json:"mode"`
	// Percent of expected in (0, 100] for pct_expected; hashrate in Unit for absolute.
	Value float64      `json:"value"`
	Unit  HashrateUnit `json:"unit,omitempty"`
}

type TemperatureRuleConfig struct {
	MaxCelsius float64 `json:"max_celsius"`
}

// RuleScope limits which miners a rule fires for: the union of the listed placements (current
// membership via fleet_device_placement) and explicit DeviceIDs. Nil/empty means the whole org.
type RuleScope struct {
	SiteIDs     []int64  `json:"site_ids,omitempty"`
	DeviceIDs   []string `json:"device_ids,omitempty"`
	BuildingIDs []int64  `json:"building_ids,omitempty"`
	RackIDs     []int64  `json:"rack_ids,omitempty"`
	GroupIDs    []int64  `json:"group_ids,omitempty"`
	// Live "any site": miners assigned to any current or future site (excludes unassigned miners); supersedes SiteIDs.
	AllSites bool `json:"all_sites,omitempty"`
}

// IsZero reports whether the scope places no restriction (org-wide).
func (s *RuleScope) IsZero() bool {
	return s == nil || (!s.AllSites &&
		len(s.SiteIDs) == 0 && len(s.DeviceIDs) == 0 &&
		len(s.BuildingIDs) == 0 && len(s.RackIDs) == 0 && len(s.GroupIDs) == 0)
}

// RuleConfig is a user rule's definition; it round-trips through the
// alert_rule_config store so edits never need to parse the compiled SQL back apart.
type RuleConfig struct {
	Name            string                 `json:"name"`
	DurationSeconds int32                  `json:"duration_seconds"`
	Offline         *OfflineRuleConfig     `json:"offline,omitempty"`
	Hashrate        *HashrateRuleConfig    `json:"hashrate,omitempty"`
	Temperature     *TemperatureRuleConfig `json:"temperature,omitempty"`
	Scope           *RuleScope             `json:"scope,omitempty"`
	// Write-request metadata, never persisted: marks a nil Scope as deliberate org-wide rather
	// than a stale pre-scope client that cannot round-trip the field (see updateRuleSerialized).
	ScopeOrgWideExplicit bool `json:"-"`
}

func (c RuleConfig) Template() RuleTemplate {
	switch {
	case c.Offline != nil:
		return RuleTemplateOffline
	case c.Hashrate != nil:
		return RuleTemplateHashrate
	case c.Temperature != nil:
		return RuleTemplateTemperature
	}
	return ""
}

type Rule struct {
	ID              string
	OrganizationID  int64
	Name            string
	Template        RuleTemplate
	Group           string
	Severity        string
	Summary         string
	Description     string
	DurationSeconds int32
	Enabled         bool
	Origin          RuleOrigin
	// Nil for provisioned rules.
	Config *RuleConfig
	// Nil means default routing (all org channels).
	Routing *RoutePolicy
	// Set when a best-effort routing read failed: the wire omits routing entirely so the client keeps its last-known value instead of reading nil as default.
	RoutingUnknown bool
	// The rawSql the live Grafana rule evaluates, for config divergence checks; never serialized.
	CompiledSQL string
	// Set when Config no longer compiles to CompiledSQL (a save was interrupted between the Grafana write and the config write); re-saving converges both sides.
	ConfigOutOfSync bool
	// Set when a best-effort config read failed on a mutation response: the wire flags it so the client keeps its last-known config instead of reading nil as "no editable config" (mirrors RoutingUnknown).
	ConfigUnknown bool
	// The legacy pre-config-store annotation for rules created before migration 000135; reads fall back to it when the store has no row, and the first successful update migrates it into a row.
	legacyConfigJSON string
}

type MaintenanceWindowScopeKind string

const (
	MaintenanceWindowScopeRule   MaintenanceWindowScopeKind = "rule"
	MaintenanceWindowScopeGroup  MaintenanceWindowScopeKind = "group"
	MaintenanceWindowScopeSite   MaintenanceWindowScopeKind = "site"
	MaintenanceWindowScopeDevice MaintenanceWindowScopeKind = "device"
)

type MaintenanceWindowScope struct {
	Kind      MaintenanceWindowScopeKind
	RuleID    string
	GroupID   string
	SiteID    string
	DeviceIDs []string
}

// Active is derived from Now() ∈ [StartsAt, EndsAt) at read time.
type MaintenanceWindow struct {
	ID             string
	OrganizationID int64
	Scope          MaintenanceWindowScope
	StartsAt       time.Time
	EndsAt         time.Time
	Comment        string
	CreatedBy      string
	CreatedAt      time.Time
	Active         bool
}
