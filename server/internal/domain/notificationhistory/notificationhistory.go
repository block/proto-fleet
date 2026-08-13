package notificationhistory

import (
	"context"
	"time"
)

type Notification struct {
	AlertName      string
	Status         string
	Severity       string
	RuleGroup      string
	Fingerprint    string
	OrganizationID *int64
	DeviceID       string
	Template       string
	Summary        string
	StartsAt       *time.Time
	EndsAt         *time.Time
	Labels         map[string]string
	Annotations    map[string]string
}

type Store interface {
	Insert(ctx context.Context, n *Notification) error
	// InsertBatch persists many notifications atomically (all-or-nothing), for large alert batches.
	InsertBatch(ctx context.Context, notifs []*Notification) error
}

type StoredNotification struct {
	ID         int64
	ReceivedAt time.Time
	DeviceName string
	DeviceMAC  string
	// The active row's immutable per-alert identity, and so its page cursor; empty on history rows, which page on ID.
	AlertKey string
	Notification
}

// ActiveAlertGroup is one firing rule's rollup: its blast radius across the fleet rather than a single instance.
// Identity and counts only; per-instance detail (severity, summary) belongs to the drill-in rows that carry it.
type ActiveAlertGroup struct {
	AlertName string
	RuleGroup string
	// Instances vs distinct miners: they diverge only for rules that fire on a non-device dimension.
	AlertCount     int64
	DeviceCount    int64
	FirstStartedAt time.Time
}

type ActiveAlertFilter struct {
	AlertName string
	// Matched exactly, so drilling into one group can't pull in another group's miners; "" is the group of
	// rules carrying no rule label, which is how the rollup reports them too.
	RuleGroup string
	// The previous page's last alert key; empty for the first page.
	AfterKey string
	Limit    int32
}

type Lister interface {
	// List pages history descending by row id; a nil beforeID starts at the newest.
	List(ctx context.Context, organizationID int64, beforeID *int64, limit int32) ([]StoredNotification, error)
	// ListActive returns the latest row per alert still firing, newest first, so callers derive current state
	// without paging through history.
	ListActive(ctx context.Context, organizationID int64, limit int32) ([]StoredNotification, error)
	// ListActiveByAlert narrows that set to one rule's instances, one per affected miner, which an outage makes
	// as large as the fleet. Keyset-paged on the alert key rather than the row id, which a re-assert rewrites.
	ListActiveByAlert(ctx context.Context, organizationID int64, filter ActiveAlertFilter) ([]StoredNotification, error)
	// ListActiveGroups rolls the firing set up per rule, widest blast radius first, so an org-wide outage
	// costs one row per alert instead of one per miner.
	ListActiveGroups(ctx context.Context, organizationID int64, limit int32) ([]ActiveAlertGroup, error)
}
