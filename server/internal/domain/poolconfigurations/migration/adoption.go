package migration

import (
	"context"
	"strconv"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
)

// Import runs offline with application writers stopped. Existing explicit
// board intent and complete private device snapshots can be adopted. Matching
// reported URL/username values alone cannot establish a configuration.
func Import(ctx context.Context, q sqlc.Querier, secrets *encrypt.Service, orgID int64, snapshots ...DeviceSnapshot) (poolconfigurations.ImportReport, error) {
	if err := q.BoundPoolConfigurationImportLocks(ctx); err != nil {
		return poolconfigurations.ImportReport{}, err
	}
	if _, err := q.LockPoolConfigurationImportOrganization(ctx, orgID); err != nil {
		return poolconfigurations.ImportReport{}, err
	}
	if err := q.LockPoolAssignmentAdoptionSources(ctx); err != nil {
		return poolconfigurations.ImportReport{}, err
	}
	blocked, err := blockedPoolDevices(ctx, q, orgID)
	if err != nil {
		return poolconfigurations.ImportReport{}, err
	}
	devices, issues, err := prepareDevices(ctx, q, orgID, snapshots, blocked)
	if err != nil {
		return poolconfigurations.ImportReport{}, err
	}
	report, err := Libraries(ctx, q, secrets, orgID, devices...)
	if err != nil {
		return report, err
	}
	bySource := map[poolconfigurations.ImportSource]poolconfigurations.ImportedConfiguration{}
	report.Unresolved = append(report.Unresolved, issues...)
	for _, imported := range report.Imported {
		bySource[imported.Source] = imported
	}
	controllers, err := importControllers(ctx, q, orgID, blocked, &report)
	if err != nil {
		return report, err
	}
	rows, err := q.ListLegacyHashboardPoolAdoptions(ctx, orgID)
	if err != nil {
		return poolconfigurations.ImportReport{}, err
	}
	repository := poolconfigurations.NewRepository(secrets)
	for _, candidate := range devices {
		imported, found := bySource[candidate.Source]
		if !found {
			continue
		}
		deviceID, err := strconv.ParseInt(candidate.Source.ID, 10, 64)
		if err != nil {
			return poolconfigurations.ImportReport{}, fleeterror.NewInternalError("invalid device import identity")
		}
		target := poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: deviceID, DeviceID: deviceID}
		if err := repository.Adopt(ctx, q, candidate.Scope, target, imported.ConfigurationID, imported.RevisionID); err != nil {
			return poolconfigurations.ImportReport{}, err
		}
		report.AdoptedAssignments = append(report.AdoptedAssignments, poolconfigurations.ImportedAssignment{Target: target, ConfigurationID: imported.ConfigurationID, RevisionID: imported.RevisionID})
	}
	candidates := []int64{}
	eligible := map[int64]sqlc.ListLegacyHashboardPoolAdoptionsRow{}
	for _, row := range rows {
		source := poolconfigurations.ImportSource{Kind: "hashboard_intent", ID: strconv.FormatInt(row.HashboardID, 10), Version: row.OperationID.String()}
		reason := ""
		_, found := bySource[poolconfigurations.ImportSource{Kind: "hosted_revision", ID: row.PoolRevisionID, Version: strconv.FormatInt(row.SourceVersion, 10)}]
		switch {
		case blocked[row.DeviceID]:
			reason = unsettledPoolReason
		case !row.PhysicalMatch:
			reason = "The original board installation is no longer current; no assignment was adopted."
		case row.ConfigurationOwner != row.EquipmentOwner:
			reason = "Equipment ownership changed; the previous owner's private configuration was not adopted."
		case !found:
			reason = "The saved configuration requires review before adoption."
		}
		if reason != "" {
			report.Unresolved = append(report.Unresolved, poolconfigurations.ImportIssue{Source: source, Reason: reason})
			continue
		}
		candidates = append(candidates, row.InstallationID)
		eligible[row.InstallationID] = row
	}
	adopted, err := q.AdoptLegacyPoolBoardAssignments(ctx, sqlc.AdoptLegacyPoolBoardAssignmentsParams{OrganizationID: orgID, InstallationIds: candidates})
	if err != nil {
		return report, err
	}
	for _, assignment := range adopted {
		row := eligible[assignment.TargetID]
		imported := bySource[poolconfigurations.ImportSource{Kind: "hosted_revision", ID: row.PoolRevisionID, Version: strconv.FormatInt(row.SourceVersion, 10)}]
		if err := adoptBoardExecution(ctx, q, orgID, row, imported, controllers[row.DeviceID]); err != nil {
			if !fleeterror.IsFailedPreconditionError(err) {
				return report, err
			}
			report.Unresolved = append(report.Unresolved, poolconfigurations.ImportIssue{Source: poolconfigurations.ImportSource{Kind: "hashboard_intent", ID: strconv.FormatInt(row.HashboardID, 10), Version: row.OperationID.String()}, Reason: "Controller membership is ambiguous; resolve retained group identity before completing adoption."})
			delete(eligible, assignment.TargetID)
			continue
		}
		report.AdoptedAssignments = append(report.AdoptedAssignments, poolconfigurations.ImportedAssignment{Target: poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetInstallation, ID: row.InstallationID, DeviceID: row.DeviceID}, ConfigurationID: assignment.ConfigurationID, RevisionID: assignment.RevisionID})
		delete(eligible, assignment.TargetID)
	}
	for _, row := range eligible {
		report.Unresolved = append(report.Unresolved, poolconfigurations.ImportIssue{Source: poolconfigurations.ImportSource{Kind: "hashboard_intent", ID: strconv.FormatInt(row.HashboardID, 10), Version: row.OperationID.String()}, Reason: "Equipment or configuration intent changed; import did not replace it."})
	}
	return report, nil
}

const unsettledPoolReason = "Equipment has an unfinished pool or device-lifecycle command, an unresolved write, or an unfinished mining-group operation; no assignment was adopted."

func blockedPoolDevices(ctx context.Context, q sqlc.Querier, orgID int64) (map[int64]bool, error) {
	work, err := q.ListUnsettledPoolAdoptionWork(ctx, orgID)
	if err != nil {
		return nil, err
	}
	blocked := map[int64]bool{}
	for _, item := range work {
		if devicewrite.MayAffectPools(item.OperationKind, item.CommandType) {
			blocked[item.DeviceID] = true
		}
	}
	return blocked, nil
}
