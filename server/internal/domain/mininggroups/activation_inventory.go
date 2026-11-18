package mininggroups

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

type activationPoolIdentity struct {
	HashboardID int64  `json:"hashboard_id"`
	MAC         string `json:"mac"`
	Serial      string `json:"serial"`
	AccountID   int64  `json:"account_id"`
	Revision    int64  `json:"revision"`
}

type activationOwnership struct {
	Device sqlc.GetPoolEquipmentDeviceRow
	Owners []sqlc.ListPoolEquipmentModuleOwnersRow
}

func authorizeActivationInventory(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, requirePools bool) error {
	device, err := q.GetPoolEquipmentDevice(ctx, sqlc.GetPoolEquipmentDeviceParams{OrgID: row.OrganizationID, ID: row.DeviceID})
	if err != nil {
		return err
	}
	permissions := []string{authz.PermMinerActivateMiningGroups, authz.PermMinerStartMining, authz.PermMinerStopMining}
	if requirePools {
		permissions = append(permissions, authz.PermMinerUpdatePools)
	}
	if err := authorize(ctx, q, row.ActorID.Int64, row.OrganizationID, device.SiteID, permissions...); err != nil {
		return err
	}
	return nil
}

func (s *Service) fetchActivationInventory(ctx context.Context, deviceID int64) (*sdk.MiningGroupInventory, error) {
	result, err := s.call(ctx, deviceID, sdk.MiningGroupRequest{Action: "activation_inventory"})
	if err != nil || result.Inventory == nil {
		return nil, fleeterror.NewFailedPreconditionErrorf("%w", newInventoryStaleError("Fleet cannot verify the complete hashboard identity inventory. Refresh the controller and check that its Fleet Node supports this inventory read."))
	}
	return result.Inventory, nil
}

func (s *Service) readActivationInventory(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, requirePools bool) ([]activationPoolIdentity, string, error) {
	if err := authorizeActivationInventory(ctx, q, row, requirePools); err != nil {
		return nil, "", err
	}
	inventory, err := s.fetchActivationInventory(ctx, row.DeviceID)
	if err != nil {
		return nil, "", err
	}
	return resolveActivationInventory(ctx, q, row, requirePools, inventory)
}

// Resolve a controller read against locked database evidence. No device I/O or
// actor authorization occurs here; completed handoffs retain their saved intent.
func resolveActivationInventory(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, requirePools bool, inventory *sdk.MiningGroupInventory) ([]activationPoolIdentity, string, error) {
	device, err := q.GetPoolEquipmentDevice(ctx, sqlc.GetPoolEquipmentDeviceParams{OrgID: row.OrganizationID, ID: row.DeviceID})
	if err != nil {
		return nil, "", err
	}
	if err := q.LockHostedAccountAssetInventory(ctx, []int64{row.DeviceID}); err != nil {
		return nil, "", err
	}
	if err := inventory.Validate(); err != nil {
		return nil, "", fleeterror.NewFailedPreconditionError(err.Error())
	}
	if requirePools && len(inventory.Boards) == 0 {
		return nil, "", fleeterror.NewFailedPreconditionError("Hashboard inventory is empty. Fleet cannot transfer the container pool assignment.")
	}
	if age := time.Since(inventory.ObservedAt); age > 2*time.Minute || age < -5*time.Second {
		return nil, "", fleeterror.NewFailedPreconditionError("Hashboard inventory is stale. Refresh the controller inventory before enabling mining groups.")
	}
	data, err := json.Marshal(inventory.Boards)
	if err != nil {
		return nil, "", fmt.Errorf("encode activation inventory: %w", err)
	}
	boards, err := q.ListActivationPoolInventory(ctx, sqlc.ListActivationPoolInventoryParams{OrganizationID: row.OrganizationID, DeviceID: row.DeviceID, Boards: data})
	if err != nil {
		return nil, "", err
	}
	if len(boards) != len(inventory.Boards) {
		return nil, "", fleeterror.NewFailedPreconditionError("Hashboard inventory contains an identity that Fleet cannot verify. Wait for a fresh inventory before enabling mining groups.")
	}
	identities := make([]activationPoolIdentity, 0, len(boards))
	ownership := activationOwnership{Device: device}
	seen := map[int64]bool{}
	for _, board := range boards {
		if !board.Ready || seen[board.HashboardID] {
			return nil, "", fleeterror.NewFailedPreconditionError("Hashboard inventory has stale or conflicting controller ownership. Refresh the inventory before enabling mining groups.")
		}
		seen[board.HashboardID] = true
		if board.AccountID != 0 {
			return nil, "", fleeterror.NewForbiddenError("Mining groups cannot be enabled while a hashboard belongs to a customer account.")
		}
		identities = append(identities, activationPoolIdentity{HashboardID: board.HashboardID, MAC: board.MacAddress, Serial: board.Serial, AccountID: board.AccountID, Revision: board.Revision})
		ownership.Owners = append(ownership.Owners, sqlc.ListPoolEquipmentModuleOwnersRow{HashboardID: board.HashboardID, AccountID: board.AccountID, Revision: board.Revision})
	}
	encoded, err := json.Marshal(ownership)
	return identities, string(encoded), err
}

// Container pools also apply to boards connected after the original update.
// Validate current ownership separately and preserve allocation generations for
// boards present in both observations. Slot and module IDs are not ownership.
func sameActivationPoolOwner(previous, current string) bool {
	var before, after activationOwnership
	if json.Unmarshal([]byte(previous), &before) != nil || json.Unmarshal([]byte(current), &after) != nil || !reflect.DeepEqual(before.Device, after.Device) {
		return false
	}
	owners := map[int64]sqlc.ListPoolEquipmentModuleOwnersRow{}
	for _, owner := range before.Owners {
		owners[owner.HashboardID] = owner
	}
	for _, owner := range after.Owners {
		if old, found := owners[owner.HashboardID]; found && (old.AccountID != owner.AccountID || old.Revision != owner.Revision) {
			return false
		}
	}
	return true
}

func activationPoolPlacements(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, inventory []activationPoolIdentity) ([]activationPoolBoard, bool, error) {
	ids := make([]int64, len(inventory))
	byID := map[int64]activationPoolIdentity{}
	for i, board := range inventory {
		ids[i], byID[board.HashboardID] = board.HashboardID, board
	}
	boards, err := q.ListPoolSelectionBoards(ctx, sqlc.ListPoolSelectionBoardsParams{OrgID: row.OrganizationID, HashboardIds: ids})
	if err != nil {
		return nil, false, err
	}
	complete := len(boards) == len(ids)
	var selected []activationPoolBoard
	for _, board := range boards {
		if board.DeviceID != row.DeviceID || !board.Ready || board.MacAddress != byID[board.HashboardID].MAC {
			complete = false
		}
		_, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: row.OrganizationID, TargetKind: poolconfigurations.TargetInstallation, TargetID: board.InstallationID})
		if !errors.Is(err, sql.ErrNoRows) {
			if err != nil {
				return nil, false, err
			}
			return nil, false, fleeterror.NewFailedPreconditionError(activationPoolRecovery)
		}
		selected = append(selected, activationPoolBoard{Binding: poolapplications.BoardBinding{InstallationID: board.InstallationID, HashboardID: board.HashboardID, ModuleID: board.ModuleID, Position: board.Position, MAC: board.MacAddress}})
	}
	if !complete {
		return nil, false, nil
	}
	return selected, true, nil
}

func (s *Service) validateActivationInventory(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, capture activationPoolCapture, transferred bool) error {
	inventory, owner, err := s.readActivationInventory(ctx, q, row, capture.SnapshotID != "")
	if err != nil {
		return err
	}
	return validateActivationPoolEvidence(ctx, q, row, capture, transferred, inventory, owner)
}

func validateActivationPoolEvidence(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, capture activationPoolCapture, transferred bool, inventory []activationPoolIdentity, owner string) error {
	if !reflect.DeepEqual(inventory, capture.Inventory) || (capture.SnapshotID != "" && owner != capture.Ownership) {
		return fleeterror.NewFailedPreconditionError("Hashboard inventory or ownership changed during activation. Refresh the container and review its pool assignment.")
	}
	if capture.SnapshotID == "" {
		return nil
	}
	if !transferred || capture.Deferred {
		source, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: row.OrganizationID, TargetKind: poolconfigurations.TargetDevice, TargetID: row.DeviceID})
		if err != nil {
			return err
		}
		if source.Version != capture.Source.Version || source.RetiredAt.Valid || source.ConfigurationID != capture.Source.ConfigurationID || source.RevisionID != capture.Source.RevisionID || source.AccountID != capture.Source.AccountID {
			return fleeterror.NewFailedPreconditionError(activationPoolRecovery)
		}
	}
	runtime, err := q.GetPoolConfigurationRuntime(ctx, sqlc.GetPoolConfigurationRuntimeParams{OrgID: row.OrganizationID, ID: row.DeviceID})
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(runtime, capture.Runtime) {
		return fleeterror.NewFailedPreconditionError(activationPoolRecovery)
	}
	if capture.Deferred {
		return nil
	}
	ids := make([]int64, len(capture.Boards))
	for i, board := range capture.Boards {
		ids[i] = board.Binding.InstallationID
	}
	boards, err := q.ListPoolSelectionBoards(ctx, sqlc.ListPoolSelectionBoardsParams{OrgID: row.OrganizationID, InstallationIds: ids})
	if err != nil {
		return err
	}
	if len(boards) != len(capture.Boards) {
		return fleeterror.NewFailedPreconditionError(activationPoolRecovery)
	}
	for i, board := range boards {
		binding := poolapplications.BoardBinding{InstallationID: board.InstallationID, HashboardID: board.HashboardID, ModuleID: board.ModuleID, Position: board.Position, MAC: board.MacAddress}
		if !board.Ready || board.DeviceID != row.DeviceID || binding != capture.Boards[i].Binding {
			return fleeterror.NewFailedPreconditionError(activationPoolRecovery)
		}
		assignment, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: row.OrganizationID, TargetKind: poolconfigurations.TargetInstallation, TargetID: board.InstallationID})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if transferred {
			if err != nil || assignment.RetiredAt.Valid || assignment.Version != 1 || assignment.OperationID.Valid || assignment.ConfigurationID != capture.Source.ConfigurationID || assignment.RevisionID != capture.Source.RevisionID {
				return fleeterror.NewFailedPreconditionError(activationPoolRecovery)
			}
		} else if err == nil {
			return fleeterror.NewFailedPreconditionError(activationPoolRecovery)
		}
	}
	return nil
}

// A later inventory poll can resolve placement without another controller write.
// Keep the original source, identities and frozen usernames until the complete
// assignment transfer commits under the organization and controller locks.
func (s *Service) finishPendingActivationPlacement(ctx context.Context, orgID, deviceID int64) error {
	pending, err := s.queries.HasPendingMiningGroupPoolHandoff(ctx, sqlc.HasPendingMiningGroupPoolHandoffParams{OrganizationID: orgID, DeviceID: deviceID})
	if err != nil || !pending {
		return err
	}
	// Obtain the read under normal observation admission, before the organization
	// lock. Recheck its identities, freshness and runtime binding inside the commit.
	runtime, err := s.queries.GetPoolConfigurationRuntime(ctx, sqlc.GetPoolConfigurationRuntimeParams{OrgID: orgID, ID: deviceID})
	if err != nil {
		return err
	}
	var inventory *sdk.MiningGroupInventory
	err = s.writes.WithHostObservation(ctx, orgID, deviceID, runtime.Epoch, func(ctx context.Context) error {
		return s.writes.WithBindingObservation(ctx, deviceID, func(sqlc.Querier) error { return nil }, func(ctx context.Context) error {
			var readErr error
			inventory, readErr = s.fetchActivationInventory(ctx, deviceID)
			return readErr
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
			if (row.Status != statusComplete && row.Status != "failed") || row.OperationKind != operationActivate || row.Step.Valid || row.ControllerOperationID != "" {
				return nil
			}
			journal, err := q.GetMiningGroupPoolHandoff(ctx, sqlc.GetMiningGroupPoolHandoffParams{OrganizationID: orgID, DeviceID: deviceID, OperationID: row.OperationID.UUID})
			if err != nil || !journal.PlacementPending {
				return err
			}
			var capture activationPoolCapture
			if err := decode(journal.Captured, &capture); err != nil {
				return err
			}
			identities, owner, err := resolveActivationInventory(ctx, q, row, true, inventory)
			if err != nil {
				return err
			}
			if err := validateActivationPoolEvidence(ctx, q, row, capture, true, identities, owner); err != nil {
				return err
			}
			var observed sdk.MiningGroupResult
			if err := decode(row.Observed, &observed); err != nil {
				return err
			}
			if !row.ObservedAt.Valid || time.Since(row.ObservedAt.Time) > reconciliationTimeout || !observed.Enabled || observed.Pending || observed.Suspended {
				return nil
			}
			boards, ready, err := activationPoolPlacements(ctx, q, row, capture.Inventory)
			if err != nil || !ready {
				return err
			}
			members := map[string]bool{}
			for _, group := range observed.Groups {
				if group.ID == "default" {
					for _, mac := range group.MACs {
						members[mac] = true
					}
				}
			}
			for _, board := range capture.Inventory {
				if !members[board.MAC] {
					return nil
				}
			}
			bindings := map[string]binding{}
			if err := decode(row.Bindings, &bindings); err != nil {
				return err
			}
			if bindings["default"] != (binding{ConfigID: capture.SnapshotID, Configured: true, Inherited: true}) {
				return fleeterror.NewFailedPreconditionError(activationPoolRecovery)
			}
			capture.Boards, capture.Deferred = boards, false
			if err := q.SetMiningGroupPoolHandoffPlacementPending(ctx, sqlc.SetMiningGroupPoolHandoffPlacementPendingParams{OrganizationID: orgID, DeviceID: deviceID, OperationID: row.OperationID.UUID}); err != nil {
				return err
			}
			if err := s.adoptActivationPoolAssignments(ctx, q, &row, capture); err != nil {
				return err
			}
			encoded, err := encode(capture)
			if err != nil {
				return err
			}
			if err := q.UpdateMiningGroupPoolHandoffCapture(ctx, sqlc.UpdateMiningGroupPoolHandoffCaptureParams{OrganizationID: orgID, DeviceID: deviceID, OperationID: row.OperationID.UUID, Captured: encoded}); err != nil {
				return err
			}
			row.Message = "Mining groups are enabled. Pool assignments have been transferred to the hashboards."
			return save(ctx, q, row)
		})
	})
}

func (s *Service) activationPlacementMessage(ctx context.Context, row *sqlc.ContainerMiningGroupState) {
	if pending, err := s.queries.HasPendingMiningGroupPoolHandoff(ctx, sqlc.HasPendingMiningGroupPoolHandoffParams{OrganizationID: row.OrganizationID, DeviceID: row.DeviceID}); err == nil && pending {
		row.Message = devicewrite.ActivationPoolPlacementPending
	}
}
