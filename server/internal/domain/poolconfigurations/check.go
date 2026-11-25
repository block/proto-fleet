package poolconfigurations

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// MaxReadbackControllers bounds a request to one concurrent wave. Each
// controller gets its own timeout, with no work queued behind slow devices.
const MaxReadbackControllers = 4

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
	ReadDevicePools(ctx context.Context, principal Principal, targets []AssignmentTarget) []CheckResult
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

// ReadDevicePools observes a bounded selection without canonical assignments.
// Target failures are unavailable readings; invalid requests and account-level
// authority failures reject the whole request before any device I/O.
func (s *Service) ReadDevicePools(ctx context.Context, accountID string, targets []AssignmentTarget) ([]CheckResult, error) {
	if len(targets) == 0 || len(targets) > 500 {
		return nil, fleeterror.NewInvalidArgumentError("select between 1 and 500 targets")
	}
	targets = append([]AssignmentTarget(nil), targets...)
	seen := make(map[AssignmentTarget]bool, len(targets))
	var installations []int64
	for i := range targets {
		target := &targets[i]
		target.DeviceID = 0 // Resolve routing exclusively from current server bindings.
		if target.ID <= 0 || (target.Kind != TargetDevice && target.Kind != TargetInstallation) || seen[*target] {
			return nil, fleeterror.NewInvalidArgumentError("select distinct valid equipment")
		}
		seen[*target] = true
		if target.Kind == TargetInstallation {
			installations = append(installations, target.ID)
		}
	}
	p, err := withAuthority(ctx, s, accountID, Observe, func(q sqlc.Querier, p Principal) (Principal, error) {
		boards, err := q.ListPoolSelectionBoards(ctx, sqlc.ListPoolSelectionBoardsParams{OrgID: p.Scope.OrganizationID, InstallationIds: installations})
		if err != nil {
			return p, storageError(err)
		}
		devices := make(map[int64]int64, len(boards))
		for _, board := range boards {
			devices[board.InstallationID] = board.DeviceID
		}
		for i := range targets {
			if targets[i].Kind == TargetDevice {
				targets[i].DeviceID = targets[i].ID
			} else {
				targets[i].DeviceID = devices[targets[i].ID]
			}
		}
		return p, nil
	})
	if err != nil {
		return nil, err
	}
	if s.checker == nil {
		return nil, fleeterror.NewUnavailableErrorf("configuration checks are unavailable")
	}
	controllers := make(map[int64]struct{})
	for _, target := range targets {
		if target.DeviceID > 0 {
			controllers[target.DeviceID] = struct{}{}
		}
	}
	if len(controllers) > MaxReadbackControllers {
		return nil, fleeterror.NewInvalidArgumentError("select at most 4 controllers per readback request")
	}
	return s.checker.ReadDevicePools(ctx, p, targets), nil
}
