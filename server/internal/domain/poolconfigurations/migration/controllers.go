package migration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
)

type importedGroupBinding struct {
	ConfigID   string `json:"config_id"`
	Configured bool   `json:"configured"`
}

type importedController struct {
	bindings        map[string]importedGroupBinding
	observed        sdk.MiningGroupResult
	sourceSnapshots map[string][]string
}

// Retained group identity is independent of library deduplication. In particular,
// two groups using equal credentials must not acquire the same planner identity.
func importControllers(ctx context.Context, q sqlc.Querier, orgID int64, blocked map[int64]bool, report *poolconfigurations.ImportReport) (map[int64]importedController, error) {
	sources, err := q.ListHostedPoolImportSources(ctx, orgID)
	if err != nil {
		return nil, err
	}
	imported := map[string]poolconfigurations.ImportedConfiguration{}
	for _, item := range report.Imported {
		if item.Source.Kind == "hosted_revision" {
			imported[item.Source.ID] = item
		}
	}
	slots := map[string]json.RawMessage{}
	for _, source := range sources {
		item, ok := imported[source.RevisionID]
		if !ok {
			continue
		}
		revision, err := q.GetPoolConfigurationRevision(ctx, sqlc.GetPoolConfigurationRevisionParams{OrganizationID: orgID, AccountID: sql.NullInt64{Int64: source.OwnerAccountID, Valid: source.OwnerAccountID != 0}, ConfigurationID: item.ConfigurationID, RevisionID: item.RevisionID})
		if err != nil {
			return nil, err
		}
		slots[source.RevisionID] = revision.Slots
	}
	rows, err := q.ListPoolImportControllerBindings(ctx, orgID)
	if err != nil {
		return nil, err
	}
	controllers := map[int64]importedController{}
	for _, row := range rows {
		if blocked[row.DeviceID] {
			continue
		}
		controller := importedController{bindings: map[string]importedGroupBinding{}, sourceSnapshots: map[string][]string{}}
		if err := json.Unmarshal(row.Bindings, &controller.bindings); err != nil {
			return nil, fmt.Errorf("decode retained controller bindings: %w", err)
		}
		if err := json.Unmarshal(row.Observed, &controller.observed); err != nil {
			return nil, fmt.Errorf("decode retained controller observation: %w", err)
		}
		changed := false
		for groupID, binding := range controller.bindings {
			source, found := imported[binding.ConfigID]
			if !found {
				_, err := q.GetPoolGroupSnapshot(ctx, sqlc.GetPoolGroupSnapshotParams{OrganizationID: orgID, DeviceID: row.DeviceID, SnapshotID: binding.ConfigID})
				if err == nil {
					for legacyID := range imported {
						key := fmt.Sprintf("pool-cutover:%d:%d:%s:%s", orgID, row.DeviceID, groupID, legacyID)
						if uuid.NewSHA1(uuid.NameSpaceOID, []byte(key)).String() == binding.ConfigID {
							controller.sourceSnapshots[legacyID] = append(controller.sourceSnapshots[legacyID], binding.ConfigID)
						}
					}
					continue
				}
				if !errors.Is(err, sql.ErrNoRows) {
					return nil, err
				}
				report.Unresolved = append(report.Unresolved, poolconfigurations.ImportIssue{Source: poolconfigurations.ImportSource{Kind: "controller_binding", ID: strconv.FormatInt(row.DeviceID, 10) + ":" + groupID, Version: binding.ConfigID}, Reason: "Retained controller credentials could not be imported."})
				continue
			}
			key := fmt.Sprintf("pool-cutover:%d:%d:%s:%s", orgID, row.DeviceID, groupID, binding.ConfigID)
			snapshotID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(key)).String()
			if err := q.CreatePoolGroupSnapshot(ctx, sqlc.CreatePoolGroupSnapshotParams{OrganizationID: orgID, DeviceID: row.DeviceID, SnapshotID: snapshotID, ConfigurationID: source.ConfigurationID, RevisionID: source.RevisionID, Slots: slots[binding.ConfigID]}); err != nil {
				return nil, err
			}
			controller.sourceSnapshots[binding.ConfigID] = append(controller.sourceSnapshots[binding.ConfigID], snapshotID)
			binding.ConfigID = snapshotID
			controller.bindings[groupID] = binding
			changed = true
		}
		if changed {
			encoded, err := json.Marshal(controller.bindings)
			if err != nil {
				return nil, fmt.Errorf("encode retained controller bindings: %w", err)
			}
			if err := q.RewritePoolImportControllerBindings(ctx, sqlc.RewritePoolImportControllerBindingsParams{OrganizationID: orgID, DeviceID: row.DeviceID, Bindings: encoded}); err != nil {
				return nil, err
			}
		}
		controllers[row.DeviceID] = controller
	}
	return controllers, nil
}

func adoptBoardExecution(ctx context.Context, q sqlc.Querier, orgID int64, row sqlc.ListLegacyHashboardPoolAdoptionsRow, imported poolconfigurations.ImportedConfiguration, controller importedController) error {
	snapshotID := ""
	for _, group := range controller.observed.Groups {
		for _, mac := range group.MACs {
			if mac != row.MacAddress {
				continue
			}
			binding, found := controller.bindings[group.ID]
			if !found {
				continue
			}
			snapshot, err := q.GetPoolGroupSnapshot(ctx, sqlc.GetPoolGroupSnapshotParams{OrganizationID: orgID, DeviceID: row.DeviceID, SnapshotID: binding.ConfigID})
			if err != nil {
				return err
			}
			if snapshot.ConfigurationID == imported.ConfigurationID && snapshot.RevisionID == imported.RevisionID {
				if snapshotID != "" && snapshotID != snapshot.SnapshotID {
					return fleeterror.NewFailedPreconditionError("Ambiguous controller membership; no execution intent was adopted.")
				}
				snapshotID = snapshot.SnapshotID
			}
		}
	}
	if snapshotID == "" {
		matches := controller.sourceSnapshots[row.PoolRevisionID]
		if len(matches) > 1 {
			return fleeterror.NewFailedPreconditionError("Controller membership is required to distinguish retained groups; no execution intent was adopted.")
		}
		if len(matches) == 1 {
			snapshotID = matches[0]
		}
	}
	if snapshotID == "" {
		// Explicit legacy intent can exist without a current group. Preserve its
		// identity and exact encrypted settings; adoption itself does not dispatch it.
		key := fmt.Sprintf("pool-cutover:%d:%d:intent:%s", orgID, row.DeviceID, row.PoolRevisionID)
		snapshotID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(key)).String()
		revision, err := q.GetPoolConfigurationRevision(ctx, sqlc.GetPoolConfigurationRevisionParams{OrganizationID: orgID, AccountID: sql.NullInt64{Int64: row.ConfigurationOwner, Valid: row.ConfigurationOwner != 0}, ConfigurationID: imported.ConfigurationID, RevisionID: imported.RevisionID})
		if err != nil {
			return err
		}
		if err := q.CreatePoolGroupSnapshot(ctx, sqlc.CreatePoolGroupSnapshotParams{OrganizationID: orgID, DeviceID: row.DeviceID, SnapshotID: snapshotID, ConfigurationID: imported.ConfigurationID, RevisionID: imported.RevisionID, Slots: revision.Slots}); err != nil {
			return err
		}
	}
	return q.AdoptPoolGroupIntent(ctx, sqlc.AdoptPoolGroupIntentParams{OrganizationID: orgID, InstallationID: row.InstallationID, GroupSnapshotID: snapshotID})
}
