package sqlstores_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/rollout/betweenchannel"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
)

func TestRolloutLaneMembersListUsesHistoricalAndCurrentChannels(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 2)
	q := sqlc.New(db)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	lane := createMembershipTestLane(t, service, orgID, actorID, "Member list", deviceIDs)
	started, err := service.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID:          orgID,
		LaneID:         lane.ID,
		Name:           "Member list target",
		ReleaseTargets: []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "b")},
		Batches: []rollout.CreateBatch{{
			Label: "all",
			Members: []rollout.CreateMember{
				{DeviceIdentifier: deviceIDs[0]},
				{DeviceIdentifier: deviceIDs[1]},
			},
		}},
		IdempotencyKey: "member-list-target",
		Reason:         "exercise split membership list",
		ActorUserID:    actorID,
	})
	require.NoError(t, err)
	require.NotNil(t, started.Rollout.TargetChannelID)

	_, err = q.TestMoveDeviceChannelMembership(
		t.Context(),
		sqlc.TestMoveDeviceChannelMembershipParams{
			TargetChannelID:  *started.Rollout.TargetChannelID,
			OrgID:            orgID,
			DeviceIdentifier: deviceIDs[1],
		},
	)
	require.NoError(t, err)

	first, err := service.ListMembers(t.Context(), betweenchannel.ListMembersRequest{
		OrgID:             orgID,
		LaneID:            lane.ID,
		Limit:             1,
		IncludeTotalCount: true,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), first.TotalCount)
	require.Len(t, first.Members, 1)
	assert.NotEmpty(t, first.NextIdentifier)

	second, err := service.ListMembers(t.Context(), betweenchannel.ListMembersRequest{
		OrgID:           orgID,
		LaneID:          lane.ID,
		AfterIdentifier: first.NextIdentifier,
		Limit:           1,
	})
	require.NoError(t, err)
	require.Len(t, second.Members, 1)
	assert.Zero(t, second.TotalCount)
	assert.Empty(t, second.NextIdentifier)
	assert.NotEqual(t, first.Members[0].ChannelPosition, second.Members[0].ChannelPosition)
	assert.NotEqual(t, first.Members[0].OnCurrentChannel, second.Members[0].OnCurrentChannel)

	assignments, err := service.GetAssignments(t.Context(), orgID, deviceIDs)
	require.NoError(t, err)
	require.Len(t, assignments, 2)
	for _, assignment := range assignments {
		assert.Equal(t, lane.ID, assignment.LaneID)
		assert.Equal(t, lane.Label, assignment.LaneLabel)
	}

	reloaded, err := service.GetLane(t.Context(), orgID, lane.ID, false, nil)
	require.NoError(t, err)
	assert.Equal(t, int32(2), reloaded.MemberCount)
}

func TestRolloutLaneCreationAllowsEmptyLaneAndBlocksStart(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	before := membershipMutationCounts(t, db, orgID)

	lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:          orgID,
		Label:          "Empty lane",
		ReleaseTargets: []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "e")},
		IdempotencyKey: "create-empty-lane",
		ActorUserID:    actorID,
	})
	require.NoError(t, err)
	assert.Zero(t, lane.MemberCount)
	assert.Zero(t, lane.FirmwareConvergence.TotalCount)
	assert.Zero(t, lane.FirmwareConvergence.PendingCount)
	require.Len(t, lane.Channels, 1)
	after := membershipMutationCounts(t, db, orgID)
	assert.Equal(t, before.Memberships, after.Memberships)
	assert.Equal(t, before.Authorities+1, after.Authorities)
	assert.Equal(t, before.Enforcements, after.Enforcements)
	assert.Equal(t, before.Changes, after.Changes)

	_, err = service.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID:          orgID,
		LaneID:         lane.ID,
		Name:           "Empty lane rollout",
		ReleaseTargets: []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "f")},
		Batches: []rollout.CreateBatch{{
			Label:   "all",
			Members: []rollout.CreateMember{{DeviceIdentifier: deviceIDs[0]}},
		}},
		IdempotencyKey: "start-empty-lane",
		Reason:         "prove empty lane guard",
		ActorUserID:    actorID,
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, betweenchannel.ErrLaneEmpty.Error())

	err = service.DeleteLane(t.Context(), betweenchannel.DeleteLaneRequest{
		OrgID:            orgID,
		LaneID:           lane.ID,
		ExpectedRevision: lane.Revision,
		IdempotencyKey:   "delete-empty-lane",
		Reason:           "empty lane lifecycle proof",
		ActorUserID:      actorID,
		ActorType:        rollout.ActorTypeUser,
	})
	require.NoError(t, err)
}

func TestRolloutLaneCreationReassignsAtomicallyAndAudits(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 2)
	q := sqlc.New(db)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	source := createMembershipTestLane(t, service, orgID, actorID, "Creation source", deviceIDs[:1])

	preview, err := service.PreviewLane(t.Context(), betweenchannel.PreviewLaneRequest{
		OrgID:             orgID,
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "b")},
		DeviceIdentifiers: deviceIDs,
	})
	require.NoError(t, err)
	assert.True(t, preview.RequiresReassignConfirmation)
	require.Len(t, preview.Reassignments, 1)
	assert.Equal(t, source.ID, preview.Reassignments[0].SourceLaneID)

	request := betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Creation target",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "b")},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "create-with-reassignment",
		ActorUserID:       actorID,
	}
	_, err = service.CreateLane(t.Context(), request)
	require.Error(t, err)
	assert.ErrorContains(t, err, betweenchannel.ErrReassignmentConfirmationRequired.Error())
	assert.Equal(t, []string{deviceIDs[0]}, channelMembers(t, db, orgID, source.CurrentChannelID))

	request.ConfirmReassignment = true
	request.ReassignmentConfirmationToken = "tampered"
	_, err = service.CreateLane(t.Context(), request)
	require.Error(t, err)
	assert.ErrorContains(t, err, betweenchannel.ErrReassignmentConfirmationRequired.Error())
	assert.Equal(t, []string{deviceIDs[0]}, channelMembers(t, db, orgID, source.CurrentChannelID))

	request.ReassignmentConfirmationToken = preview.ReassignmentConfirmationToken
	target, err := service.CreateLane(t.Context(), request)
	require.NoError(t, err)
	assert.ElementsMatch(t, deviceIDs, channelMembers(t, db, orgID, target.CurrentChannelID))
	assert.Empty(t, channelMembers(t, db, orgID, source.CurrentChannelID))

	reloadedSource, err := service.GetLane(t.Context(), orgID, source.ID, false, nil)
	require.NoError(t, err)
	assert.Equal(t, source.Revision+1, reloadedSource.Revision)
	audit, err := q.GetRolloutLaneMembershipChangeTestState(
		t.Context(),
		sqlc.GetRolloutLaneMembershipChangeTestStateParams{
			OrgID:          orgID,
			IdempotencyKey: request.IdempotencyKey,
		},
	)
	require.NoError(t, err)
	assert.True(t, audit.HasAuthority)
	assert.Contains(t, audit.Applied, source.ID.String())
	assert.Contains(t, audit.Applied, source.Label)

	replayed, err := service.CreateLane(t.Context(), request)
	require.NoError(t, err)
	assert.Equal(t, target.ID, replayed.ID)
	reloadedSource, err = service.GetLane(t.Context(), orgID, source.ID, false, nil)
	require.NoError(t, err)
	assert.Equal(t, source.Revision+1, reloadedSource.Revision)
	changedToken := request
	changedToken.ReassignmentConfirmationToken = "different-token"
	_, err = service.CreateLane(t.Context(), changedToken)
	require.ErrorIs(t, err, betweenchannel.ErrIdempotencyConflict)

	assignments, err := service.GetAssignments(t.Context(), orgID, deviceIDs)
	require.NoError(t, err)
	require.Len(t, assignments, 2)
	for _, assignment := range assignments {
		assert.Equal(t, target.ID, assignment.LaneID)
	}
	crossOrgAssignments, err := service.GetAssignments(t.Context(), orgID+999999, deviceIDs)
	require.NoError(t, err)
	assert.Empty(t, crossOrgAssignments)

	err = service.DeleteLane(t.Context(), betweenchannel.DeleteLaneRequest{
		OrgID:            orgID,
		LaneID:           target.ID,
		ExpectedRevision: target.Revision,
		IdempotencyKey:   "delete-created-target",
		Reason:           "prove archived assignments are excluded",
		ActorUserID:      actorID,
		ActorType:        rollout.ActorTypeUser,
	})
	require.NoError(t, err)
	assignments, err = service.GetAssignments(t.Context(), orgID, deviceIDs)
	require.NoError(t, err)
	assert.Empty(t, assignments)
}

func TestRolloutLaneCreationRejectsPreviewAfterSourceRevisionChanges(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	source := createMembershipTestLane(t, service, orgID, actorID, "Revision source", deviceIDs)
	targets := []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "r")}
	preview, err := service.PreviewLane(t.Context(), betweenchannel.PreviewLaneRequest{
		OrgID:             orgID,
		ReleaseTargets:    targets,
		DeviceIdentifiers: deviceIDs,
	})
	require.NoError(t, err)

	revisions, err := sqlc.New(db).BumpRolloutLaneMembershipRevisions(
		t.Context(),
		sqlc.BumpRolloutLaneMembershipRevisionsParams{
			OrgID:   orgID,
			LaneIds: []uuid.UUID{source.ID},
		},
	)
	require.NoError(t, err)
	require.Len(t, revisions, 1)
	before := membershipMutationCounts(t, db, orgID)

	_, err = service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:                         orgID,
		Label:                         "Stale revision target",
		ReleaseTargets:                targets,
		DeviceIdentifiers:             deviceIDs,
		IdempotencyKey:                "stale-source-revision",
		ActorUserID:                   actorID,
		ConfirmReassignment:           true,
		ReassignmentConfirmationToken: preview.ReassignmentConfirmationToken,
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, betweenchannel.ErrReassignmentConfirmationRequired.Error())
	assert.Equal(t, before, membershipMutationCounts(t, db, orgID))
	assert.ElementsMatch(t, deviceIDs, channelMembers(t, db, orgID, source.CurrentChannelID))
}

func TestRolloutLaneCreationRejectsPreviewAfterMinerMovesSourceLanes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	sourceA := createMembershipTestLane(t, service, orgID, actorID, "Movement source A", deviceIDs)
	sourceB := createMembershipTestLane(t, service, orgID, actorID, "Movement source B", nil)
	targets := []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "m")}
	preview, err := service.PreviewLane(t.Context(), betweenchannel.PreviewLaneRequest{
		OrgID:             orgID,
		ReleaseTargets:    targets,
		DeviceIdentifiers: deviceIDs,
	})
	require.NoError(t, err)
	require.Equal(t, sourceA.ID, preview.Reassignments[0].SourceLaneID)

	moved, err := service.UpdateMembership(t.Context(), betweenchannel.UpdateMembershipRequest{
		OrgID:            orgID,
		LaneID:           sourceB.ID,
		ExpectedRevision: sourceB.Revision,
		AddIdentifiers:   deviceIDs,
		ConfirmReassign:  true,
		IdempotencyKey:   "move-between-preview-and-create",
		Reason:           "exercise stale reassignment confirmation",
		ActorUserID:      actorID,
		ActorType:        rollout.ActorTypeUser,
	})
	require.NoError(t, err)
	before := membershipMutationCounts(t, db, orgID)

	_, err = service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:                         orgID,
		Label:                         "Stale movement target",
		ReleaseTargets:                targets,
		DeviceIdentifiers:             deviceIDs,
		IdempotencyKey:                "stale-source-movement",
		ActorUserID:                   actorID,
		ConfirmReassignment:           true,
		ReassignmentConfirmationToken: preview.ReassignmentConfirmationToken,
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, betweenchannel.ErrReassignmentConfirmationRequired.Error())
	assert.Equal(t, before, membershipMutationCounts(t, db, orgID))
	assert.Empty(t, channelMembers(t, db, orgID, sourceA.CurrentChannelID))
	assert.ElementsMatch(t, deviceIDs, channelMembers(t, db, orgID, moved.Lane.CurrentChannelID))
}

func TestRolloutLaneCreationRejectsActiveSourceWorkWithoutMutation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	source := createMembershipTestLane(t, service, orgID, actorID, "Active creation source", deviceIDs)
	setLaneInitialEnforcementState(t, db, orgID, source.ID.String(), "pending")

	_, err := service.PreviewLane(t.Context(), betweenchannel.PreviewLaneRequest{
		OrgID:             orgID,
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "c")},
		DeviceIdentifiers: deviceIDs,
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, betweenchannel.ErrLaneWorkActive.Error())

	_, err = service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:                     orgID,
		Label:                     "Blocked creation target",
		ReleaseTargets:            []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "c")},
		DeviceIdentifiers:         deviceIDs,
		IdempotencyKey:            "blocked-create-reassignment",
		ActorUserID:               actorID,
		ConfirmInitialEnforcement: true,
		ConfirmReassignment:       true,
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, betweenchannel.ErrLaneWorkActive.Error())
	assert.Equal(t, []string{deviceIDs[0]}, channelMembers(t, db, orgID, source.CurrentChannelID))
}

func TestRolloutLaneCreationRollsBackSourceRemovalOnLateConflict(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	source := createMembershipTestLane(t, service, orgID, actorID, "Rollback source", deviceIDs)
	_ = createMembershipTestLane(t, service, orgID, actorID, "Duplicate target", nil)
	targets := []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "d")}
	preview, err := service.PreviewLane(t.Context(), betweenchannel.PreviewLaneRequest{
		OrgID:             orgID,
		ReleaseTargets:    targets,
		DeviceIdentifiers: deviceIDs,
	})
	require.NoError(t, err)

	_, err = service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:                         orgID,
		Label:                         "Duplicate target",
		ReleaseTargets:                targets,
		DeviceIdentifiers:             deviceIDs,
		IdempotencyKey:                "late-conflict-create",
		ActorUserID:                   actorID,
		ConfirmReassignment:           true,
		ReassignmentConfirmationToken: preview.ReassignmentConfirmationToken,
	})
	require.Error(t, err)
	assert.ElementsMatch(t, deviceIDs, channelMembers(t, db, orgID, source.CurrentChannelID))
	reloadedSource, reloadErr := service.GetLane(t.Context(), orgID, source.ID, false, nil)
	require.NoError(t, reloadErr)
	assert.Equal(t, source.Revision, reloadedSource.Revision)
}

func TestRolloutLaneConcurrentCreationReassignmentUsesCanonicalLockOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 2)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	sourceA := createMembershipTestLane(t, service, orgID, actorID, "Creation lock source A", deviceIDs[:1])
	sourceB := createMembershipTestLane(t, service, orgID, actorID, "Creation lock source B", deviceIDs[1:])

	start := make(chan struct{})
	type createResult struct {
		lane *betweenchannel.Lane
		err  error
	}
	results := make(chan createResult, 2)
	var wg sync.WaitGroup
	requests := [][]string{
		{deviceIDs[0], deviceIDs[1]},
		{deviceIDs[1], deviceIDs[0]},
	}
	tokens := make([]string, len(requests))
	for index, identifiers := range requests {
		preview, err := service.PreviewLane(t.Context(), betweenchannel.PreviewLaneRequest{
			OrgID:             orgID,
			ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", string(rune('e'+index)))},
			DeviceIdentifiers: identifiers,
		})
		require.NoError(t, err)
		tokens[index] = preview.ReassignmentConfirmationToken
	}
	for index, identifiers := range requests {
		wg.Add(1)
		go func(index int, identifiers []string) {
			defer wg.Done()
			<-start
			lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
				OrgID:                         orgID,
				Label:                         "Concurrent creation target " + string(rune('A'+index)),
				ReleaseTargets:                []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", string(rune('e'+index)))},
				DeviceIdentifiers:             identifiers,
				IdempotencyKey:                "concurrent-create-" + string(rune('a'+index)),
				ActorUserID:                   actorID,
				ConfirmReassignment:           true,
				ReassignmentConfirmationToken: tokens[index],
			})
			results <- createResult{lane: lane, err: err}
		}(index, identifiers)
	}
	close(start)
	wg.Wait()
	close(results)

	var winner *betweenchannel.Lane
	var failures int
	for result := range results {
		if result.err == nil {
			require.Nil(t, winner)
			winner = result.lane
			continue
		}
		failures++
		require.ErrorIs(t, result.err, betweenchannel.ErrMembershipConflict)
	}
	require.NotNil(t, winner)
	assert.Equal(t, 1, failures)
	assert.ElementsMatch(t, deviceIDs, channelMembers(t, db, orgID, winner.CurrentChannelID))
	for _, source := range []*betweenchannel.Lane{sourceA, sourceB} {
		reloaded, err := service.GetLane(t.Context(), orgID, source.ID, false, nil)
		require.NoError(t, err)
		assert.Equal(t, source.Revision+1, reloaded.Revision)
		assert.Zero(t, reloaded.MemberCount)
	}
}

func TestRolloutLaneMembersListHoldsRevisionSnapshotThroughPageQuery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 2)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	lane := createMembershipTestLane(t, service, orgID, actorID, "Snapshot list", deviceIDs[:1])
	candidates, err := sqlc.New(db).ListRolloutLaneMembershipCandidates(
		t.Context(),
		sqlc.ListRolloutLaneMembershipCandidatesParams{
			OrgID:             orgID,
			DeviceIdentifiers: []string{deviceIDs[1]},
		},
	)
	require.NoError(t, err)
	require.Len(t, candidates, 1)

	pageBlocker, err := db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	require.NoError(t, sqlc.New(pageBlocker).TestLockDeviceSetMembershipTable(t.Context()))

	type listResult struct {
		page betweenchannel.ListMembersResult
		err  error
	}
	listDone := make(chan listResult, 1)
	go func() {
		page, listErr := service.ListMembers(t.Context(), betweenchannel.ListMembersRequest{
			OrgID:             orgID,
			LaneID:            lane.ID,
			ExpectedRevision:  lane.Revision,
			Limit:             10,
			IncludeTotalCount: true,
		})
		listDone <- listResult{page: page, err: listErr}
	}()
	waitForLaneLock(t, db, orgID, lane.ID)

	updateDone := make(chan error, 1)
	go func() {
		tx, beginErr := db.BeginTx(t.Context(), nil)
		if beginErr != nil {
			updateDone <- beginErr
			return
		}
		q := sqlc.New(tx)
		revisions, updateErr := q.BumpRolloutLaneMembershipRevisions(
			t.Context(),
			sqlc.BumpRolloutLaneMembershipRevisionsParams{
				OrgID:   orgID,
				LaneIds: []uuid.UUID{lane.ID},
			},
		)
		if updateErr == nil && len(revisions) != 1 {
			updateErr = errors.New("membership revision bump returned an unexpected lane count")
		}
		if updateErr == nil {
			_, updateErr = q.AddRolloutLaneMembershipDevices(
				t.Context(),
				sqlc.AddRolloutLaneMembershipDevicesParams{
					DeviceIds: []int64{candidates[0].DeviceID},
					LaneID:    lane.ID,
					OrgID:     orgID,
				},
			)
		}
		if updateErr == nil {
			updateErr = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
		updateDone <- updateErr
	}()
	select {
	case updateErr := <-updateDone:
		require.FailNow(t, "concurrent membership commit bypassed the list snapshot lock", "%v", updateErr)
	case <-time.After(100 * time.Millisecond):
	}

	require.NoError(t, pageBlocker.Rollback())
	listed := <-listDone
	require.NoError(t, listed.err)
	assert.Equal(t, lane.Revision, listed.page.Revision)
	assert.Equal(t, int64(1), listed.page.TotalCount)
	require.Len(t, listed.page.Members, 1)
	require.NoError(t, <-updateDone)

	updated, err := service.ListMembers(t.Context(), betweenchannel.ListMembersRequest{
		OrgID:             orgID,
		LaneID:            lane.ID,
		ExpectedRevision:  lane.Revision + 1,
		Limit:             10,
		IncludeTotalCount: true,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), updated.TotalCount)
	require.Len(t, updated.Members, 2)
}

func TestRolloutLaneMembershipUpdateIsAtomicAuditedAndIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 5)
	q := sqlc.New(db)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	targetLane := createMembershipTestLane(t, service, orgID, actorID, "Target lane", deviceIDs[:1])
	sourceLane := createMembershipTestLane(t, service, orgID, actorID, "Source lane", deviceIDs[1:2])
	setDiscoveredFirmwareVersion(t, db, orgID, deviceIDs[2], "1.0.0")
	setDiscoveredFirmwareVersion(t, db, orgID, deviceIDs[3], "0.9.0")
	setDiscoveredFirmwareVersion(t, db, orgID, deviceIDs[4], "")

	preview, err := service.PreviewMembershipChange(
		t.Context(),
		betweenchannel.PreviewMembershipChangeRequest{
			OrgID:          orgID,
			LaneID:         targetLane.ID,
			AddIdentifiers: deviceIDs[2:5],
		},
	)
	require.NoError(t, err)
	assert.Equal(t, int32(1), preview.TargetFirmwarePreview.MatchingCount)
	assert.Equal(t, int32(1), preview.TargetFirmwarePreview.MismatchedCount)
	assert.Equal(t, int32(1), preview.TargetFirmwarePreview.UnknownCount)
	assert.True(t, preview.RequiresFirmwareConfirmation)

	addRequest := betweenchannel.UpdateMembershipRequest{
		OrgID:            orgID,
		LaneID:           targetLane.ID,
		ExpectedRevision: targetLane.Revision,
		AddIdentifiers:   deviceIDs[2:5],
		IdempotencyKey:   "membership-add",
		Reason:           "expand target lane",
		ActorUserID:      actorID,
		ActorType:        rollout.ActorTypeUser,
	}
	before := membershipMutationCounts(t, db, orgID)
	_, err = service.UpdateMembership(t.Context(), addRequest)
	require.ErrorIs(t, err, betweenchannel.ErrFirmwareConfirmationRequired)
	assert.Equal(t, before, membershipMutationCounts(t, db, orgID))

	addRequest.ConfirmFirmware = true
	added, err := service.UpdateMembership(t.Context(), addRequest)
	require.NoError(t, err)
	assert.Equal(t, int64(2), added.Lane.Revision)
	assert.Equal(t, int32(4), added.Lane.MemberCount)
	assert.Empty(t, added.Lane.FirmwareConvergence.Members)
	require.Len(t, added.TransitionMembers, 3)
	assert.Equal(t, targetLane.CurrentChannelID, deviceChannel(t, db, orgID, deviceIDs[2]))
	assert.Equal(t, targetLane.CurrentChannelID, deviceChannel(t, db, orgID, deviceIDs[3]))
	assert.Equal(t, targetLane.CurrentChannelID, deviceChannel(t, db, orgID, deviceIDs[4]))
	_, err = service.ListMembers(t.Context(), betweenchannel.ListMembersRequest{
		OrgID:            orgID,
		LaneID:           targetLane.ID,
		ExpectedRevision: targetLane.Revision,
		Limit:            1,
	})
	require.ErrorIs(t, err, betweenchannel.ErrLaneConflict)

	beforeReplay := membershipMutationCounts(t, db, orgID)
	replayed, err := service.UpdateMembership(t.Context(), addRequest)
	require.NoError(t, err)
	assert.Equal(t, added.Lane.ID, replayed.Lane.ID)
	assert.Empty(t, replayed.Lane.FirmwareConvergence.Members)
	assert.Equal(t, beforeReplay, membershipMutationCounts(t, db, orgID))

	changedFirmwareConfirmation := addRequest
	changedFirmwareConfirmation.ConfirmFirmware = false
	_, err = service.UpdateMembership(t.Context(), changedFirmwareConfirmation)
	require.ErrorIs(t, err, betweenchannel.ErrIdempotencyConflict)

	changedReassignConfirmation := addRequest
	changedReassignConfirmation.ConfirmReassign = true
	_, err = service.UpdateMembership(t.Context(), changedReassignConfirmation)
	require.ErrorIs(t, err, betweenchannel.ErrIdempotencyConflict)

	mismatch := addRequest
	mismatch.Reason = "different request"
	_, err = service.UpdateMembership(t.Context(), mismatch)
	require.ErrorIs(t, err, betweenchannel.ErrIdempotencyConflict)

	beforeRemovalFirmware := discoveredFirmwareVersion(t, db, orgID, deviceIDs[2])
	beforeRemovalCounts := membershipMutationCounts(t, db, orgID)
	removeRequest := betweenchannel.UpdateMembershipRequest{
		OrgID:             orgID,
		LaneID:            targetLane.ID,
		ExpectedRevision:  added.Lane.Revision,
		RemoveIdentifiers: []string{deviceIDs[2]},
		IdempotencyKey:    "membership-remove",
		Reason:            "stop lane management",
		ActorUserID:       actorID,
		ActorType:         rollout.ActorTypeUser,
	}
	_, err = service.UpdateMembership(t.Context(), removeRequest)
	require.ErrorIs(t, err, betweenchannel.ErrLaneWorkActive)
	assert.Equal(t, beforeRemovalCounts, membershipMutationCounts(t, db, orgID))
	_, err = q.TestSetRolloutLaneMembershipEnforcementState(
		t.Context(),
		sqlc.TestSetRolloutLaneMembershipEnforcementStateParams{
			State:              "attention_required",
			LastError:          sql.NullString{String: "injected terminal attention state", Valid: true},
			OrgID:              orgID,
			AuthorityType:      "rollout_lane_membership",
			AuthorityReference: "",
			CurrentState:       "pending",
		},
	)
	require.NoError(t, err)
	removed, err := service.UpdateMembership(t.Context(), removeRequest)
	require.NoError(t, err)
	assert.Equal(t, int64(3), removed.Lane.Revision)
	assert.Equal(t, beforeRemovalFirmware, discoveredFirmwareVersion(t, db, orgID, deviceIDs[2]))
	afterRemovalCounts := membershipMutationCounts(t, db, orgID)
	assert.Equal(t, beforeRemovalCounts.Authorities, afterRemovalCounts.Authorities)
	assert.Equal(t, beforeRemovalCounts.Enforcements, afterRemovalCounts.Enforcements)
	assert.Equal(t, beforeRemovalCounts.Changes+1, afterRemovalCounts.Changes)
	pureRemovalState, err := q.GetRolloutLaneMembershipChangeTestState(
		t.Context(),
		sqlc.GetRolloutLaneMembershipChangeTestStateParams{
			OrgID:          orgID,
			IdempotencyKey: removeRequest.IdempotencyKey,
		},
	)
	require.NoError(t, err)
	assert.False(t, pureRemovalState.HasAuthority)
	removalAudit := pureRemovalState.Applied
	assert.Contains(t, removalAudit, `"device_identifier"`)
	assert.Contains(t, removalAudit, `"source_lane_id"`)
	assert.Contains(t, removalAudit, `"source_channel_id"`)
	assert.NotContains(t, removalAudit, `"manufacturer"`)
	assert.NotContains(t, removalAudit, `"enforcement"`)
	replayedRemoval, err := service.UpdateMembership(t.Context(), removeRequest)
	require.NoError(t, err)
	assert.Empty(t, removed.TransitionMembers)
	assert.Equal(t, removed.TransitionMembers, replayedRemoval.TransitionMembers)

	credentialID := "apikey:membership-integration"
	reassignRequest := betweenchannel.UpdateMembershipRequest{
		OrgID:             orgID,
		LaneID:            targetLane.ID,
		ExpectedRevision:  removed.Lane.Revision,
		AddIdentifiers:    []string{deviceIDs[1]},
		IdempotencyKey:    "membership-reassign",
		Reason:            "move source miner",
		ActorUserID:       actorID,
		ActorType:         rollout.ActorTypeAPIKey,
		ActorCredentialID: &credentialID,
	}
	reassignPreview, err := service.PreviewMembershipChange(
		t.Context(),
		betweenchannel.PreviewMembershipChangeRequest{
			OrgID:          orgID,
			LaneID:         targetLane.ID,
			AddIdentifiers: []string{deviceIDs[1]},
		},
	)
	require.NoError(t, err)
	require.Len(t, reassignPreview.Reassignments, 1)
	assert.Equal(t, sourceLane.ID, reassignPreview.Reassignments[0].SourceLaneID)
	assert.True(t, reassignPreview.RequiresReassignConfirmation)

	_, err = service.UpdateMembership(t.Context(), reassignRequest)
	require.ErrorIs(t, err, betweenchannel.ErrReassignmentConfirmationRequired)
	reassignRequest.ConfirmReassign = true
	reassigned, err := service.UpdateMembership(t.Context(), reassignRequest)
	require.NoError(t, err)
	assert.Equal(t, targetLane.CurrentChannelID, deviceChannel(t, db, orgID, deviceIDs[1]))
	assert.Equal(t, int64(4), reassigned.Lane.Revision)
	reassignmentAudit, err := q.GetRolloutLaneMembershipChangeTestState(
		t.Context(),
		sqlc.GetRolloutLaneMembershipChangeTestStateParams{
			OrgID:          orgID,
			IdempotencyKey: reassignRequest.IdempotencyKey,
		},
	)
	require.NoError(t, err)
	var applied struct {
		Reassigned []struct {
			DeviceIdentifier      string `json:"device_identifier"`
			SourceLaneID          string `json:"source_lane_id"`
			SourceLaneLabel       string `json:"source_lane_label"`
			SourceChannelID       int64  `json:"source_channel_id"`
			SourceChannelPosition int32  `json:"source_channel_position"`
			SourceReleaseVersion  string `json:"source_release_version"`
		} `json:"reassigned"`
	}
	require.NoError(t, json.Unmarshal([]byte(reassignmentAudit.Applied), &applied))
	require.Len(t, applied.Reassigned, 1)
	assert.Equal(t, deviceIDs[1], applied.Reassigned[0].DeviceIdentifier)
	assert.Equal(t, sourceLane.ID.String(), applied.Reassigned[0].SourceLaneID)
	assert.Equal(t, sourceLane.Label, applied.Reassigned[0].SourceLaneLabel)
	assert.Equal(t, sourceLane.CurrentChannelID, applied.Reassigned[0].SourceChannelID)
	assert.Equal(t, int32(0), applied.Reassigned[0].SourceChannelPosition)
	assert.Equal(t, "1.0.0", applied.Reassigned[0].SourceReleaseVersion)

	beforeReassignmentReplay := membershipMutationCounts(t, db, orgID)
	replayedReassignment, err := service.UpdateMembership(t.Context(), reassignRequest)
	require.NoError(t, err)
	assert.Equal(t, reassigned.Lane.ID, replayedReassignment.Lane.ID)
	assert.Equal(t, beforeReassignmentReplay, membershipMutationCounts(t, db, orgID))
	sourceReloaded, err := service.GetLane(t.Context(), orgID, sourceLane.ID, false, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(2), sourceReloaded.Revision)
	assert.Zero(t, sourceReloaded.MemberCount)

	attentionRemoved, err := service.UpdateMembership(
		t.Context(),
		betweenchannel.UpdateMembershipRequest{
			OrgID:             orgID,
			LaneID:            targetLane.ID,
			ExpectedRevision:  reassigned.Lane.Revision,
			RemoveIdentifiers: []string{deviceIDs[3]},
			IdempotencyKey:    "membership-remove-attention",
			Reason:            "remove terminal attention miner",
			ActorUserID:       actorID,
			ActorType:         rollout.ActorTypeUser,
		},
	)
	require.NoError(t, err)
	assert.Equal(t, int64(5), attentionRemoved.Lane.Revision)

	backToSource, err := service.UpdateMembership(
		t.Context(),
		betweenchannel.UpdateMembershipRequest{
			OrgID:            orgID,
			LaneID:           sourceLane.ID,
			ExpectedRevision: sourceReloaded.Revision,
			AddIdentifiers:   []string{deviceIDs[1]},
			ConfirmReassign:  true,
			IdempotencyKey:   "membership-reassign-back",
			Reason:           "return miner to source",
			ActorUserID:      actorID,
			ActorType:        rollout.ActorTypeUser,
		},
	)
	require.NoError(t, err)
	assert.Equal(t, sourceLane.CurrentChannelID, deviceChannel(t, db, orgID, deviceIDs[1]))
	assert.Equal(t, int64(3), backToSource.Lane.Revision)
	targetAfterBack, err := service.GetLane(t.Context(), orgID, targetLane.ID, false, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(6), targetAfterBack.Revision)

	backToTarget, err := service.UpdateMembership(
		t.Context(),
		betweenchannel.UpdateMembershipRequest{
			OrgID:            orgID,
			LaneID:           targetLane.ID,
			ExpectedRevision: targetAfterBack.Revision,
			AddIdentifiers:   []string{deviceIDs[1]},
			ConfirmReassign:  true,
			IdempotencyKey:   "membership-reassign-forward-again",
			Reason:           "move miner forward again",
			ActorUserID:      actorID,
			ActorType:        rollout.ActorTypeUser,
		},
	)
	require.NoError(t, err)
	assert.Equal(t, int64(7), backToTarget.Lane.Revision)
	assert.Equal(t, targetLane.CurrentChannelID, deviceChannel(t, db, orgID, deviceIDs[1]))
	detailedAfterRepeatedAdd, err := service.GetLane(
		t.Context(),
		orgID,
		targetLane.ID,
		true,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, int32(3), detailedAfterRepeatedAdd.FirmwareConvergence.TotalCount)
	require.Len(t, detailedAfterRepeatedAdd.FirmwareConvergence.Members, 3)

	beforeStale := membershipMutationCounts(t, db, orgID)
	_, err = service.UpdateMembership(t.Context(), betweenchannel.UpdateMembershipRequest{
		OrgID:            orgID,
		LaneID:           targetLane.ID,
		ExpectedRevision: targetAfterBack.Revision,
		AddIdentifiers:   []string{deviceIDs[2]},
		IdempotencyKey:   "membership-stale",
		Reason:           "stale writer",
		ActorUserID:      actorID,
		ActorType:        rollout.ActorTypeUser,
	})
	require.ErrorIs(t, err, betweenchannel.ErrLaneConflict)
	assert.Equal(t, beforeStale, membershipMutationCounts(t, db, orgID))

	_, err = q.TestSoftDeleteDeviceByIdentifier(
		t.Context(),
		sqlc.TestSoftDeleteDeviceByIdentifierParams{
			OrgID:            orgID,
			DeviceIdentifier: deviceIDs[4],
		},
	)
	require.NoError(t, err)
	beforeSoftDeletedRemove := membershipMutationCounts(t, db, orgID)
	_, err = service.UpdateMembership(t.Context(), betweenchannel.UpdateMembershipRequest{
		OrgID:             orgID,
		LaneID:            targetLane.ID,
		ExpectedRevision:  backToTarget.Lane.Revision,
		RemoveIdentifiers: []string{deviceIDs[4]},
		IdempotencyKey:    "membership-soft-deleted-remove",
		Reason:            "invalid soft deleted removal",
		ActorUserID:       actorID,
		ActorType:         rollout.ActorTypeUser,
	})
	require.ErrorIs(t, err, betweenchannel.ErrMembershipConflict)
	assert.Equal(t, beforeSoftDeletedRemove, membershipMutationCounts(t, db, orgID))

	auditState, err := q.GetRolloutLaneMembershipChangeTestState(
		t.Context(),
		sqlc.GetRolloutLaneMembershipChangeTestStateParams{
			OrgID:          orgID,
			IdempotencyKey: reassignRequest.IdempotencyKey,
		},
	)
	require.NoError(t, err)
	assert.Equal(t, "api_key", auditState.ActorType)
	assert.Equal(t, actorID, auditState.ActorUserID)
	assert.Equal(t, credentialID, auditState.ActorCredentialID)
	assert.Equal(t, reassignRequest.Reason, auditState.Reason)
	assert.NotEmpty(t, auditState.AuthorityID)
	assert.Equal(t, reassignmentAudit.Applied, auditState.Applied, "later reassignments must not rewrite the source snapshot")
	_, err = q.TestMutateRolloutLaneMembershipChangeReason(
		t.Context(),
		sqlc.TestMutateRolloutLaneMembershipChangeReasonParams{
			Reason:         "tampered",
			OrgID:          orgID,
			IdempotencyKey: reassignRequest.IdempotencyKey,
		},
	)
	require.Error(t, err)
	_, err = q.TestDeleteRolloutLaneMembershipChange(
		t.Context(),
		sqlc.TestDeleteRolloutLaneMembershipChangeParams{
			OrgID:          orgID,
			IdempotencyKey: reassignRequest.IdempotencyKey,
		},
	)
	require.Error(t, err)
	require.Error(t, q.TestTruncateRolloutLaneMembershipChanges(t.Context()))

	_, err = service.ListMembers(t.Context(), betweenchannel.ListMembersRequest{
		OrgID:  orgID + 1,
		LaneID: targetLane.ID,
		Limit:  10,
	})
	require.ErrorIs(t, err, betweenchannel.ErrLaneNotFound)
}

func TestRolloutLaneMembershipPreviewRejectsMixedModelAndNonLaneChannel(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 3)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	lane := createMembershipTestLane(t, service, orgID, actorID, "Validation target", deviceIDs[:1])
	setDiscoveredModel(t, db, orgID, deviceIDs[1], "UnsupportedMiner")
	_, err := service.PreviewMembershipChange(
		t.Context(),
		betweenchannel.PreviewMembershipChangeRequest{
			OrgID:          orgID,
			LaneID:         lane.ID,
			AddIdentifiers: []string{deviceIDs[1]},
		},
	)
	require.ErrorIs(t, err, betweenchannel.ErrCompatibility)
	_, err = service.PreviewLane(t.Context(), betweenchannel.PreviewLaneRequest{
		OrgID:             orgID,
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "a")},
		DeviceIdentifiers: []string{"missing-miner"},
	})
	require.ErrorIs(t, err, betweenchannel.ErrMembershipConflict)
	_, err = service.PreviewLane(t.Context(), betweenchannel.PreviewLaneRequest{
		OrgID:             orgID,
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "b")},
		DeviceIdentifiers: []string{deviceIDs[1]},
	})
	require.ErrorIs(t, err, betweenchannel.ErrCompatibility)

	q := sqlc.New(db)
	releaseSet, err := q.CreateFirmwareReleaseSet(t.Context(), orgID)
	require.NoError(t, err)
	channelSet, err := q.CreateDeviceSet(t.Context(), sqlc.CreateDeviceSetParams{
		OrgID: orgID,
		Type:  sqlc.DeviceSetTypeChannel,
		Label: "generic-channel",
	})
	require.NoError(t, err)
	rows, err := q.CreateChannelExtension(t.Context(), sqlc.CreateChannelExtensionParams{
		ReleaseSetID: releaseSet.ID,
		DeviceSetID:  channelSet.ID,
		OrgID:        orgID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
	_, err = q.AddDevicesToDeviceSet(t.Context(), sqlc.AddDevicesToDeviceSetParams{
		OrgID:             orgID,
		DeviceSetID:       channelSet.ID,
		DeviceIdentifiers: []string{deviceIDs[2]},
	})
	require.NoError(t, err)
	_, err = service.PreviewMembershipChange(
		t.Context(),
		betweenchannel.PreviewMembershipChangeRequest{
			OrgID:          orgID,
			LaneID:         lane.ID,
			AddIdentifiers: []string{deviceIDs[2]},
		},
	)
	require.ErrorIs(t, err, betweenchannel.ErrMembershipConflict)
	_, err = service.PreviewLane(t.Context(), betweenchannel.PreviewLaneRequest{
		OrgID:             orgID,
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "c")},
		DeviceIdentifiers: []string{deviceIDs[2]},
	})
	require.ErrorIs(t, err, betweenchannel.ErrMembershipConflict)
	_, err = service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:               orgID,
		Label:               "Rejected generic reassignment",
		ReleaseTargets:      []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "c")},
		DeviceIdentifiers:   []string{deviceIDs[2]},
		IdempotencyKey:      "reject-generic-create",
		ActorUserID:         actorID,
		ConfirmReassignment: true,
	})
	require.ErrorIs(t, err, betweenchannel.ErrMembershipConflict)
	assert.Equal(t, channelSet.ID, deviceChannel(t, db, orgID, deviceIDs[2]))
}

func TestRolloutLaneConcurrentMembershipUpdatesLeaveNoPartialWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 3)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	lane := createMembershipTestLane(t, service, orgID, actorID, "Concurrent membership", deviceIDs[:1])
	before := membershipMutationCounts(t, db, orgID)

	start := make(chan struct{})
	type updateResult struct {
		result betweenchannel.UpdateMembershipResult
		err    error
	}
	results := make(chan updateResult, 2)
	var wg sync.WaitGroup
	for index, identifier := range deviceIDs[1:] {
		wg.Add(1)
		go func(index int, identifier string) {
			defer wg.Done()
			<-start
			result, err := service.UpdateMembership(
				t.Context(),
				betweenchannel.UpdateMembershipRequest{
					OrgID:            orgID,
					LaneID:           lane.ID,
					ExpectedRevision: lane.Revision,
					AddIdentifiers:   []string{identifier},
					IdempotencyKey:   "concurrent-membership-" + string(rune('a'+index)),
					Reason:           "concurrent writer",
					ActorUserID:      actorID,
					ActorType:        rollout.ActorTypeUser,
				},
			)
			results <- updateResult{result: result, err: err}
		}(index, identifier)
	}
	close(start)
	wg.Wait()
	close(results)

	var succeeded, conflicted int
	for result := range results {
		if result.err == nil {
			succeeded++
			assert.Equal(t, int64(2), result.result.Lane.Revision)
			continue
		}
		require.ErrorIs(t, result.err, betweenchannel.ErrLaneConflict)
		conflicted++
	}
	assert.Equal(t, 1, succeeded)
	assert.Equal(t, 1, conflicted)
	after := membershipMutationCounts(t, db, orgID)
	assert.Equal(t, before.Memberships+1, after.Memberships)
	assert.Equal(t, before.Authorities+1, after.Authorities)
	assert.Equal(t, before.Enforcements+1, after.Enforcements)
	assert.Equal(t, before.Changes+1, after.Changes)
	reloaded, err := service.GetLane(t.Context(), orgID, lane.ID, false, nil)
	require.NoError(t, err)
	assert.Equal(t, int32(2), reloaded.MemberCount)
	assert.Equal(t, int64(2), reloaded.Revision)
}

func TestRolloutLaneMembershipSerializesFirmwareObservationInBothOrders(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	t.Run("telemetry first is revalidated before confirmation", func(t *testing.T) {
		db, orgID, deviceIDs := setupRolloutLaneTestData(t, 2)
		actorID := testOrganizationUserID(t, db, orgID)
		service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
		lane := createMembershipTestLane(t, service, orgID, actorID, "Telemetry first", deviceIDs[:1])

		telemetryTx, err := db.BeginTx(t.Context(), nil)
		require.NoError(t, err)
		rows, err := sqlc.New(telemetryTx).TestSetDiscoveredFirmwareVersion(
			t.Context(),
			sqlc.TestSetDiscoveredFirmwareVersionParams{
				FirmwareVersion:  "0.9.0",
				OrgID:            orgID,
				DeviceIdentifier: deviceIDs[1],
			},
		)
		require.NoError(t, err)
		require.Equal(t, int64(1), rows)

		updateDone := make(chan error, 1)
		go func() {
			_, updateErr := service.UpdateMembership(
				t.Context(),
				betweenchannel.UpdateMembershipRequest{
					ChangeID:         uuid.New(),
					OrgID:            orgID,
					LaneID:           lane.ID,
					ExpectedRevision: lane.Revision,
					AddIdentifiers:   []string{deviceIDs[1]},
					IdempotencyKey:   "telemetry-first",
					Reason:           "observe committed telemetry",
					ActorUserID:      actorID,
					ActorType:        rollout.ActorTypeUser,
				},
			)
			updateDone <- updateErr
		}()
		select {
		case updateErr := <-updateDone:
			require.FailNow(t, "membership update bypassed the telemetry row lock", "%v", updateErr)
		case <-time.After(100 * time.Millisecond):
		}

		require.NoError(t, telemetryTx.Commit())
		require.ErrorIs(t, <-updateDone, betweenchannel.ErrFirmwareConfirmationRequired)
	})

	t.Run("membership first keeps one observation through enforcement creation", func(t *testing.T) {
		db, orgID, deviceIDs := setupRolloutLaneTestData(t, 2)
		actorID := testOrganizationUserID(t, db, orgID)
		service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
		lane := createMembershipTestLane(t, service, orgID, actorID, "Membership first", deviceIDs[:1])

		authorityBlocker, err := db.BeginTx(t.Context(), nil)
		require.NoError(t, err)
		require.NoError(t, sqlc.New(authorityBlocker).TestLockChannelFirmwareAuthorityTable(t.Context()))

		changeID := uuid.New()
		type membershipResult struct {
			result betweenchannel.UpdateMembershipResult
			err    error
		}
		updateDone := make(chan membershipResult, 1)
		go func() {
			result, updateErr := service.UpdateMembership(
				t.Context(),
				betweenchannel.UpdateMembershipRequest{
					ChangeID:         changeID,
					OrgID:            orgID,
					LaneID:           lane.ID,
					ExpectedRevision: lane.Revision,
					AddIdentifiers:   []string{deviceIDs[1]},
					IdempotencyKey:   "membership-first",
					Reason:           "hold observation through enforcement",
					ActorUserID:      actorID,
					ActorType:        rollout.ActorTypeUser,
				},
			)
			updateDone <- membershipResult{result: result, err: updateErr}
		}()
		waitForDeviceObservationLock(t, db, orgID, deviceIDs[1])

		telemetryDone := make(chan error, 1)
		go func() {
			rows, telemetryErr := sqlc.New(db).TestSetDiscoveredFirmwareVersion(
				t.Context(),
				sqlc.TestSetDiscoveredFirmwareVersionParams{
					FirmwareVersion:  "0.9.0",
					OrgID:            orgID,
					DeviceIdentifier: deviceIDs[1],
				},
			)
			if telemetryErr == nil && rows != 1 {
				telemetryErr = errors.New("telemetry update changed an unexpected row count")
			}
			telemetryDone <- telemetryErr
		}()
		select {
		case telemetryErr := <-telemetryDone:
			require.FailNow(t, "telemetry update bypassed the membership observation lock", "%v", telemetryErr)
		case <-time.After(100 * time.Millisecond):
		}

		require.NoError(t, authorityBlocker.Rollback())
		updated := <-updateDone
		require.NoError(t, updated.err)
		require.Len(t, updated.result.TransitionMembers, 1)
		require.NoError(t, <-telemetryDone)
		state, err := sqlc.New(db).TestGetMembershipEnforcementState(
			t.Context(),
			sqlc.TestGetMembershipEnforcementStateParams{
				OrgID:              orgID,
				AuthorityReference: changeID.String(),
				DeviceIdentifier:   deviceIDs[1],
			},
		)
		require.NoError(t, err)
		assert.Equal(t, "confirmed", state)
		assert.Equal(t, "0.9.0", discoveredFirmwareVersion(t, db, orgID, deviceIDs[1]))
	})
}

func TestRemovedTerminalMembershipEnforcementDoesNotBlockDeviceDeletion(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	for _, state := range []string{"attention_required", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			db, orgID, deviceIDs := setupRolloutLaneTestData(t, 2)
			actorID := testOrganizationUserID(t, db, orgID)
			laneService := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
			lane := createMembershipTestLane(t, laneService, orgID, actorID, "Deletion "+state, deviceIDs[:1])

			added, err := laneService.UpdateMembership(
				t.Context(),
				betweenchannel.UpdateMembershipRequest{
					OrgID:            orgID,
					LaneID:           lane.ID,
					ExpectedRevision: lane.Revision,
					AddIdentifiers:   []string{deviceIDs[1]},
					IdempotencyKey:   "add-deletion-" + state,
					Reason:           "prepare terminal membership enforcement",
					ActorUserID:      actorID,
					ActorType:        rollout.ActorTypeUser,
				},
			)
			require.NoError(t, err)
			_, err = sqlc.New(db).TestSetRolloutLaneMembershipEnforcementState(
				t.Context(),
				sqlc.TestSetRolloutLaneMembershipEnforcementStateParams{
					State:              state,
					OrgID:              orgID,
					AuthorityType:      "rollout_lane_membership",
					AuthorityReference: "",
					CurrentState:       "",
				},
			)
			require.NoError(t, err)

			removed, err := laneService.UpdateMembership(
				t.Context(),
				betweenchannel.UpdateMembershipRequest{
					OrgID:             orgID,
					LaneID:            lane.ID,
					ExpectedRevision:  added.Lane.Revision,
					RemoveIdentifiers: []string{deviceIDs[1]},
					IdempotencyKey:    "remove-deletion-" + state,
					Reason:            "stop lane management",
					ActorUserID:       actorID,
					ActorType:         rollout.ActorTypeUser,
				},
			)
			require.NoError(t, err)
			assert.Empty(t, removed.TransitionMembers)

			deleted, err := sqlstores.NewSQLDeviceStore(db).SoftDeleteDevices(
				t.Context(),
				[]string{deviceIDs[1]},
				orgID,
			)
			require.NoError(t, err)
			assert.Equal(t, int64(1), deleted)
		})
	}
}

func TestRolloutLaneConcurrentIdempotentMembershipUpdateReplays(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 2)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	lane := createMembershipTestLane(t, service, orgID, actorID, "Concurrent replay", deviceIDs[:1])
	request := betweenchannel.UpdateMembershipRequest{
		OrgID:            orgID,
		LaneID:           lane.ID,
		ExpectedRevision: lane.Revision,
		AddIdentifiers:   []string{deviceIDs[1]},
		IdempotencyKey:   "concurrent-membership-replay",
		Reason:           "same request twice",
		ActorUserID:      actorID,
		ActorType:        rollout.ActorTypeUser,
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := service.UpdateMembership(t.Context(), request)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	counts := membershipMutationCounts(t, db, orgID)
	assert.Equal(t, int64(1), counts.Changes)
	reloaded, err := service.GetLane(t.Context(), orgID, lane.ID, false, nil)
	require.NoError(t, err)
	assert.Equal(t, int32(2), reloaded.MemberCount)
	assert.Equal(t, int64(2), reloaded.Revision)
}

func TestRolloutLaneMembershipBlocksActiveTargetAndSourceWork(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 3)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	target := createMembershipTestLane(t, service, orgID, actorID, "Work target", deviceIDs[:1])
	source := createMembershipTestLane(t, service, orgID, actorID, "Work source", deviceIDs[1:2])

	setLaneInitialEnforcementState(t, db, orgID, target.ID.String(), "pending")
	before := membershipMutationCounts(t, db, orgID)
	_, err := service.UpdateMembership(t.Context(), betweenchannel.UpdateMembershipRequest{
		OrgID:            orgID,
		LaneID:           target.ID,
		ExpectedRevision: target.Revision,
		AddIdentifiers:   []string{deviceIDs[2]},
		IdempotencyKey:   "blocked-target-setup",
		Reason:           "target setup active",
		ActorUserID:      actorID,
		ActorType:        rollout.ActorTypeUser,
	})
	require.ErrorIs(t, err, betweenchannel.ErrLaneWorkActive)
	assert.Equal(t, before, membershipMutationCounts(t, db, orgID))
	setLaneInitialEnforcementState(t, db, orgID, target.ID.String(), "confirmed")

	setLaneInitialEnforcementState(t, db, orgID, source.ID.String(), "pending")
	_, err = service.UpdateMembership(t.Context(), betweenchannel.UpdateMembershipRequest{
		OrgID:            orgID,
		LaneID:           target.ID,
		ExpectedRevision: target.Revision,
		AddIdentifiers:   []string{deviceIDs[1]},
		ConfirmReassign:  true,
		IdempotencyKey:   "blocked-source-setup",
		Reason:           "source setup active",
		ActorUserID:      actorID,
		ActorType:        rollout.ActorTypeUser,
	})
	require.ErrorIs(t, err, betweenchannel.ErrLaneWorkActive)
	assert.Equal(t, before, membershipMutationCounts(t, db, orgID))
	setLaneInitialEnforcementState(t, db, orgID, source.ID.String(), "confirmed")

	_, err = service.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID:          orgID,
		LaneID:         source.ID,
		Name:           "Active source rollout",
		ReleaseTargets: []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "c")},
		Batches: []rollout.CreateBatch{{
			Label:   "all",
			Members: []rollout.CreateMember{{DeviceIdentifier: deviceIDs[1]}},
		}},
		IdempotencyKey: "active-source-rollout",
		Reason:         "exercise membership gate",
		ActorUserID:    actorID,
	})
	require.NoError(t, err)
	beforeRolloutBlocked := membershipMutationCounts(t, db, orgID)
	_, err = service.UpdateMembership(t.Context(), betweenchannel.UpdateMembershipRequest{
		OrgID:            orgID,
		LaneID:           target.ID,
		ExpectedRevision: target.Revision,
		AddIdentifiers:   []string{deviceIDs[1]},
		ConfirmReassign:  true,
		IdempotencyKey:   "blocked-source-rollout",
		Reason:           "source rollout active",
		ActorUserID:      actorID,
		ActorType:        rollout.ActorTypeUser,
	})
	require.ErrorIs(t, err, betweenchannel.ErrLaneWorkActive)
	assert.Equal(t, beforeRolloutBlocked, membershipMutationCounts(t, db, orgID))
}

func TestRolloutLaneMembershipUpdateSerializesWithFinalizer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 2)
	actorID := testOrganizationUserID(t, db, orgID)
	laneStore := sqlstores.NewSQLRolloutLaneStore(db)
	laneService := betweenchannel.NewService(laneStore, nil)
	rolloutService := rollout.NewService(
		sqlstores.NewSQLRolloutStore(db),
		betweenchannel.NewStrategy(laneStore),
	)
	lane := createMembershipTestLane(t, laneService, orgID, actorID, "Finalizer race", deviceIDs[:1])
	started, err := laneService.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID:          orgID,
		LaneID:         lane.ID,
		Name:           "Finalizer race target",
		ReleaseTargets: []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "d")},
		Batches: []rollout.CreateBatch{{
			Label:   "all",
			Members: []rollout.CreateMember{{DeviceIdentifier: deviceIDs[0]}},
		}},
		IdempotencyKey: "finalizer-race-start",
		Reason:         "exercise canonical membership locks",
		ActorUserID:    actorID,
	})
	require.NoError(t, err)
	_, err = rolloutService.Admit(t.Context(), rollout.AdmitRequest{
		OrgID:            orgID,
		RolloutID:        started.Rollout.ID,
		BatchID:          started.Rollout.Batches[0].ID,
		ExpectedRevision: started.Rollout.Revision,
		IdempotencyKey:   "finalizer-race-admit",
		Reason:           "exercise canonical membership locks",
		ActorUserID:      actorID,
	})
	require.NoError(t, err)
	admitted, err := rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	member := memberByIdentifier(t, admitted, deviceIDs[0])
	require.NotNil(t, member.EnforcementID)
	confirmEnforcement(t, db, *member.EnforcementID)
	finalizations, err := laneStore.ListFinalizations(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, finalizations, 1)
	before := membershipMutationCounts(t, db, orgID)

	start := make(chan struct{})
	finalizerErr := make(chan error, 1)
	updateErr := make(chan error, 1)
	go func() {
		<-start
		_, finalizeErr := laneStore.Finalize(t.Context(), finalizations[0])
		finalizerErr <- finalizeErr
	}()
	go func() {
		<-start
		_, membershipErr := laneService.UpdateMembership(
			t.Context(),
			betweenchannel.UpdateMembershipRequest{
				OrgID:            orgID,
				LaneID:           lane.ID,
				ExpectedRevision: lane.Revision,
				AddIdentifiers:   []string{deviceIDs[1]},
				IdempotencyKey:   "finalizer-race-membership",
				Reason:           "concurrent finalizer",
				ActorUserID:      actorID,
				ActorType:        rollout.ActorTypeUser,
			},
		)
		updateErr <- membershipErr
	}()
	close(start)

	require.NoError(t, <-finalizerErr)
	membershipErr := <-updateErr
	require.Error(t, membershipErr)
	assert.True(
		t,
		errors.Is(membershipErr, betweenchannel.ErrLaneWorkActive) ||
			errors.Is(membershipErr, betweenchannel.ErrLaneConflict),
		"got %v",
		membershipErr,
	)
	after := membershipMutationCounts(t, db, orgID)
	assert.Equal(t, before.Changes, after.Changes)
	assert.Equal(t, before.Memberships, after.Memberships)
	addedMemberships, err := sqlc.New(db).CountDeviceChannelMembershipsForTest(
		t.Context(),
		sqlc.CountDeviceChannelMembershipsForTestParams{
			OrgID:            orgID,
			DeviceIdentifier: deviceIDs[1],
		},
	)
	require.NoError(t, err)
	assert.Zero(t, addedMemberships)
}

func TestRolloutLaneMembershipMigrationDownSucceedsWhenAuditIsEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	downSQL, err := migrations.Migrations.ReadFile("000149_rollout_lane_membership_changes.down.sql")
	require.NoError(t, err)

	_, err = db.ExecContext(t.Context(), string(downSQL))
	require.NoError(t, err)
	var tableExists bool
	require.NoError(
		t,
		db.QueryRowContext(
			t.Context(),
			"SELECT to_regclass('rollout_lane_membership_change') IS NOT NULL",
		).Scan(&tableExists),
	)
	assert.False(t, tableExists)
}

func TestRolloutLaneMembershipMigrationDownRefusesWithAuditRows(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 2)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	lane := createMembershipTestLane(t, service, orgID, actorID, "Rollback guard", deviceIDs[:1])
	request := betweenchannel.UpdateMembershipRequest{
		OrgID:            orgID,
		LaneID:           lane.ID,
		ExpectedRevision: lane.Revision,
		AddIdentifiers:   []string{deviceIDs[1]},
		IdempotencyKey:   "rollback-guard-audit",
		Reason:           "create immutable audit history",
		ActorUserID:      actorID,
		ActorType:        rollout.ActorTypeUser,
	}
	_, err := service.UpdateMembership(t.Context(), request)
	require.NoError(t, err)
	q := sqlc.New(db)
	before, err := q.GetRolloutLaneMembershipChangeByIdempotencyKey(
		t.Context(),
		sqlc.GetRolloutLaneMembershipChangeByIdempotencyKeyParams{
			OrgID:          orgID,
			IdempotencyKey: request.IdempotencyKey,
		},
	)
	require.NoError(t, err)

	downSQL, err := migrations.Migrations.ReadFile("000149_rollout_lane_membership_changes.down.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), string(downSQL))
	require.ErrorContains(t, err, "rollout lane membership audit records exist")

	after, err := q.GetRolloutLaneMembershipChangeByIdempotencyKey(
		t.Context(),
		sqlc.GetRolloutLaneMembershipChangeByIdempotencyKeyParams{
			OrgID:          orgID,
			IdempotencyKey: request.IdempotencyKey,
		},
	)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func createMembershipTestLane(
	t *testing.T,
	service *betweenchannel.Service,
	orgID int64,
	actorID int64,
	label string,
	deviceIdentifiers []string,
) *betweenchannel.Lane {
	t.Helper()
	lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             label,
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "a")},
		DeviceIdentifiers: deviceIdentifiers,
		IdempotencyKey:    "create-" + strings.ReplaceAll(strings.ToLower(label), " ", "-"),
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	return lane
}

func setLaneInitialEnforcementState(
	t *testing.T,
	db *sql.DB,
	orgID int64,
	laneID string,
	state string,
) {
	t.Helper()
	_, err := sqlc.New(db).TestSetRolloutLaneMembershipEnforcementState(
		t.Context(),
		sqlc.TestSetRolloutLaneMembershipEnforcementStateParams{
			State:              state,
			OrgID:              orgID,
			AuthorityType:      "rollout_lane_initial",
			AuthorityReference: laneID,
			CurrentState:       "",
		},
	)
	require.NoError(t, err)
}

type membershipCounts struct {
	Memberships  int64
	Authorities  int64
	Enforcements int64
	Changes      int64
}

func membershipMutationCounts(t *testing.T, db *sql.DB, orgID int64) membershipCounts {
	t.Helper()
	row, err := sqlc.New(db).GetRolloutLaneMembershipMutationCountsForTest(t.Context(), orgID)
	require.NoError(t, err)
	return membershipCounts{
		Memberships:  row.Memberships,
		Authorities:  row.Authorities,
		Enforcements: row.Enforcements,
		Changes:      row.Changes,
	}
}

func discoveredFirmwareVersion(
	t *testing.T,
	db *sql.DB,
	orgID int64,
	deviceIdentifier string,
) string {
	t.Helper()
	version, err := sqlc.New(db).GetDiscoveredFirmwareVersionForTest(
		t.Context(),
		sqlc.GetDiscoveredFirmwareVersionForTestParams{
			OrgID:            orgID,
			DeviceIdentifier: deviceIdentifier,
		},
	)
	require.NoError(t, err)
	return version.String
}

func waitForLaneLock(t *testing.T, db *sql.DB, orgID int64, laneID uuid.UUID) {
	t.Helper()
	q := sqlc.New(db)
	require.Eventually(t, func() bool {
		_, err := q.TestLockRolloutLaneNowait(
			t.Context(),
			sqlc.TestLockRolloutLaneNowaitParams{
				LaneID: laneID,
				OrgID:  orgID,
			},
		)
		return err != nil
	}, 5*time.Second, 10*time.Millisecond)
}

func waitForDeviceObservationLock(
	t *testing.T,
	db *sql.DB,
	orgID int64,
	deviceIdentifier string,
) {
	t.Helper()
	q := sqlc.New(db)
	require.Eventually(t, func() bool {
		_, err := q.TestLockMembershipDeviceObservationNowait(
			t.Context(),
			sqlc.TestLockMembershipDeviceObservationNowaitParams{
				OrgID:            orgID,
				DeviceIdentifier: deviceIdentifier,
			},
		)
		return err != nil
	}, 5*time.Second, 10*time.Millisecond)
}
