package notes

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	pb "github.com/block/proto-fleet/server/generated/grpc/notes/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/notes"
	"github.com/block/proto-fleet/server/internal/domain/notes/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/handlers/handlerstest"
)

// testHarness wires a real *notes.Service against a mock store so
// handler tests exercise both the auth gate and the body. activitySvc
// is nil; Log is nil-safe so audit fire-and-forget no-ops in tests.
type testHarness struct {
	handler *Handler
	store   *mocks.MockNoteStore
}

func newTestHandler(t *testing.T) *testHarness {
	t.Helper()
	ctrl := gomock.NewController(t)
	store := mocks.NewMockNoteStore(ctrl)
	return &testHarness{
		handler: NewHandler(notes.NewService(store, nil)),
		store:   store,
	}
}

// authorCtx builds a caller with note:read + note:create at org scope
// and the identity fields the handler stamps authorship from.
func authorCtx(t *testing.T, orgID, userID int64, username string, extraPerms ...string) context.Context {
	t.Helper()
	perms := append([]string{authz.PermNoteRead, authz.PermNoteCreate}, extraPerms...)
	return handlerstest.CtxWithSessionInfo(t,
		&session.Info{OrganizationID: orgID, UserID: userID, Username: username},
		authz.Assignment{AssignmentID: 1, ScopeType: authz.ScopeOrg, Permissions: perms},
	)
}

// siteScopedAuthorCtx is authorCtx with the grant attached to a single
// site-scope assignment — the FIELD_TECH@Site-A shape the *Anywhere
// gates exist for.
func siteScopedAuthorCtx(t *testing.T, orgID, siteID, userID int64, username string, perms ...string) context.Context {
	t.Helper()
	return handlerstest.CtxWithSessionInfo(t,
		&session.Info{OrganizationID: orgID, UserID: userID, Username: username},
		authz.Assignment{AssignmentID: 1, ScopeType: authz.ScopeSite, SiteID: &siteID, Permissions: perms},
	)
}

func requireCode(t *testing.T, err error, want connect.Code) {
	t.Helper()
	require.Error(t, err)
	var fe fleeterror.FleetError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, want, fe.GRPCCode)
}

func TestHandler_authGate(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil) // gate rejects before the body can touch the nil service

	cases := []struct {
		name string
		call func(ctx context.Context) error
	}{
		{"ListNotes", func(ctx context.Context) error {
			_, err := h.ListNotes(ctx, connect.NewRequest(&pb.ListNotesRequest{PageSize: 10}))
			return err
		}},
		{"CreateNote", func(ctx context.Context) error {
			_, err := h.CreateNote(ctx, connect.NewRequest(&pb.CreateNoteRequest{Content: "hi"}))
			return err
		}},
		{"UpdateNote", func(ctx context.Context) error {
			_, err := h.UpdateNote(ctx, connect.NewRequest(&pb.UpdateNoteRequest{Id: 1, Content: "hi"}))
			return err
		}},
		{"DeleteNote", func(ctx context.Context) error {
			_, err := h.DeleteNote(ctx, connect.NewRequest(&pb.DeleteNoteRequest{Id: 1}))
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name+" rejects caller without note permissions", func(t *testing.T) {
			t.Parallel()
			ctx := handlerstest.CtxWithPermissions(t, 1, authz.PermFleetRead)
			requireCode(t, tc.call(ctx), connect.CodePermissionDenied)
		})
		t.Run(tc.name+" rejects unauthenticated caller", func(t *testing.T) {
			t.Parallel()
			requireCode(t, tc.call(context.Background()), connect.CodeUnauthenticated)
		})
	}
}

func TestHandler_readOnlyRoleCannotPost(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	ctx := handlerstest.CtxWithPermissions(t, 1, authz.PermNoteRead)

	h.store.EXPECT().ListNotes(gomock.Any(), gomock.Any()).Return(nil, nil)
	_, err := h.handler.ListNotes(ctx, connect.NewRequest(&pb.ListNotesRequest{PageSize: 10}))
	require.NoError(t, err, "note:read alone must satisfy ListNotes")

	_, err = h.handler.CreateNote(ctx, connect.NewRequest(&pb.CreateNoteRequest{Content: "hi"}))
	requireCode(t, err, connect.CodePermissionDenied)
}

func TestHandler_siteScopedCallerPassesAnywhereGates(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	// A caller whose ONLY assignment is site-scoped — the regression
	// the *Anywhere gates exist for. Has() with an org resource would
	// deny this caller; the notepad must not.
	ctx := siteScopedAuthorCtx(t, 1, 42, 7, "tech",
		authz.PermNoteRead, authz.PermNoteCreate)

	h.store.EXPECT().ListNotes(gomock.Any(), gomock.Any()).Return(nil, nil)
	_, err := h.handler.ListNotes(ctx, connect.NewRequest(&pb.ListNotesRequest{PageSize: 10}))
	require.NoError(t, err)

	h.store.EXPECT().CreateNote(gomock.Any(), int64(1), int64(7), "hello from the field").
		Return(&models.Note{ID: 5, OrgID: 1, UserID: 7, Content: "hello from the field"}, nil)
	resp, err := h.handler.CreateNote(ctx, connect.NewRequest(&pb.CreateNoteRequest{Content: "hello from the field"}))
	require.NoError(t, err)
	require.Equal(t, "tech", resp.Msg.GetNote().GetAuthorUsername(),
		"author username is stamped from the session")
}

func TestHandler_createTrimsAndRejectsWhitespaceContent(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)
	ctx := authorCtx(t, 1, 7, "alice")

	_, err := h.handler.CreateNote(ctx, connect.NewRequest(&pb.CreateNoteRequest{Content: "   \n\t  "}))
	requireCode(t, err, connect.CodeInvalidArgument)

	h.store.EXPECT().CreateNote(gomock.Any(), int64(1), int64(7), "trimmed").
		Return(&models.Note{ID: 1, OrgID: 1, UserID: 7, Content: "trimmed"}, nil)
	resp, err := h.handler.CreateNote(ctx, connect.NewRequest(&pb.CreateNoteRequest{Content: "  trimmed \n"}))
	require.NoError(t, err)
	require.Equal(t, "trimmed", resp.Msg.GetNote().GetContent())
}

func TestHandler_updateIsAuthorOnly(t *testing.T) {
	t.Parallel()

	t.Run("author edits own note", func(t *testing.T) {
		t.Parallel()
		h := newTestHandler(t)
		ctx := authorCtx(t, 1, 7, "alice")

		h.store.EXPECT().GetNote(gomock.Any(), int64(1), int64(5)).
			Return(&models.Note{ID: 5, OrgID: 1, UserID: 7, Content: "old"}, nil)
		h.store.EXPECT().UpdateNoteContent(gomock.Any(), int64(1), int64(5), int64(7), "new").
			Return(&models.Note{ID: 5, OrgID: 1, UserID: 7, Content: "new"}, nil)

		resp, err := h.handler.UpdateNote(ctx, connect.NewRequest(&pb.UpdateNoteRequest{Id: 5, Content: "new"}))
		require.NoError(t, err)
		require.Equal(t, "new", resp.Msg.GetNote().GetContent())
		require.Equal(t, "alice", resp.Msg.GetNote().GetAuthorUsername())
	})

	t.Run("non-author is rejected even with note:manage", func(t *testing.T) {
		t.Parallel()
		h := newTestHandler(t)
		// Moderator holds note:manage — moderation covers deletion
		// only, never editing another author's words.
		ctx := authorCtx(t, 1, 8, "mallory", authz.PermNoteManage)

		h.store.EXPECT().GetNote(gomock.Any(), int64(1), int64(5)).
			Return(&models.Note{ID: 5, OrgID: 1, UserID: 7, Content: "old"}, nil)

		_, err := h.handler.UpdateNote(ctx, connect.NewRequest(&pb.UpdateNoteRequest{Id: 5, Content: "new"}))
		requireCode(t, err, connect.CodePermissionDenied)
	})

	t.Run("missing note is NotFound", func(t *testing.T) {
		t.Parallel()
		h := newTestHandler(t)
		ctx := authorCtx(t, 1, 7, "alice")

		h.store.EXPECT().GetNote(gomock.Any(), int64(1), int64(99)).
			Return(nil, fleeterror.NewNotFoundErrorf("note %d not found", 99))

		_, err := h.handler.UpdateNote(ctx, connect.NewRequest(&pb.UpdateNoteRequest{Id: 99, Content: "new"}))
		requireCode(t, err, connect.CodeNotFound)
	})
}

func TestHandler_deleteAuthorOrModerator(t *testing.T) {
	t.Parallel()

	someoneElsesNote := &models.Note{ID: 5, OrgID: 1, UserID: 7, Content: "x"}

	t.Run("author deletes own note", func(t *testing.T) {
		t.Parallel()
		h := newTestHandler(t)
		ctx := authorCtx(t, 1, 7, "alice")

		h.store.EXPECT().GetNote(gomock.Any(), int64(1), int64(5)).Return(someoneElsesNote, nil)
		h.store.EXPECT().SoftDeleteNote(gomock.Any(), int64(1), int64(5)).Return(nil)

		_, err := h.handler.DeleteNote(ctx, connect.NewRequest(&pb.DeleteNoteRequest{Id: 5}))
		require.NoError(t, err)
	})

	t.Run("non-author without note:manage is rejected", func(t *testing.T) {
		t.Parallel()
		h := newTestHandler(t)
		ctx := authorCtx(t, 1, 8, "bob")

		h.store.EXPECT().GetNote(gomock.Any(), int64(1), int64(5)).Return(someoneElsesNote, nil)

		_, err := h.handler.DeleteNote(ctx, connect.NewRequest(&pb.DeleteNoteRequest{Id: 5}))
		requireCode(t, err, connect.CodePermissionDenied)
	})

	t.Run("site-scoped note:manage moderates any note", func(t *testing.T) {
		t.Parallel()
		h := newTestHandler(t)
		// Moderator-only role, site-scoped: no note:create at all, so
		// the delete gate's second key and the capability probe both
		// have to resolve through HasAnywhere.
		ctx := siteScopedAuthorCtx(t, 1, 42, 8, "mod",
			authz.PermNoteRead, authz.PermNoteManage)

		h.store.EXPECT().GetNote(gomock.Any(), int64(1), int64(5)).Return(someoneElsesNote, nil)
		h.store.EXPECT().SoftDeleteNote(gomock.Any(), int64(1), int64(5)).Return(nil)

		_, err := h.handler.DeleteNote(ctx, connect.NewRequest(&pb.DeleteNoteRequest{Id: 5}))
		require.NoError(t, err)
	})
}

func TestHandler_listPagination(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	mkNote := func(id int64, at time.Time) models.Note {
		return models.Note{ID: id, OrgID: 1, UserID: 7, AuthorUsername: "alice", Content: "n", CreatedAt: at, UpdatedAt: at}
	}

	t.Run("full page emits a round-trippable next token", func(t *testing.T) {
		t.Parallel()
		h := newTestHandler(t)
		ctx := authorCtx(t, 1, 7, "alice")

		page1 := []models.Note{mkNote(3, base.Add(2*time.Minute)), mkNote(2, base.Add(time.Minute))}
		h.store.EXPECT().ListNotes(gomock.Any(), models.ListNotesParams{OrgID: 1, PageSize: 2}).Return(page1, nil)

		resp, err := h.handler.ListNotes(ctx, connect.NewRequest(&pb.ListNotesRequest{PageSize: 2}))
		require.NoError(t, err)
		require.Len(t, resp.Msg.GetNotes(), 2)
		token := resp.Msg.GetNextPageToken()
		require.NotEmpty(t, token, "full page must carry a continuation token")

		// The token round-trips into cursor params for the next page.
		wantTime := base.Add(time.Minute)
		wantID := int64(2)
		h.store.EXPECT().ListNotes(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, params models.ListNotesParams) ([]models.Note, error) {
				require.NotNil(t, params.CursorTime)
				require.NotNil(t, params.CursorID)
				require.True(t, params.CursorTime.Equal(wantTime))
				require.Equal(t, wantID, *params.CursorID)
				return []models.Note{mkNote(1, base)}, nil
			})

		resp2, err := h.handler.ListNotes(ctx, connect.NewRequest(&pb.ListNotesRequest{PageSize: 2, PageToken: token}))
		require.NoError(t, err)
		require.Len(t, resp2.Msg.GetNotes(), 1)
		require.Empty(t, resp2.Msg.GetNextPageToken(), "short page means the feed is exhausted")
	})

	t.Run("invalid tokens are InvalidArgument", func(t *testing.T) {
		t.Parallel()
		h := newTestHandler(t)
		ctx := authorCtx(t, 1, 7, "alice")

		badJSON := base64.URLEncoding.EncodeToString([]byte("{not json"))
		zeroTime := base64.URLEncoding.EncodeToString([]byte(`{"created_at":"0001-01-01T00:00:00Z","id":3}`))
		badID := base64.URLEncoding.EncodeToString([]byte(`{"created_at":"2026-06-01T12:00:00Z","id":0}`))

		for name, token := range map[string]string{
			"not base64":      "%%%not-base64%%%",
			"not json":        badJSON,
			"zero created_at": zeroTime,
			"non-positive id": badID,
		} {
			_, err := h.handler.ListNotes(ctx, connect.NewRequest(&pb.ListNotesRequest{PageSize: 2, PageToken: token}))
			requireCode(t, err, connect.CodeInvalidArgument)
			require.Contains(t, connectDebug(t, err), "invalid page token", "case %s", name)
		}
	})
}

func connectDebug(t *testing.T, err error) string {
	t.Helper()
	var fe fleeterror.FleetError
	require.ErrorAs(t, err, &fe)
	return fe.DebugMessage
}
