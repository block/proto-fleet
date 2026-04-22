package interfaces

//go:generate go run go.uber.org/mock/mockgen -source=activity.go -destination=mocks/mock_activity_store.go -package=mocks ActivityStore

import (
	"context"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/activity/models"
)

type ActivityStore interface {
	Insert(ctx context.Context, event *models.Event) error
	List(ctx context.Context, filter models.Filter) ([]models.Entry, error)
	Count(ctx context.Context, filter models.Filter) (int64, error)
	GetDistinctUsers(ctx context.Context, orgID int64) ([]models.UserInfo, error)
	GetDistinctEventTypes(ctx context.Context, orgID int64) ([]models.EventTypeInfo, error)
	GetDistinctScopeTypes(ctx context.Context, orgID int64) ([]string, error)
	// DeleteOlderThan removes at most maxRows rows whose created_at is strictly
	// before cutoff and returns the number deleted. Used by the retention
	// cleaner which loops until this returns fewer rows than maxRows.
	DeleteOlderThan(ctx context.Context, cutoff time.Time, maxRows int32) (int64, error)
}
