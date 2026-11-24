package mininggroups

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Pending identity intent belongs to its activation journal, not the latest
// container operation. Mapped-board edits can replace that operation freely.
func (s *Service) finishPendingActivationPlacement(ctx context.Context, orgID, deviceID int64) error {
	pending, err := s.queries.ListActivationPoolBoardIntents(ctx, sqlc.ListActivationPoolBoardIntentsParams{OrganizationID: orgID, DeviceID: deviceID})
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}
	runtime, err := s.queries.GetPoolConfigurationRuntime(ctx, sqlc.GetPoolConfigurationRuntimeParams{OrgID: orgID, ID: deviceID})
	if err != nil {
		return err
	}
	var inventory *sdk.MiningGroupInventory
	err = s.writes.WithHostObservation(ctx, orgID, deviceID, runtime.Epoch, func(ctx context.Context) error {
		return s.writes.WithBindingObservation(ctx, deviceID, func(sqlc.Querier) error { return nil }, func(ctx context.Context) error {
			var err error
			inventory, err = s.fetchActivationInventory(ctx, deviceID)
			return err
		})
	})
	if err != nil {
		return err
	}
	return s.writes.WithBindingUpdateTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
		if _, err := q.LockPoolConfigurationImportOrganization(ctx, orgID); err != nil {
			return err
		}
		return s.writes.WithBindingObservation(ctx, deviceID, func(sqlc.Querier) error { return nil }, func(ctx context.Context) error {
			row, err := q.LockMiningGroupState(ctx, sqlc.LockMiningGroupStateParams{OrganizationID: orgID, DeviceID: deviceID})
			if err != nil {
				return err
			}
			if active(row.Status) || row.Step.Valid || row.ControllerOperationID != "" {
				return nil
			}
			fresh, err := q.GetPoolConfigurationRuntime(ctx, sqlc.GetPoolConfigurationRuntimeParams{OrgID: orgID, ID: deviceID})
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(fresh, runtime) {
				return nil
			}
			if err := inventory.Validate(); err != nil {
				return err
			}
			if age := time.Since(inventory.ObservedAt); age > 2*time.Minute || age < -5*time.Second {
				return nil
			}
			if err := q.LockHostedAccountAssetInventory(ctx, []int64{deviceID}); err != nil {
				return err
			}
			pending, err := q.ListActivationPoolBoardIntents(ctx, sqlc.ListActivationPoolBoardIntentsParams{OrganizationID: orgID, DeviceID: deviceID})
			if err != nil || len(pending) == 0 {
				return err
			}
			journal, err := q.GetMiningGroupPoolHandoff(ctx, sqlc.GetMiningGroupPoolHandoffParams{OrganizationID: orgID, DeviceID: deviceID, OperationID: pending[0].OperationID})
			if err != nil {
				return err
			}
			var capture activationPoolCapture
			if err := decode(journal.Captured, &capture); err != nil {
				return err
			}
			// Placement and unrelated worker metadata may change; controller identity may not.
			if !slices.Equal(fresh.IdentityKeys, capture.Runtime.IdentityKeys) {
				return fleeterror.NewFailedPreconditionError("Controller identity or inherited pool settings changed. Pending board assignments require review.")
			}
			var observed sdk.MiningGroupResult
			if err := decode(row.Observed, &observed); err != nil {
				return err
			}
			if !row.ObservedAt.Valid || time.Since(row.ObservedAt.Time) > reconciliationTimeout || !observed.Enabled || observed.Pending || observed.Suspended {
				return nil
			}
			bindings := map[string]binding{}
			if err := decode(row.Bindings, &bindings); err != nil {
				return err
			}
			if bindings["default"] != (binding{ConfigID: capture.SnapshotID, Configured: true, Inherited: true}) {
				return fleeterror.NewFailedPreconditionError("Controller identity or inherited pool settings changed. Pending board assignments require review.")
			}
			members := map[string]bool{}
			for _, g := range observed.Groups {
				if g.ID == "default" {
					for _, mac := range g.MACs {
						members[mac] = true
					}
				}
			}
			data, err := json.Marshal(inventory.Boards)
			if err != nil {
				return fmt.Errorf("encode pending board inventory: %w", err)
			}
			resolved, err := q.ListActivationPoolInventory(ctx, sqlc.ListActivationPoolInventoryParams{OrganizationID: orgID, DeviceID: deviceID, Boards: data})
			if err != nil {
				return err
			}
			byID := map[int64]sqlc.ListActivationPoolInventoryRow{}
			ambiguous := map[int64]bool{}
			for _, board := range resolved {
				if _, exists := byID[board.HashboardID]; exists {
					ambiguous[board.HashboardID] = true
				}
				byID[board.HashboardID] = board
			}
			var valid []activationPoolIdentity
			for _, intent := range pending {
				board, found := byID[intent.HashboardID]
				if !found || ambiguous[intent.HashboardID] || !board.Ready || !members[intent.MacAddress] || board.AccountID != intent.AccountID || board.Revision != intent.OwnershipRevision || board.MacAddress != intent.MacAddress || board.Serial != intent.Serial {
					continue
				}
				valid = append(valid, activationPoolIdentity{HashboardID: board.HashboardID, MAC: board.MacAddress, Serial: board.Serial, AccountID: board.AccountID, Revision: board.Revision})
			}
			boards, _, err := activationPoolPlacements(ctx, q, row, valid)
			if err != nil {
				return err
			}
			// Unresolved boards cost one batch read, not one assignment query each.
			// Delete reservations and adopt only ready installations in this transaction.
			for _, board := range boards {
				if err := q.DeleteActivationPoolBoardIntent(ctx, sqlc.DeleteActivationPoolBoardIntentParams{OrganizationID: orgID, DeviceID: deviceID, HashboardID: board.Binding.HashboardID}); err != nil {
					return err
				}
			}
			one := capture
			one.Boards = boards
			if err := s.adoptActivationPoolBoards(ctx, q, &row, one); err != nil {
				return err
			}
			remaining := len(pending) - len(boards)
			if remaining == 0 {
				if err := q.SetMiningGroupPoolHandoffPlacementPending(ctx, sqlc.SetMiningGroupPoolHandoffPlacementPendingParams{OrganizationID: orgID, DeviceID: deviceID, OperationID: journal.OperationID}); err != nil {
					return err
				}
				if row.Status != "failed" {
					row.Message = "Pool assignments have been transferred to all hashboards."
				}
			} else if row.Status != "failed" {
				row.Message = "Some pool assignments are waiting for board locations or ownership checks. Mapped boards remain editable."
			}
			return save(ctx, q, row)
		})
	})
}
