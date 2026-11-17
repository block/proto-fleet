package rollout

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// EventControllerSoftwareTargetChanged is the activity type recorded when a
// channel's controller software target is set, changed or cleared. Operator
// copy calls the target the channel's controller software version. Keep the
// type in sync with the client label maps
// (client/src/protoFleet/features/activity/utils).
const EventControllerSoftwareTargetChanged = "release_channel_controller_software_changed"

// ChannelContainer is a container resolved into a channel, with the controller
// software version it reports.
type ChannelContainer struct {
	DeviceIdentifier          string
	ControllerSoftwareVersion string
	OnTarget                  bool
}

// ControllerSoftware is a channel's controller software target for one
// container model, with counts. The target starts no update:
// it marks which containers are behind and holds their firmware updates.
type ControllerSoftware struct {
	Manufacturer   string
	Model          string
	TargetVersion  string
	ContainerCount int32
	OnTargetCount  int32
}

// ListControllerSoftware returns every container model of the channel that has
// members or a target, ordered by manufacturer then model.
func (s *Service) ListControllerSoftware(ctx context.Context, orgID, channelID int64) ([]ControllerSoftware, error) {
	q := s.store.GetQueries(ctx)
	if _, err := q.GetReleaseChannel(ctx, sqlc.GetReleaseChannelParams{ChannelID: channelID, OrgID: orgID}); err != nil {
		return nil, channelLookupError(channelID, err)
	}
	return s.controllerSoftware(ctx, orgID, channelID)
}

func (s *Service) controllerSoftware(ctx context.Context, orgID, channelID int64) ([]ControllerSoftware, error) {
	rows, err := s.store.GetQueries(ctx).ListReleaseChannelControllerSoftwareSummary(ctx, sqlc.ListReleaseChannelControllerSoftwareSummaryParams{OrgID: orgID, ChannelID: channelID})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("list controller software summary: %w", err)
	}
	models := make([]ControllerSoftware, 0, len(rows))
	for _, row := range rows {
		models = append(models, ControllerSoftware{Manufacturer: row.Manufacturer, Model: row.Model,
			TargetVersion: row.TargetVersion, ContainerCount: row.ContainerCount, OnTargetCount: row.OnTargetCount})
	}
	// Preserve the summary's byte ordering across database collations.
	sort.Slice(models, func(i, j int) bool {
		if models[i].Manufacturer != models[j].Manufacturer {
			return models[i].Manufacturer < models[j].Manufacturer
		}
		return models[i].Model < models[j].Model
	})
	return models, nil
}

// ListControllerSoftwareContainers returns one page for an exact observed pair.
func (s *Service) ListControllerSoftwareContainers(ctx context.Context, orgID, channelID int64, manufacturer, model string, pageSize int32, cursor string) ([]ChannelContainer, string, error) {
	q := s.store.GetQueries(ctx)
	if _, err := q.GetReleaseChannel(ctx, sqlc.GetReleaseChannelParams{ChannelID: channelID, OrgID: orgID}); err != nil {
		return nil, "", channelLookupError(channelID, err)
	}
	params := sqlc.ListReleaseChannelContainersPageParams{OrgID: orgID, ChannelID: channelID, Manufacturer: manufacturer, Model: model}
	if cursor != "" {
		parts, err := decodeCursor(cursor, 2)
		if err != nil {
			return nil, "", err
		}
		id, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || id <= 0 {
			return nil, "", fleeterror.NewInvalidArgumentError("invalid cursor")
		}
		params.AfterIdentifier = sql.NullString{String: parts[0], Valid: true}
		params.AfterDeviceID = sql.NullInt64{Int64: id, Valid: true}
	}
	limit := clampPageSize(pageSize)
	params.PageLimit = limit + 1
	rows, err := q.ListReleaseChannelContainersPage(ctx, params)
	if err != nil {
		return nil, "", fleeterror.NewInternalErrorf("list channel containers: %w", err)
	}
	next := ""
	if len(rows) > int(limit) {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = encodeCursor(last.DeviceIdentifier, strconv.FormatInt(last.DeviceID, 10))
	}
	containers := make([]ChannelContainer, 0, len(rows))
	for _, row := range rows {
		containers = append(containers, ChannelContainer{DeviceIdentifier: row.DeviceIdentifier, ControllerSoftwareVersion: row.ReportedVersion, OnTarget: row.OnTarget})
	}
	return containers, next, nil
}

// SetControllerSoftware sets a container model's target version, or clears it
// when version is empty. The version must be that of an uploaded controller
// software package. It starts no update; it changes which containers a rollout
// may dispatch firmware to.
func (s *Service) SetControllerSoftware(ctx context.Context, orgID, userID, channelID int64, manufacturer, model, version string) error {
	manufacturer, model, version = strings.TrimSpace(manufacturer), strings.TrimSpace(model), strings.TrimSpace(version)
	if manufacturer == "" || model == "" {
		return fleeterror.NewInvalidArgumentError("manufacturer and model are required")
	}
	var channelName string
	changed := true
	err := s.tx.RunInTx(ctx, func(ctx context.Context) error {
		q := s.store.GetQueries(ctx)
		// Match assignment changes: the scope lock, then the channel. Dispatch
		// takes the same locks, so a target change never interleaves with it.
		if err := s.lockEventStream(ctx, orgID); err != nil {
			return err
		}
		channel, err := q.GetReleaseChannelForUpdate(ctx, sqlc.GetReleaseChannelForUpdateParams{ChannelID: channelID, OrgID: orgID})
		if err != nil {
			return channelLookupError(channelID, err)
		}
		channelName = channel.Name
		if version == "" {
			cleared, err := q.DeleteReleaseChannelControllerSoftware(ctx, sqlc.DeleteReleaseChannelControllerSoftwareParams{
				ChannelID: channelID, Manufacturer: manufacturer, Model: model,
			})
			if err != nil {
				return fleeterror.NewInternalErrorf("clear controller software target: %w", err)
			}
			// Clearing a pair that has no target is not an operator change.
			changed = cleared > 0
		} else {
			uploaded, err := q.ControllerSoftwareVersionUploaded(ctx, sqlc.ControllerSoftwareVersionUploadedParams{OrgID: orgID, Version: version})
			if err != nil {
				return fleeterror.NewInternalErrorf("check controller software packages: %w", err)
			}
			if !uploaded {
				return fleeterror.NewFailedPreconditionErrorf("no uploaded controller software package has version %q; upload it before setting it as the target", version)
			}
			if _, err := q.UpsertReleaseChannelControllerSoftware(ctx, sqlc.UpsertReleaseChannelControllerSoftwareParams{
				ChannelID: channelID, Manufacturer: manufacturer, Model: model, Version: version, AssignedBy: userID,
			}); err != nil {
				return fleeterror.NewInternalErrorf("set controller software target: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if changed {
		s.logControllerSoftwareTarget(ctx, orgID, channelID, channelName, manufacturer, model, version)
	}
	return nil
}

func (s *Service) logControllerSoftwareTarget(ctx context.Context, orgID, channelID int64, channelName, manufacturer, model, version string) {
	if s.activity == nil {
		return
	}
	pair := strings.TrimSpace(manufacturer + " " + model)
	description := fmt.Sprintf("Set controller software version: %s %s → %s", channelName, pair, version)
	if version == "" {
		description = fmt.Sprintf("Cleared controller software version: %s %s", channelName, pair)
	}
	event := activitymodels.Event{
		Category:       activitymodels.CategoryDeviceCommand,
		Type:           EventControllerSoftwareTargetChanged,
		Description:    description,
		OrganizationID: &orgID,
		Metadata: map[string]any{
			"channel_id": channelID, "channel_name": channelName,
			"manufacturer": manufacturer, "model": model, "controller_software_version": version,
		},
	}
	activity.StampActor(ctx, &event)
	s.activity.Log(ctx, event)
}

// controllerSoftwareHold returns, of the given devices, the containers the
// channel's controller software target holds firmware from: the target version
// each is waiting for, by device ID. The controller installs that firmware, so
// it is brought to the channel's version first. Empty when the channel sets no
// target or every container is on it.
func (s *Service) controllerSoftwareHold(ctx context.Context, channelID int64, deviceIDs []int64) (map[int64]string, error) {
	if len(deviceIDs) == 0 {
		return nil, nil
	}
	rows, err := s.store.GetQueries(ctx).ListDevicesBehindControllerSoftware(ctx, sqlc.ListDevicesBehindControllerSoftwareParams{
		ChannelID: channelID, DeviceIds: deviceIDs,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("check controller software targets: %w", err)
	}
	held := make(map[int64]string, len(rows))
	for _, row := range rows {
		held[row.DeviceID] = row.TargetVersion
	}
	return held, nil
}

// holdControllerSoftware explains a rollout that has nothing to dispatch
// because every container due an update is behind the channel's controller
// software target.
func holdControllerSoftware(held map[int64]string) string {
	versions := map[string]bool{}
	for _, version := range held {
		versions[version] = true
	}
	names := make([]string, 0, len(versions))
	for version := range versions {
		names = append(names, version)
	}
	sort.Strings(names)
	noun, whose := "containers", "their"
	if len(held) == 1 {
		noun, whose = "container", "its"
	}
	return fmt.Sprintf("Waiting for controller software %s on %d %s. Update %s controller software to continue.",
		strings.Join(names, ", "), len(held), noun, whose)
}
