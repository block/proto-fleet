package notes

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/notes/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

// newService wires the domain service against a mock note store and a
// mock-backed activity service that records every event for
// assertion. Pass withActivity=false to exercise the nil-activity
// path (handler test harnesses rely on it being a no-op).
func newService(t *testing.T, withActivity bool) (*Service, *mocks.MockNoteStore, *[]activitymodels.Event) {
	t.Helper()
	ctrl := gomock.NewController(t)
	store := mocks.NewMockNoteStore(ctrl)

	events := &[]activitymodels.Event{}
	var activitySvc *activity.Service
	if withActivity {
		activityStore := mocks.NewMockActivityStore(ctrl)
		activityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
			func(_ any, e *activitymodels.Event) error {
				*events = append(*events, *e)
				return nil
			})
		activitySvc = activity.NewService(activityStore)
	}
	return NewService(store, activitySvc), store, events
}

func TestService_CreateNote_NormalizesContentAndLogs(t *testing.T) {
	t.Parallel()
	svc, store, events := newService(t, true)

	store.EXPECT().CreateNote(gomock.Any(), int64(1), int64(7), "hello").
		Return(&models.Note{ID: 3, OrgID: 1, UserID: 7, Content: "hello"}, nil)

	note, err := svc.CreateNote(t.Context(), 1, 7, "alice", "  hello \n")
	require.NoError(t, err)
	require.Equal(t, "hello", note.Content, "stored value is the trimmed string")
	require.Equal(t, "alice", note.AuthorUsername, "username stamped for immediate display")

	require.Len(t, *events, 1)
	got := (*events)[0]
	require.Equal(t, activitymodels.CategoryNote, got.Category)
	require.Equal(t, "note.created", got.Type)
	require.NotContains(t, got.Description, "hello",
		"note content must not leak into the audit row")
	require.Equal(t, int64(3), got.Metadata["note_id"])
}

func TestService_ContentValidationBoundaries(t *testing.T) {
	t.Parallel()
	svc, store, _ := newService(t, false)

	_, err := svc.CreateNote(t.Context(), 1, 7, "alice", "   \t\n ")
	requireCodeIs(t, err, fleeterror.NewInvalidArgumentError("").GRPCCode)

	// Exactly MaxContentRunes is accepted (multibyte runes count as one).
	atCap := strings.Repeat("ü", models.MaxContentRunes)
	store.EXPECT().CreateNote(gomock.Any(), int64(1), int64(7), atCap).
		Return(&models.Note{ID: 1, OrgID: 1, UserID: 7, Content: atCap}, nil)
	_, err = svc.CreateNote(t.Context(), 1, 7, "alice", atCap)
	require.NoError(t, err)

	// One rune over is rejected before the store is touched.
	_, err = svc.CreateNote(t.Context(), 1, 7, "alice", atCap+"x")
	requireCodeIs(t, err, fleeterror.NewInvalidArgumentError("").GRPCCode)
}

func TestService_UpdateNote_AuthorOnly(t *testing.T) {
	t.Parallel()
	svc, store, events := newService(t, true)

	// Non-author: Forbidden, no write, no audit row.
	store.EXPECT().GetNote(gomock.Any(), int64(1), int64(5)).
		Return(&models.Note{ID: 5, OrgID: 1, UserID: 7}, nil)
	_, err := svc.UpdateNote(t.Context(), 1, 5, 8, "mallory", "rewrite")
	requireCodeIs(t, err, fleeterror.NewForbiddenError("").GRPCCode)
	require.Empty(t, *events)

	// Author: update lands and logs note.updated.
	store.EXPECT().GetNote(gomock.Any(), int64(1), int64(5)).
		Return(&models.Note{ID: 5, OrgID: 1, UserID: 7}, nil)
	store.EXPECT().UpdateNoteContent(gomock.Any(), int64(1), int64(5), int64(7), "better").
		Return(&models.Note{ID: 5, OrgID: 1, UserID: 7, Content: "better"}, nil)
	note, err := svc.UpdateNote(t.Context(), 1, 5, 7, "alice", " better ")
	require.NoError(t, err)
	require.Equal(t, "better", note.Content)
	require.Len(t, *events, 1)
	require.Equal(t, "note.updated", (*events)[0].Type)
}

func TestService_DeleteNote_AuthorOrModerator(t *testing.T) {
	t.Parallel()

	t.Run("author deletes own; moderated=false", func(t *testing.T) {
		t.Parallel()
		svc, store, events := newService(t, true)
		store.EXPECT().GetNote(gomock.Any(), int64(1), int64(5)).
			Return(&models.Note{ID: 5, OrgID: 1, UserID: 7}, nil)
		store.EXPECT().SoftDeleteNote(gomock.Any(), int64(1), int64(5)).Return(nil)

		require.NoError(t, svc.DeleteNote(t.Context(), 1, 5, 7, false))
		require.Len(t, *events, 1)
		got := (*events)[0]
		require.Equal(t, "note.deleted", got.Type)
		require.Equal(t, false, got.Metadata["moderated"])
		require.Equal(t, int64(7), got.Metadata["author_user_id"])
	})

	t.Run("non-author without moderation is Forbidden", func(t *testing.T) {
		t.Parallel()
		svc, store, _ := newService(t, false)
		store.EXPECT().GetNote(gomock.Any(), int64(1), int64(5)).
			Return(&models.Note{ID: 5, OrgID: 1, UserID: 7}, nil)

		err := svc.DeleteNote(t.Context(), 1, 5, 8, false)
		requireCodeIs(t, err, fleeterror.NewForbiddenError("").GRPCCode)
	})

	t.Run("moderator deletes another author's note; moderated=true", func(t *testing.T) {
		t.Parallel()
		svc, store, events := newService(t, true)
		store.EXPECT().GetNote(gomock.Any(), int64(1), int64(5)).
			Return(&models.Note{ID: 5, OrgID: 1, UserID: 7}, nil)
		store.EXPECT().SoftDeleteNote(gomock.Any(), int64(1), int64(5)).Return(nil)

		require.NoError(t, svc.DeleteNote(t.Context(), 1, 5, 8, true))
		require.Len(t, *events, 1)
		require.Equal(t, true, (*events)[0].Metadata["moderated"])
	})
}

func TestService_ListNotes_ClampsPageSize(t *testing.T) {
	t.Parallel()
	svc, store, _ := newService(t, false)

	store.EXPECT().ListNotes(gomock.Any(), models.ListNotesParams{OrgID: 1, PageSize: models.DefaultPageSize}).Return(nil, nil)
	_, err := svc.ListNotes(t.Context(), models.ListNotesParams{OrgID: 1})
	require.NoError(t, err)

	store.EXPECT().ListNotes(gomock.Any(), models.ListNotesParams{OrgID: 1, PageSize: models.MaxPageSize}).Return(nil, nil)
	_, err = svc.ListNotes(t.Context(), models.ListNotesParams{OrgID: 1, PageSize: models.MaxPageSize + 50})
	require.NoError(t, err)
}

func TestService_NilActivityServiceIsNoOp(t *testing.T) {
	t.Parallel()
	svc, store, _ := newService(t, false)

	store.EXPECT().CreateNote(gomock.Any(), int64(1), int64(7), "hi").
		Return(&models.Note{ID: 1, OrgID: 1, UserID: 7, Content: "hi"}, nil)

	// Must not panic despite activitySvc == nil.
	_, err := svc.CreateNote(t.Context(), 1, 7, "alice", "hi")
	require.NoError(t, err)
}

func requireCodeIs(t *testing.T, err error, want any) {
	t.Helper()
	require.Error(t, err)
	var fe fleeterror.FleetError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, want, fe.GRPCCode)
}
