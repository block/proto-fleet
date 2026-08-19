package alerts

import (
	"context"
	"time"
)

// ChannelRecord is the persisted form of a channel; the destination secret is an opaque encrypted blob, never in the clear here.
type ChannelRecord struct {
	ID              int64
	OrganizationID  int64
	Name            string
	Kind            ChannelKind
	EncryptedConfig string
	ValidationState ValidationState
	ValidatedAt     *time.Time
	ValidationError string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ChannelStore persists org alert channels; implementations return ErrNotFound when a row is absent.
type ChannelStore interface {
	Insert(ctx context.Context, rec ChannelRecord) (ChannelRecord, error)
	Update(ctx context.Context, rec ChannelRecord) (ChannelRecord, error)
	Get(ctx context.Context, orgID, id int64) (ChannelRecord, error)
	GetByName(ctx context.Context, orgID int64, name string) (ChannelRecord, error)
	List(ctx context.Context, orgID int64) ([]ChannelRecord, error)
	SoftDelete(ctx context.Context, orgID, id int64) error
}

// RouteMode says where a rule's alerts deliver; RouteModeDefault is the absence of a policy row and is never persisted.
type RouteMode string

const (
	RouteModeDefault RouteMode = "default"
	RouteModeCustom  RouteMode = "custom"
	RouteModeNone    RouteMode = "none"
)

// RoutePolicy is one rule's delivery override for one org; a rule with no policy delivers to every org channel.
type RoutePolicy struct {
	RuleUID    string
	Mode       RouteMode
	ChannelIDs []int64
}

// RouteStore persists per-rule delivery routing.
type RouteStore interface {
	// SetPolicy upserts the rule's policy and replaces its channel set atomically.
	SetPolicy(ctx context.Context, orgID int64, policy RoutePolicy) error
	DeletePolicy(ctx context.Context, orgID int64, ruleUID string) error
	ListPolicies(ctx context.Context, orgID int64) ([]RoutePolicy, error)
}

// MaintenanceWindowRecord is the persisted form of a maintenance window. Empty RuleUIDs means
// every rule; empty ChannelIDs means every channel. The id slices are stored intent, not foreign
// keys: a deleted rule or channel leaves its id dangling, which simply mutes nothing — dropping
// it instead would silently widen the window to "every rule/channel".
type MaintenanceWindowRecord struct {
	ID             int64
	OrganizationID int64
	RuleUIDs       []string
	ChannelIDs     []int64
	StartsAt       time.Time
	EndsAt         time.Time
	Comment        string
	CreatedBy      string
	CreatedAt      time.Time
}

// MaintenanceWindowStore persists maintenance windows; implementations return ErrNotFound when
// an update or delete targets a row the org doesn't own.
type MaintenanceWindowStore interface {
	Insert(ctx context.Context, rec MaintenanceWindowRecord) (MaintenanceWindowRecord, error)
	// Update replaces the window's scope and times; CreatedBy/CreatedAt are write-once.
	Update(ctx context.Context, rec MaintenanceWindowRecord) (MaintenanceWindowRecord, error)
	List(ctx context.Context, orgID int64) ([]MaintenanceWindowRecord, error)
	// ListActive returns only the org's windows covering now — the delivery-path read.
	ListActive(ctx context.Context, orgID int64, now time.Time) ([]MaintenanceWindowRecord, error)
	// CountUnexpired counts the org's active-or-scheduled windows (ends_at > now), for the
	// write quota; excludingID skips the row an update rewrites (0 on create).
	CountUnexpired(ctx context.Context, orgID int64, now time.Time, excludingID int64) (int64, error)
	// DeleteExpiredBefore reclaims the org's windows that ended before the cutoff (retention).
	DeleteExpiredBefore(ctx context.Context, orgID int64, before time.Time) (int64, error)
	Delete(ctx context.Context, orgID, id int64) error
}

// RuleConfigStore persists user rule configs keyed by Grafana rule UID — annotations are unusable because
// Grafana copies them onto every alert instance, bloating batches. Rows follow the route-policy lifecycle.
type RuleConfigStore interface {
	UpsertConfig(ctx context.Context, orgID int64, ruleUID string, cfg RuleConfig) error
	// GetConfig returns nil (no error) when the rule has no stored config.
	GetConfig(ctx context.Context, orgID int64, ruleUID string) (*RuleConfig, error)
	// ListConfigs returns only the requested rule UIDs' configs, so orphan rows
	// (ambiguous create failures; see CreateRule) never inflate the read path.
	ListConfigs(ctx context.Context, orgID int64, ruleUIDs []string) (map[string]RuleConfig, error)
	DeleteConfig(ctx context.Context, orgID int64, ruleUID string) error
	// SweepConfigs deletes rows for rules absent from liveRuleUIDs, sparing recently written
	// rows (in-flight creates store their config before the Grafana rule exists).
	SweepConfigs(ctx context.Context, orgID int64, liveRuleUIDs []string) (int64, error)
}

// ScopeLookup reports which of the requested placement ids are live and org-owned, for rule-scope validation. Each method returns the subset of ids that exist; callers diff against the request.
type ScopeLookup interface {
	SitesByIDs(ctx context.Context, orgID int64, ids []int64) ([]int64, error)
	BuildingsByIDs(ctx context.Context, orgID int64, ids []int64) ([]int64, error)
	// setType is "rack" or "group" (device_set.type).
	DeviceSetsByIDs(ctx context.Context, orgID int64, setType string, ids []int64) ([]int64, error)
}

// DeviceIdentity is the human-facing name + MAC for a device_id, for alert messages.
type DeviceIdentity struct {
	Name string
	MAC  string
}

// DeviceIdentityLookup resolves friendly device metadata by device_identifier within one org.
type DeviceIdentityLookup interface {
	DeviceIdentities(ctx context.Context, orgID int64, deviceIDs []string) (map[string]DeviceIdentity, error)
}
