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
}

type StoredNotification struct {
	ID         int64
	ReceivedAt time.Time
	DeviceName string
	DeviceMAC  string
	Notification
}

// beforeID is the keyset cursor, nil for the first page.
type Lister interface {
	List(ctx context.Context, organizationID int64, beforeID *int64, limit int32) ([]StoredNotification, error)
	// ListActive returns the latest row per alert still firing, so callers derive current state without paging through history.
	ListActive(ctx context.Context, organizationID int64, limit int32) ([]StoredNotification, error)
}
