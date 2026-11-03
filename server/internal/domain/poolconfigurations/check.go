package poolconfigurations

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// PoolReading contains only public fields. Passwords never leave verification.
type PoolReading struct {
	Priority      int32
	URL, Username string
}
type CheckResult struct {
	ReadbackAvailable  bool
	State, ResultCode  string
	CheckedAt          time.Time
	Expected, Reported []PoolReading
}
type ConfigurationChecker interface {
	CheckConfiguration(ctx context.Context, principal Principal, configurationID string, target AssignmentTarget) (CheckResult, error)
}

func (s *Service) WithChecker(checker ConfigurationChecker) *Service { s.checker = checker; return s }
func (s *Service) CheckConfiguration(ctx context.Context, accountID, configurationID string, target AssignmentTarget) (CheckResult, error) {
	if target.ID <= 0 || (target.Kind != TargetDevice && target.Kind != TargetInstallation) {
		return CheckResult{}, fleeterror.NewInvalidArgumentError("select valid equipment")
	}
	p, err := withAuthority(ctx, s, accountID, Observe, func(q sqlc.Querier, p Principal) (Principal, error) {
		if configurationID == "" {
			if target.Kind == TargetDevice {
				target.DeviceID = target.ID
			} else {
				boards, err := q.ListPoolSelectionBoards(ctx, sqlc.ListPoolSelectionBoardsParams{OrgID: p.Scope.OrganizationID, InstallationIds: []int64{target.ID}})
				if err != nil {
					return p, storageError(err)
				}
				if len(boards) != 1 {
					return p, staleAssignment()
				}
				target.DeviceID = boards[0].DeviceID
			}
			a, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: p.Scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID})
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return p, storageError(err)
			}
			if err == nil && !a.RetiredAt.Valid {
				return p, staleAssignment()
			}
			return p, nil
		}
		a, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: p.Scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID})
		if err != nil {
			return p, storageError(err)
		}
		if a.ConfigurationID != configurationID || a.AccountID.Int64 != p.Scope.AccountID || a.RetiredAt.Valid {
			return p, staleAssignment()
		}
		target.DeviceID = a.DeviceID
		return p, nil
	})
	if err != nil {
		return CheckResult{}, err
	}
	if s.checker == nil {
		return CheckResult{}, fleeterror.NewUnavailableErrorf("configuration checks are unavailable")
	}
	return s.checker.CheckConfiguration(ctx, p, configurationID, target)
}
