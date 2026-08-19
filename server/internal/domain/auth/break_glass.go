package auth

import (
	"context"
	"fmt"

	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"golang.org/x/crypto/bcrypt"
)

const breakGlassResetEventType = "cli_reset_password"

type sessionRevoker interface {
	RevokeAllSessions(ctx context.Context, userID int64) error
}

type strictActivityLogger interface {
	LogStrict(ctx context.Context, event activitymodels.Event) error
}

// BreakGlassService owns the transaction used by fleetd's offline recovery
// command. It is separate from the authenticated user-management service so
// the command cannot accidentally inherit request/session assumptions.
type BreakGlassService struct {
	userStore  stores.BreakGlassUserStore
	transactor stores.Transactor
	sessions   sessionRevoker
	activity   strictActivityLogger
}

type BreakGlassResetResult struct {
	Username string
}

func NewBreakGlassService(
	userStore stores.BreakGlassUserStore,
	transactor stores.Transactor,
	sessions sessionRevoker,
	activity strictActivityLogger,
) *BreakGlassService {
	return &BreakGlassService{
		userStore:  userStore,
		transactor: transactor,
		sessions:   sessions,
		activity:   activity,
	}
}

// ResetSuperAdminPassword resets the sole live org-scope SUPER_ADMIN, revokes
// its sessions, and writes the audit event in one transaction.
func (s *BreakGlassService) ResetSuperAdminPassword(ctx context.Context, password string) (*BreakGlassResetResult, error) {
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}

	// Do the expensive, fallible bcrypt work before opening a transaction.
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	var result BreakGlassResetResult
	err = s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		admins, err := s.userStore.LockActiveSuperAdminUsers(txCtx)
		if err != nil {
			return fmt.Errorf("find SUPER_ADMIN: %w", err)
		}
		if len(admins) == 0 {
			hasUser, err := s.userStore.HasUser(txCtx)
			if err != nil {
				return fmt.Errorf("check onboarding state: %w", err)
			}
			if !hasUser {
				return fmt.Errorf("no users exist; complete onboarding before resetting a SUPER_ADMIN password")
			}
			return fmt.Errorf("SUPER_ADMIN invariant violated: no live org-scope SUPER_ADMIN exists")
		}
		if len(admins) != 1 {
			return fmt.Errorf("SUPER_ADMIN invariant violated: found %d live org-scope SUPER_ADMIN users", len(admins))
		}

		admin := admins[0]
		rowsAffected, err := s.userStore.BreakGlassResetUserPassword(txCtx, admin.ID, string(hashedPassword))
		if err != nil {
			return fmt.Errorf("reset SUPER_ADMIN password: %w", err)
		}
		if rowsAffected != 1 {
			return fmt.Errorf("reset SUPER_ADMIN password: expected one updated user, got %d", rowsAffected)
		}
		if err := s.sessions.RevokeAllSessions(txCtx, admin.ID); err != nil {
			return fmt.Errorf("revoke SUPER_ADMIN sessions: %w", err)
		}
		if err := s.activity.LogStrict(txCtx, activitymodels.Event{
			Category:       activitymodels.CategoryAuth,
			Type:           breakGlassResetEventType,
			Description:    "Break-glass SUPER_ADMIN password reset",
			ActorType:      activitymodels.ActorSystem,
			OrganizationID: &admin.OrganizationID,
			Metadata: map[string]any{
				"target_user_id":  admin.ExternalUserID,
				"target_username": admin.Username,
			},
		}); err != nil {
			return fmt.Errorf("write password reset activity: %w", err)
		}

		result.Username = admin.Username
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}
