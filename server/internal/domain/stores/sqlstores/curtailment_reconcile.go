package sqlstores

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

var _ interfaces.CurtailmentDispatchRejectionStore = (*SQLCurtailmentStore)(nil)
var _ interfaces.CurtailmentTargetObservationStore = (*SQLCurtailmentStore)(nil)

func (s *SQLCurtailmentStore) GetDispatchRejection(ctx context.Context, orgID, eventID int64) (*string, error) {
	reason, err := s.GetQueries(ctx).GetCurtailmentDispatchRejection(ctx, sqlc.GetCurtailmentDispatchRejectionParams{OrgID: orgID, EventID: eventID})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("get dispatch rejection: %v", err)
	}
	if !reason.Valid {
		return nil, nil
	}
	return &reason.String, nil
}

func (s *SQLCurtailmentStore) RejectDispatch(ctx context.Context, orgID, eventID int64, reason string) error {
	count, err := s.GetQueries(ctx).RejectCurtailmentDispatch(ctx, sqlc.RejectCurtailmentDispatchParams{OrgID: orgID, EventID: eventID, Reason: reason})
	if err != nil {
		return fleeterror.NewInternalErrorf("persist dispatch rejection: %v", err)
	}
	if count == 0 {
		return interfaces.ErrCurtailmentEventStateRaceLoss
	}
	return nil
}

func (s *SQLCurtailmentStore) ObserveTargets(ctx context.Context, event *models.Event, observations []interfaces.CurtailmentTargetObservation) ([]string, error) {
	raw, err := json.Marshal(observations)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("encode curtailment observations: %v", err)
	}
	return s.GetQueries(ctx).BulkObserveCurtailmentTargets(ctx, sqlc.BulkObserveCurtailmentTargetsParams{EventID: event.ID, OrgID: event.OrgID, ExpectedEventState: string(event.State), Observations: raw})
}

var _ interfaces.CurtailmentDispatchFenceStore = (*SQLCurtailmentStore)(nil)

func (s *SQLCurtailmentStore) WithCurtailmentDispatchFence(ctx context.Context, event *models.Event, params interfaces.ListCandidatesParams, deviceIdentifiers []string, command func(interfaces.CurtailmentTopologyDispatchFenceSnapshot) error) error {
	if event == nil || command == nil {
		return fleeterror.NewInvalidArgumentError("dispatch fence requires an event and command")
	}
	if len(params.BuildingIDs) > 0 || len(params.RackIDs) > 0 || len(params.GroupIDs) > 0 {
		return s.WithCurtailmentTopologyDispatchFence(ctx, event, params, deviceIdentifiers, command)
	}
	return db.WithTransactionNoRetryNoResult(ctx, s.conn.DB, func(q sqlc.Querier) error {
		locked, err := q.LockCurtailmentEventByUUIDForWrite(ctx, sqlc.LockCurtailmentEventByUUIDForWriteParams{EventUuid: event.EventUUID, OrgID: event.OrgID})
		if errors.Is(err, sql.ErrNoRows) {
			return interfaces.ErrCurtailmentEventStateRaceLoss
		}
		if err != nil {
			return err
		}
		if locked.ID != event.ID || models.EventState(locked.State) != event.State || models.EventState(locked.State).IsTerminal() {
			return interfaces.ErrCurtailmentEventStateRaceLoss
		}
		if locked.DispatchRejectionReason.Valid {
			return fleeterror.NewFailedPreconditionError("curtail dispatch authorization already rejected")
		}

		if event.ExternalSource != nil && *event.ExternalSource == automationExternalSource {
			if err := lockAutomationGrantForDispatch(ctx, q, convertEventRow(locked)); err != nil {
				return err
			}
		} else {
			if _, err := q.ListEffectivePermissionsForUserForUpdate(ctx, sqlc.ListEffectivePermissionsForUserForUpdateParams{UserID: locked.CreatedByUserID, OrganizationID: locked.OrgID}); err != nil {
				return err
			}
		}
		sites, unbounded, err := lockDeviceScopeCoverage(ctx, q, event.OrgID, deviceIdentifiers, nil)
		if err != nil {
			return err
		}
		if len(params.SiteIDs) > 0 {
			allowed := map[int64]bool{}
			for _, site := range params.SiteIDs {
				allowed[site] = true
			}
			if unbounded {
				return fleeterror.NewFailedPreconditionError("claimed miner is no longer in the persisted site scope")
			}
			for _, site := range sites {
				if !allowed[site] {
					return fleeterror.NewFailedPreconditionError("claimed miner is no longer in the persisted site scope")
				}
			}
		}
		return command(interfaces.CurtailmentTopologyDispatchFenceSnapshot{Event: convertEventRow(locked), Topology: interfaces.CurtailmentTopologyDispatchSnapshot{
			Coverage: interfaces.CurtailmentTopologyScopeCoverage{SiteIDs: sites, CurrentMemberSiteIDs: sites, RequireOrgWide: unbounded}, DispatchMemberDeviceIdentifiers: deviceIdentifiers,
		}})
	})
}

func (s *SQLCurtailmentStore) FailPendingCurtailTargets(ctx context.Context, event *models.Event, reason string, includeInFlight bool) error {
	_, err := s.GetQueries(ctx).FailCurtailmentPendingDeadlineTargets(ctx, sqlc.FailCurtailmentPendingDeadlineTargetsParams{EventID: event.ID, OrgID: event.OrgID, Reason: sql.NullString{String: reason, Valid: true}, IncludeInFlight: includeInFlight})
	return err
}
