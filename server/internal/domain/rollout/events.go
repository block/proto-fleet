package rollout

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// Event is one committed, append-only lifecycle record.
type Event struct {
	ID, RolloutID, ChannelID int64
	Type                     string
	OccurredAt               time.Time
	Actor                    Actor
	RolloutRevision          int64
	Note                     string
	DeviceIdentifiers        []string
}

// EventFilter binds a polling cursor to its organization and scope.
type EventFilter struct {
	RolloutID, ChannelID int64
	PageSize             int32
	Cursor               string
}

// lockEventStream must precede channel/header locks in every transaction that
// appends an event. Reusing the scope lock preserves admission lock ordering
// and makes event IDs commit in order within each organization's stream.
func (s *Service) lockEventStream(ctx context.Context, orgID int64) error {
	if err := s.store.GetQueries(ctx).LockReleaseChannelScopes(ctx, orgID); err != nil {
		return fleeterror.NewInternalErrorf("lock rollout event stream: %w", err)
	}
	return nil
}

func (s *Service) persistRolloutEvent(ctx context.Context, r sqlc.FirmwareRollout, eventType string, system bool, extra map[string]any) error {
	q := s.store.GetQueries(ctx)
	// Device and header triggers own revisions. Read after all mutations so
	// events identify the revision the caller will receive.
	current, err := q.GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{RolloutID: r.ID, OrgID: r.OrgID})
	if err != nil {
		return rolloutLookupError(r.ID, err)
	}
	actor := Actor{Type: current.LastActionByType, ID: current.LastActionByID, Name: current.LastActionByName}
	if override, ok := extra["_actor"].(Actor); ok {
		actor = override
	}
	if system {
		actor = SystemActor
	}
	note, _ := extra["note"].(string)
	identifiers, _ := extra["device_identifiers"].([]string)
	// Large system changes use bounded events without losing target identity.
	for {
		size := min(len(identifiers), 1000)
		batch := identifiers[:size]
		if batch == nil {
			batch = []string{}
		}
		if err := q.CreateFirmwareRolloutEvent(ctx, sqlc.CreateFirmwareRolloutEventParams{
			RolloutID: r.ID, Type: eventType, ActorType: actor.Type, ActorID: actor.ID, ActorName: actor.Name,
			Note: note, DeviceIdentifiers: batch,
		}); err != nil {
			return fleeterror.NewInternalErrorf("record rollout event: %w", err)
		}
		if size == len(identifiers) {
			break
		}
		identifiers = identifiers[size:]
	}
	return nil
}

// ListRolloutEvents returns the oldest events after the cursor. Even an empty
// page retains a cursor, allowing a controller to poll without replaying events.
func (s *Service) ListRolloutEvents(ctx context.Context, orgID int64, filter EventFilter) ([]Event, string, error) {
	if filter.RolloutID < 0 || filter.ChannelID < 0 || filter.PageSize < 0 || filter.PageSize > 1000 {
		return nil, "", fleeterror.NewInvalidArgumentError("invalid event filter")
	}
	binding := []string{strconv.FormatInt(orgID, 36), strconv.FormatInt(filter.RolloutID, 36), strconv.FormatInt(filter.ChannelID, 36)}
	var after int64
	if filter.Cursor != "" {
		parts, err := decodeCursor(filter.Cursor, 5)
		if err != nil {
			return nil, "", err
		}
		if parts[0] != "e1" || parts[1] != binding[0] || parts[2] != binding[1] || parts[3] != binding[2] {
			return nil, "", fleeterror.NewInvalidArgumentError("event cursor does not match the requested scope")
		}
		after, err = strconv.ParseInt(parts[4], 36, 64)
		if err != nil || after < 0 {
			return nil, "", fleeterror.NewInvalidArgumentError("invalid event cursor")
		}
	}
	rows, err := s.store.GetQueries(ctx).ListFirmwareRolloutEvents(ctx, sqlc.ListFirmwareRolloutEventsParams{
		OrgID: orgID, RolloutID: filter.RolloutID, ChannelID: filter.ChannelID, AfterID: after, PageSize: clampPageSize(filter.PageSize),
	})
	if err != nil {
		return nil, "", fleeterror.NewInternalErrorf("list rollout events: %w", err)
	}
	events := make([]Event, 0, len(rows))
	for _, row := range rows {
		events = append(events, Event{ID: row.ID, RolloutID: row.RolloutID, ChannelID: row.ChannelID, Type: row.Type,
			OccurredAt: row.OccurredAt, Actor: Actor{Type: row.ActorType, ID: row.ActorID, Name: row.ActorName},
			RolloutRevision: row.RolloutRevision, Note: row.Note, DeviceIdentifiers: row.DeviceIdentifiers})
		after = row.ID
	}
	return events, encodeCursor("e1", binding[0], binding[1], binding[2], strconv.FormatInt(after, 36)), nil
}

func (s *Service) setControllerWait(ctx context.Context, rolloutID int64, waiting sql.NullTime) error {
	if err := s.store.GetQueries(ctx).SetFirmwareRolloutControllerWait(ctx, sqlc.SetFirmwareRolloutControllerWaitParams{RolloutID: rolloutID, WaitingSince: waiting}); err != nil {
		return fleeterror.NewInternalErrorf("record controller wait: %w", err)
	}
	return nil
}

func requeuedNames(rows []sqlc.ListReleaseChannelSuppressedMembersRow, ids []int64) []string {
	selected := make(map[int64]bool, len(ids))
	for _, id := range ids {
		selected[id] = true
	}
	var names []string
	for _, row := range rows {
		if ids == nil || selected[row.DeviceID] {
			names = append(names, row.DeviceIdentifier)
		}
	}
	return names
}
