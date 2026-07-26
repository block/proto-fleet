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

// DeviceIdentity is the human-facing name + MAC for a device_id, for alert messages.
type DeviceIdentity struct {
	Name string
	MAC  string
}

// DeviceIdentityLookup resolves friendly device metadata by device_identifier within one org.
type DeviceIdentityLookup interface {
	DeviceIdentities(ctx context.Context, orgID int64, deviceIDs []string) (map[string]DeviceIdentity, error)
}
