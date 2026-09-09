package rollout

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListRolloutsPollCursorIncludesLateCommits(t *testing.T) {
	f := newFixture(t, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	f.channel(t, allAtOnce, "miner-0")
	first := f.apply(t, "fw-1")
	second := f.apply(t, "fw-2")

	// A writes first but remains invisible while B writes and commits.
	// Their different rollout rows allow these transactions to overlap.
	writer, err := f.conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = writer.Rollback() }()
	var firstRevision, firstTxid int64
	var firstUpdatedAt time.Time
	require.NoError(t, writer.QueryRowContext(ctx, `
		UPDATE firmware_rollout SET last_action_by_name = 'late commit'
		WHERE id = $1 RETURNING revision, revision_txid, updated_at
	`, first.ID).Scan(&firstRevision, &firstTxid, &firstUpdatedAt))
	var secondUpdatedAt time.Time
	require.NoError(t, f.conn.QueryRowContext(ctx, `
		UPDATE firmware_rollout SET last_action_by_name = 'early commit'
		WHERE id = $1 RETURNING updated_at
	`, second.ID).Scan(&secondUpdatedAt))
	require.True(t, secondUpdatedAt.After(firstUpdatedAt))

	initial, pageCursor, pollCursor, err := f.svc.ListRollouts(ctx, f.orgID, RolloutFilter{})
	require.NoError(t, err)
	require.Empty(t, pageCursor)
	require.NotEmpty(t, pollCursor)
	require.Len(t, initial, 2)
	assert.Equal(t, "early commit", initial[0].LastActionBy.Name)
	assert.Less(t, initial[1].Revision, firstRevision, "A is still invisible")
	parts, err := decodeCursor(pollCursor, 1)
	require.NoError(t, err)
	xmin, err := strconv.ParseInt(parts[0], 10, 64)
	require.NoError(t, err)
	assert.LessOrEqual(t, xmin, firstTxid, "the boundary must retain invisible writers")
	require.NoError(t, writer.Commit())

	// A timestamp filter misses A permanently; the server's poll cursor
	// must include its revision once it commits, despite its earlier time.
	byTime, _, _, err := f.svc.ListRollouts(ctx, f.orgID, RolloutFilter{UpdatedAfter: &secondUpdatedAt})
	require.NoError(t, err)
	require.Len(t, byTime, 1)
	assert.Equal(t, second.ID, byTime[0].ID)
	changed, _, nextPoll, err := f.svc.ListRollouts(ctx, f.orgID, RolloutFilter{PollCursor: pollCursor})
	require.NoError(t, err)
	require.NotEmpty(t, nextPoll)
	var found *Rollout
	for i := range changed {
		if changed[i].ID == first.ID {
			found = &changed[i]
		}
	}
	require.NotNil(t, found, "the late commit must be returned by the next cycle")
	assert.Equal(t, firstRevision, found.Revision)
	assert.Equal(t, "late commit", found.LastActionBy.Name)
}

func TestListRolloutsPollCursorStaysFixedAcrossPages(t *testing.T) {
	f := newFixture(t, 1)
	ctx := t.Context()
	f.channel(t, allAtOnce, "miner-0")
	first := f.apply(t, "fw-1")
	second := f.apply(t, "fw-2")
	third := f.apply(t, "fw-1")
	_, _, priorPoll, err := f.svc.ListRollouts(ctx, f.orgID, RolloutFilter{})
	require.NoError(t, err)

	// Change every row for an incremental cycle with three pages.
	_, err = f.conn.ExecContext(ctx, `UPDATE firmware_rollout SET last_action_by_name = 'before pages' WHERE org_id = $1`, f.orgID)
	require.NoError(t, err)
	page, cursor, nextPoll, err := f.svc.ListRollouts(ctx, f.orgID, RolloutFilter{PollCursor: priorPoll, PageSize: 1})
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.NotEmpty(t, cursor)
	require.NotEmpty(t, nextPoll)
	assert.Equal(t, third.ID, page[0].ID)

	// Change the already-read row, then a row that a later page will read.
	// The final page must not advance the boundary past the missed change.
	_, err = f.conn.ExecContext(ctx, `UPDATE firmware_rollout SET last_action_by_name = 'after first page' WHERE id = $1`, third.ID)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `UPDATE firmware_rollout SET last_action_by_name = 'later page' WHERE id = $1`, first.ID)
	require.NoError(t, err)
	for _, expectedID := range []int64{second.ID, first.ID} {
		var pagePoll string
		page, cursor, pagePoll, err = f.svc.ListRollouts(ctx, f.orgID, RolloutFilter{PollCursor: priorPoll, PageSize: 1, Cursor: cursor})
		require.NoError(t, err)
		require.Len(t, page, 1)
		assert.Equal(t, expectedID, page[0].ID)
		assert.Equal(t, nextPoll, pagePoll, "all pages carry the first page's poll boundary")
	}
	require.Empty(t, cursor)

	changed, _, _, err := f.svc.ListRollouts(ctx, f.orgID, RolloutFilter{PollCursor: nextPoll})
	require.NoError(t, err)
	var found *Rollout
	for i := range changed {
		if changed[i].ID == third.ID {
			found = &changed[i]
		}
	}
	require.NotNil(t, found, "updates to an earlier page must reappear in the next cycle")
	assert.Equal(t, "after first page", found.LastActionBy.Name)
}

func TestListRolloutsRejectsInvalidPollCursors(t *testing.T) {
	// Invalid inputs fail before any store access, even for direct domain
	// callers that do not pass through request validation.
	svc := &Service{}
	for _, cursor := range []string{"not a cursor", encodeCursor("0"), encodeCursor("-1"), encodeCursor("9223372036854775808"), encodeCursor("1", "2")} {
		_, _, _, err := svc.ListRollouts(t.Context(), 1, RolloutFilter{PollCursor: cursor})
		assert.ErrorContains(t, err, "cursor")
	}
	for _, cursor := range []string{encodeCursor("1", "2"), encodeCursor("1", "2", "0"), encodeCursor("1", "2", "9223372036854775808")} {
		_, _, _, err := svc.ListRollouts(t.Context(), 1, RolloutFilter{Cursor: cursor})
		assert.ErrorContains(t, err, "cursor")
	}
	after := time.Now()
	_, _, _, err := svc.ListRollouts(t.Context(), 1, RolloutFilter{UpdatedAfter: &after, PollCursor: encodeCursor("1")})
	assert.ErrorContains(t, err, "updated_after cannot be combined with poll_cursor")
}
