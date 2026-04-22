package activity

import (
	"context"
	"fmt"
	"log/slog"

	"golang.org/x/sync/errgroup"

	"github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

type Service struct {
	store interfaces.ActivityStore
}

func NewService(store interfaces.ActivityStore) *Service {
	return &Service{store: store}
}

// Log records an activity event on a best-effort basis.
// Insert errors are logged but never propagated to the caller.
//
// Events with a nil OrganizationID (e.g. auth failures for unknown users) are
// accepted and persisted, but the current org-scoped read queries (List, Count,
// GetFilterOptions) will not return them. A global/admin read path for org-less
// events is planned as a follow-up.
//
// Use LogStrict if the caller needs to know whether the insert actually
// succeeded (e.g. the command finalizer, whose retry loop depends on seeing
// the error so the reconciler doesn't have to clean up).
func (s *Service) Log(ctx context.Context, event models.Event) {
	if err := s.LogStrict(ctx, event); err != nil {
		slog.Error("failed to insert activity log", "error", err, "event_type", event.Type)
	}
}

// LogStrict records an activity event and returns any persistence error to
// the caller. Idempotent re-inserts against the '*.completed' partial unique
// index are swallowed by the store layer and therefore look like success
// here, which is the behavior the finalizer + reconciler rely on.
func (s *Service) LogStrict(ctx context.Context, event models.Event) error {
	if event.Result == "" {
		event.Result = models.ResultSuccess
	}
	if event.ActorType == "" {
		event.ActorType = models.ActorUser
	}
	if !event.Category.Valid() {
		slog.Warn("activity event has invalid category",
			"event_type", event.Type, "category", string(event.Category))
	}
	if !event.ActorType.Valid() {
		slog.Warn("activity event has invalid actor_type",
			"event_type", event.Type, "actor_type", string(event.ActorType))
	}
	if !event.Result.Valid() {
		slog.Warn("activity event has invalid result",
			"event_type", event.Type, "result", string(event.Result))
	}
	if event.UserID != nil && event.Username == nil && event.ActorType != models.ActorSystem {
		slog.Warn("activity event has user_id but missing username",
			"event_type", event.Type, "user_id", *event.UserID)
	}
	if event.OrganizationID == nil && event.Category != models.CategoryAuth {
		slog.Warn("activity event missing organization_id for non-auth category",
			"event_type", event.Type, "category", string(event.Category))
	}
	return s.store.Insert(ctx, &event)
}

func (s *Service) List(ctx context.Context, filter models.Filter) ([]models.Entry, error) {
	return s.store.List(ctx, filter)
}

func (s *Service) Count(ctx context.Context, filter models.Filter) (int64, error) {
	return s.store.Count(ctx, filter)
}

func (s *Service) GetFilterOptions(ctx context.Context, orgID int64) (*models.FilterOptions, error) {
	var (
		eventTypes []models.EventTypeInfo
		scopeTypes []string
		users      []models.UserInfo
	)

	// Safe to parallelize: this method is only called from the handler with a
	// plain request context, never from within a RunInTx transaction scope.
	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		var err error
		eventTypes, err = s.store.GetDistinctEventTypes(ctx, orgID)
		return err
	})

	g.Go(func() error {
		var err error
		scopeTypes, err = s.store.GetDistinctScopeTypes(ctx, orgID)
		return err
	})

	g.Go(func() error {
		var err error
		users, err = s.store.GetDistinctUsers(ctx, orgID)
		return err
	})

	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("getting activity filter options: %w", err)
	}

	return &models.FilterOptions{
		EventTypes: eventTypes,
		ScopeTypes: scopeTypes,
		Users:      users,
	}, nil
}
