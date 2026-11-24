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

// Keep controller reads outside the transaction that creates the activation
// attempt. Plugin reads must see a committed admission, and slow reads must not
// hold the organization update lock. Capture rechecks this binding and evidence.
type activationInventoryEvidence struct {
	Runtime   sqlc.GetPoolConfigurationRuntimeRow
	Inventory *sdk.MiningGroupInventory
}

func (s *Service) prepareActivationInventory(ctx context.Context, orgID, actorID, deviceID int64) (activationInventoryEvidence, error) {
	var evidence activationInventoryEvidence
	miner, err := s.miners.GetMinerForCommand(ctx, deviceID)
	if err != nil {
		return evidence, err
	}
	if checker, ok := miner.(interface {
		CheckMiningGroupActivationInventory(ctx context.Context) error
	}); ok {
		if err := checker.CheckMiningGroupActivationInventory(ctx); err != nil {
			return evidence, err
		}
	}
	runtime, err := s.queries.GetPoolConfigurationRuntime(ctx, sqlc.GetPoolConfigurationRuntimeParams{OrgID: orgID, ID: deviceID})
	if err != nil {
		return evidence, err
	}
	evidence.Runtime = runtime
	err = s.writes.WithHostObservation(ctx, orgID, deviceID, runtime.Epoch, func(ctx context.Context) error {
		return s.writes.WithBindingObservation(ctx, deviceID, func(q sqlc.Querier) error {
			fresh, err := q.GetPoolConfigurationRuntime(ctx, sqlc.GetPoolConfigurationRuntimeParams{OrgID: orgID, ID: deviceID})
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(fresh, runtime) {
				return fleeterror.NewFailedPreconditionError(activationInventoryBindingChanged)
			}
			return authorizeActivationInventory(ctx, q, sqlc.ContainerMiningGroupState{OrganizationID: orgID, DeviceID: deviceID, ActorID: sql.NullInt64{Int64: actorID, Valid: true}}, false)
		}, func(ctx context.Context) error {
			var readErr error
			evidence.Inventory, readErr = s.fetchActivationInventory(ctx, deviceID)
			return readErr
		})
	})
	return evidence, err
}

const activationInventoryBindingChanged = "Container changed while reading hashboard inventory. Refresh the container before enabling mining groups."

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
		return nil, fleeterror.NewFailedPreconditionErrorf("%w", &inventoryStaleError{message: "Fleet cannot verify the complete hashboard identity inventory. Refresh the controller and check that its Fleet Node supports this inventory read.", cause: err})
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
		return nil, "", fleeterror.NewFailedPreconditionErrorf("%w", newInventoryStaleError("Hashboard inventory is stale. Refresh the controller inventory before enabling mining groups."))
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
		if !board.BindingReady || seen[board.HashboardID] {
			return nil, "", fleeterror.NewFailedPreconditionError("Hashboard inventory has stale or conflicting controller ownership. Refresh the inventory before enabling mining groups.")
		}
		seen[board.HashboardID] = true
		if board.AccountID != 0 {
			return nil, "", fleeterror.NewForbiddenError("Mining groups cannot be enabled while a hashboard belongs to a customer account.")
		}
		if !board.ObservationFresh {
			return nil, "", fleeterror.NewFailedPreconditionErrorf("%w", newInventoryStaleError("Hashboard observation is stale. Waiting for a fresh controller inventory."))
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
	installationIDs := make([]int64, len(boards))
	for i, board := range boards {
		installationIDs[i] = board.InstallationID
	}
	assigned, err := q.HasActivationPoolInstallationAssignments(ctx, sqlc.HasActivationPoolInstallationAssignmentsParams{OrganizationID: row.OrganizationID, InstallationIds: installationIDs})
	if err != nil {
		return nil, false, err
	}
	if assigned {
		return nil, false, fleeterror.NewFailedPreconditionError(activationPoolRecovery)
	}
	complete := len(boards) == len(ids)
	var selected []activationPoolBoard
	for _, board := range boards {
		if board.DeviceID != row.DeviceID || !board.Ready || board.MacAddress != byID[board.HashboardID].MAC {
			complete = false
			continue
		}
		selected = append(selected, activationPoolBoard{Binding: poolapplications.BoardBinding{InstallationID: board.InstallationID, HashboardID: board.HashboardID, ModuleID: board.ModuleID, Position: board.Position, MAC: board.MacAddress}})
	}
	return selected, complete, nil
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
	if !transferred || (capture.Deferred && !capture.PartialTransfer) {
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

func (s *Service) activationPlacementMessage(ctx context.Context, row *sqlc.ContainerMiningGroupState) {
	if active(row.Status) || row.Status == "failed" {
		return
	}
	if pending, err := s.queries.HasPendingMiningGroupPoolHandoff(ctx, sqlc.HasPendingMiningGroupPoolHandoffParams{OrganizationID: row.OrganizationID, DeviceID: row.DeviceID}); err == nil && pending {
		row.Message = devicewrite.ActivationPoolPlacementPending
	}
}
