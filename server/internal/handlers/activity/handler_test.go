package activity

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	pb "github.com/block/proto-fleet/server/generated/grpc/activity/v1"
	"github.com/block/proto-fleet/server/generated/grpc/activity/v1/activityv1connect"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	"github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
)

const testOrgID = int64(42)

var testTime = time.Date(2026, 3, 23, 21, 30, 0, 0, time.UTC)

func authedCtx() context.Context {
	return authn.SetInfo(context.Background(), &session.Info{
		SessionID:      "sess-1",
		UserID:         1,
		OrganizationID: testOrgID,
		ExternalUserID: "usr_abc",
		Username:       "admin",
	})
}

func newTestHandler(t *testing.T) (*Handler, *mocks.MockActivityStore) {
	t.Helper()
	ctrl := gomock.NewController(t)
	store := mocks.NewMockActivityStore(ctrl)
	svc := activity.NewService(store)
	return NewHandler(svc), store
}

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

func sampleEntries() []models.Entry {
	return []models.Entry{
		{
			ID:          100,
			EventID:     "evt-1",
			Category:    "device_command",
			Type:        "reboot",
			Description: "Reboot",
			Result:      "success",
			ActorType:   "user",
			UserID:      strPtr("usr_abc"),
			Username:    strPtr("admin"),
			ScopeCount:  intPtr(24),
			CreatedAt:   testTime,
			Metadata:    json.RawMessage(`{"batch_id":"b-1"}`),
		},
		{
			ID:          99,
			EventID:     "evt-2",
			Category:    "auth",
			Type:        "login",
			Description: "Login",
			Result:      "success",
			ActorType:   "user",
			UserID:      strPtr("usr_abc"),
			Username:    strPtr("admin"),
			CreatedAt:   testTime.Add(-time.Minute),
		},
	}
}

func TestListActivities_Unauthenticated(t *testing.T) {
	h, _ := newTestHandler(t)

	_, err := h.ListActivities(context.Background(), connect.NewRequest(&pb.ListActivitiesRequest{}))
	require.Error(t, err)

	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeUnauthenticated, fleetErr.GRPCCode)
}

func TestListActivities_NoFilters(t *testing.T) {
	h, store := newTestHandler(t)
	entries := sampleEntries()

	store.EXPECT().List(gomock.Any(), gomock.Any()).Return(entries, nil)
	store.EXPECT().Count(gomock.Any(), gomock.Any()).Return(int64(2), nil)

	resp, err := h.ListActivities(authedCtx(), connect.NewRequest(&pb.ListActivitiesRequest{}))
	require.NoError(t, err)

	assert.Len(t, resp.Msg.Activities, 2)
	assert.Equal(t, int32(2), resp.Msg.TotalCount)

	first := resp.Msg.Activities[0]
	assert.Equal(t, "evt-1", first.EventId)
	assert.Equal(t, "device_command", first.EventCategory)
	assert.Equal(t, "reboot", first.EventType)
	assert.Equal(t, "Reboot", first.Description)
	assert.Equal(t, "success", first.Result)
	assert.Equal(t, int32(24), first.ScopeCount)
	assert.NotNil(t, first.Metadata)
}

func TestListActivities_WithFilters(t *testing.T) {
	h, store := newTestHandler(t)

	store.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, f models.Filter) ([]models.Entry, error) {
			assert.Equal(t, testOrgID, f.OrganizationID)
			assert.Equal(t, []string{"auth"}, f.EventCategories)
			assert.Equal(t, []string{"login"}, f.EventTypes)
			assert.Equal(t, "reboot", f.SearchText)
			return nil, nil
		},
	)
	store.EXPECT().Count(gomock.Any(), gomock.Any()).Return(int64(0), nil)

	_, err := h.ListActivities(authedCtx(), connect.NewRequest(&pb.ListActivitiesRequest{
		Filter: &pb.ActivityFilter{
			EventCategories: []string{"auth"},
			EventTypes:      []string{"login"},
			SearchText:      "reboot",
		},
	}))
	require.NoError(t, err)
}

func TestListActivities_Pagination(t *testing.T) {
	h, store := newTestHandler(t)

	page1 := make([]models.Entry, models.DefaultPageSize)
	for i := range page1 {
		page1[i] = models.Entry{
			ID:        int64(100 - i),
			EventID:   "evt",
			Category:  "auth",
			Type:      "login",
			Result:    "success",
			ActorType: "user",
			CreatedAt: testTime.Add(-time.Duration(i) * time.Minute),
		}
	}

	store.EXPECT().List(gomock.Any(), gomock.Any()).Return(page1, nil)
	store.EXPECT().Count(gomock.Any(), gomock.Any()).Return(int64(75), nil)

	resp, err := h.ListActivities(authedCtx(), connect.NewRequest(&pb.ListActivitiesRequest{}))
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Msg.NextPageToken)
	assert.Equal(t, int32(75), resp.Msg.TotalCount)

	store.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, f models.Filter) ([]models.Entry, error) {
			require.NotNil(t, f.CursorTime)
			require.NotNil(t, f.CursorID)
			return nil, nil
		},
	)

	_, err = h.ListActivities(authedCtx(), connect.NewRequest(&pb.ListActivitiesRequest{
		PageToken: resp.Msg.NextPageToken,
	}))
	require.NoError(t, err)
}

func TestListActivities_OversizedPageSize(t *testing.T) {
	h, store := newTestHandler(t)

	page := make([]models.Entry, models.MaxPageSize)
	for i := range page {
		page[i] = models.Entry{
			ID:        int64(200 - i),
			EventID:   "evt",
			Category:  "auth",
			Type:      "login",
			Result:    "success",
			ActorType: "user",
			CreatedAt: testTime.Add(-time.Duration(i) * time.Minute),
		}
	}

	store.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, f models.Filter) ([]models.Entry, error) {
			assert.Equal(t, models.MaxPageSize, f.PageSize)
			return page, nil
		},
	)
	store.EXPECT().Count(gomock.Any(), gomock.Any()).Return(int64(500), nil)

	resp, err := h.ListActivities(authedCtx(), connect.NewRequest(&pb.ListActivitiesRequest{
		PageSize: 1000,
	}))
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Msg.NextPageToken, "next_page_token should be set when a full page is returned")
	assert.Equal(t, int32(500), resp.Msg.TotalCount)
}

func TestListActivities_InvalidPageToken(t *testing.T) {
	h, _ := newTestHandler(t)

	_, err := h.ListActivities(authedCtx(), connect.NewRequest(&pb.ListActivitiesRequest{
		PageToken: "not-valid-base64!@#",
	}))
	require.Error(t, err)

	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
}

func TestListActivities_ZeroIDPageToken(t *testing.T) {
	h, _ := newTestHandler(t)

	token := base64.URLEncoding.EncodeToString(
		[]byte(`{"created_at":"2026-03-23T21:30:00Z","id":0}`),
	)

	_, err := h.ListActivities(authedCtx(), connect.NewRequest(&pb.ListActivitiesRequest{
		PageToken: token,
	}))
	require.Error(t, err)

	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
}

func TestListActivities_NegativeIDPageToken(t *testing.T) {
	h, _ := newTestHandler(t)

	token := base64.URLEncoding.EncodeToString(
		[]byte(`{"created_at":"2026-03-23T21:30:00Z","id":-5}`),
	)

	_, err := h.ListActivities(authedCtx(), connect.NewRequest(&pb.ListActivitiesRequest{
		PageToken: token,
	}))
	require.Error(t, err)

	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
}

func TestListActivityFilterOptions(t *testing.T) {
	h, store := newTestHandler(t)

	eventTypes := []models.EventTypeInfo{
		{EventType: "login", EventCategory: "auth"},
		{EventType: "reboot", EventCategory: "device_command"},
	}
	scopeTypes := []string{"group", "rack"}
	users := []models.UserInfo{
		{UserID: "usr_abc", Username: "admin"},
	}

	store.EXPECT().GetDistinctEventTypes(gomock.Any(), testOrgID).Return(eventTypes, nil)
	store.EXPECT().GetDistinctScopeTypes(gomock.Any(), testOrgID).Return(scopeTypes, nil)
	store.EXPECT().GetDistinctUsers(gomock.Any(), testOrgID).Return(users, nil)

	resp, err := h.ListActivityFilterOptions(authedCtx(), connect.NewRequest(&pb.ListActivityFilterOptionsRequest{}))
	require.NoError(t, err)

	assert.Len(t, resp.Msg.EventTypes, 2)
	assert.Equal(t, "login", resp.Msg.EventTypes[0].EventType)
	assert.Equal(t, []string{"group", "rack"}, resp.Msg.ScopeTypes)
	assert.Len(t, resp.Msg.Users, 1)
	assert.Equal(t, "admin", resp.Msg.Users[0].Username)
}

func TestListActivityFilterOptions_Unauthenticated(t *testing.T) {
	h, _ := newTestHandler(t)

	_, err := h.ListActivityFilterOptions(context.Background(), connect.NewRequest(&pb.ListActivityFilterOptionsRequest{}))
	require.Error(t, err)

	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeUnauthenticated, fleetErr.GRPCCode)
}

func TestFormatScope(t *testing.T) {
	tests := []struct {
		name       string
		scopeType  *string
		scopeLabel *string
		scopeCount *int
		expected   string
	}{
		{"no scope", nil, nil, nil, "\u2014"},
		{"count only", nil, nil, intPtr(24), "24 miners"},
		{"count one", nil, nil, intPtr(1), "1 miner"},
		{"label only", strPtr("group"), strPtr("A01"), nil, "A01"},
		{"label and count", strPtr("group"), strPtr("A01"), intPtr(12), "A01 (12 miners)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatScope(tt.scopeType, tt.scopeLabel, tt.scopeCount)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatActivityTimestamp(t *testing.T) {
	ts := time.Date(2026, 3, 23, 21, 30, 0, 0, time.UTC)
	result := formatActivityTimestamp(ts)
	assert.Equal(t, "2026-03-23T21:30:00Z", result)

	morning := time.Date(2026, 1, 5, 9, 5, 0, 0, time.UTC)
	result = formatActivityTimestamp(morning)
	assert.Equal(t, "2026-01-05T09:05:00Z", result)

	noon := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	result = formatActivityTimestamp(noon)
	assert.Equal(t, "2026-06-15T12:00:00Z", result)

	midnight := time.Date(2026, 12, 25, 0, 0, 0, 0, time.UTC)
	result = formatActivityTimestamp(midnight)
	assert.Equal(t, "2026-12-25T00:00:00Z", result)
}

func TestCursorRoundTrip(t *testing.T) {
	ts := time.Date(2026, 3, 23, 21, 30, 0, 0, time.UTC)
	id := int64(42)

	token, err := encodeCursor(ts, id)
	require.NoError(t, err)

	gotTime, gotID, err := decodeCursor(token)
	require.NoError(t, err)
	assert.True(t, ts.Equal(gotTime))
	assert.Equal(t, id, gotID)
}

// --- ExportActivities (server-streaming) tests ---

type testAuthInjector struct{}

func (testAuthInjector) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		return next(authedCtx(), req)
	}
}

func (testAuthInjector) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (testAuthInjector) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		return next(authedCtx(), conn)
	}
}

type cancelableAuthInjector struct {
	cancelCh chan<- context.CancelFunc
}

func (cancelableAuthInjector) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		return next(authedCtx(), req)
	}
}

func (cancelableAuthInjector) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (c cancelableAuthInjector) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(_ context.Context, conn connect.StreamingHandlerConn) error {
		ctx, cancel := context.WithCancel(authedCtx())
		c.cancelCh <- cancel
		return next(ctx, conn)
	}
}

func startTestServer(t *testing.T, h *Handler, authenticated bool) activityv1connect.ActivityServiceClient {
	t.Helper()
	ii := []connect.Interceptor{interceptors.NewErrorMappingInterceptor()}
	if authenticated {
		ii = append(ii, testAuthInjector{})
	}
	opts := []connect.HandlerOption{connect.WithInterceptors(ii...)}
	mux := http.NewServeMux()
	mux.Handle(activityv1connect.NewActivityServiceHandler(h, opts...))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return activityv1connect.NewActivityServiceClient(http.DefaultClient, srv.URL)
}

func TestExportActivities(t *testing.T) {
	h, store := newTestHandler(t)
	entries := sampleEntries()

	store.EXPECT().List(gomock.Any(), gomock.Any()).Return(entries, nil)

	client := startTestServer(t, h, true)
	stream, err := client.ExportActivities(context.Background(), connect.NewRequest(&pb.ExportActivitiesRequest{}))
	require.NoError(t, err)

	var buf bytes.Buffer
	for stream.Receive() {
		buf.Write(stream.Msg().GetChunk())
	}
	require.NoError(t, stream.Err())
	require.NoError(t, stream.Close())

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 3)

	assert.Equal(t, "Timestamp,Type,Category,Description,Result,Scope,User", lines[0])
	assert.Contains(t, lines[1], "reboot")
	assert.Contains(t, lines[1], "24 miners")
	assert.Contains(t, lines[1], "admin")
	assert.Contains(t, lines[2], "login")
}

func TestFormatCSVRow_QuoteEscaping(t *testing.T) {
	e := models.Entry{
		ID:          1,
		EventID:     "evt-1",
		Category:    "auth",
		Type:        "login_failed",
		Description: `Login failed, reason: "invalid password"`,
		Result:      "failure",
		ActorType:   "user",
		Username:    strPtr(`admin "test"`),
		CreatedAt:   time.Date(2026, 3, 23, 21, 30, 0, 0, time.UTC),
	}

	row := formatCSVRow(e)

	assert.Contains(t, row, `"Login failed, reason: ""invalid password"""`)
	assert.Contains(t, row, `"admin ""test"""`)
	assert.NotContains(t, row, `\"`)
}

func TestFormatCSVRow_FormulaInjection(t *testing.T) {
	e := models.Entry{
		ID:          1,
		EventID:     "evt-1",
		Category:    "auth",
		Type:        "login",
		Description: "=cmd|'/C calc'!A0",
		Result:      "+success",
		ActorType:   "user",
		Username:    strPtr("-admin"),
		CreatedAt:   time.Date(2026, 3, 23, 21, 30, 0, 0, time.UTC),
	}

	row := formatCSVRow(e)
	assert.Contains(t, row, "'=cmd")
	assert.Contains(t, row, "'+success")
	assert.Contains(t, row, "'-admin")
	assert.NotContains(t, row, ",=cmd")
	assert.NotContains(t, row, ",+success")
	assert.NotContains(t, row, ",-admin")
}

func TestSanitizeCSVField(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal", "normal"},
		{"", ""},
		{"=formula", "'=formula"},
		{"+cmd", "'+cmd"},
		{"-value", "'-value"},
		{"@mention", "'@mention"},
		{"\tcmd", "'\tcmd"},
		{"\rcmd", "'\rcmd"},
		{"safe =value", "safe =value"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, sanitizeCSVField(tt.input))
		})
	}
}

func TestFormatCSVRow_UserIDFallback(t *testing.T) {
	e := models.Entry{
		ID:          1,
		EventID:     "evt-1",
		Category:    "system",
		Type:        "config_change",
		Description: "Config updated",
		Result:      "success",
		ActorType:   "user",
		UserID:      strPtr("usr_xyz"),
		Username:    nil,
		CreatedAt:   time.Date(2026, 3, 23, 21, 30, 0, 0, time.UTC),
	}

	row := formatCSVRow(e)
	assert.Contains(t, row, "usr_xyz")
	assert.True(t, strings.HasSuffix(strings.TrimSpace(row), "usr_xyz"),
		"user column should contain user_id when username is nil")
}

func TestFormatCSVRow_NoUserInfo(t *testing.T) {
	e := models.Entry{
		ID:          1,
		EventID:     "evt-1",
		Category:    "system",
		Type:        "heartbeat",
		Description: "Heartbeat",
		Result:      "success",
		ActorType:   "system",
		CreatedAt:   time.Date(2026, 3, 23, 21, 30, 0, 0, time.UTC),
	}

	row := formatCSVRow(e)
	assert.Contains(t, row, "\u2014")
}

func TestExportActivities_ContextCanceled(t *testing.T) {
	h, store := newTestHandler(t)

	fullPage := make([]models.Entry, models.MaxPageSize)
	for i := range fullPage {
		fullPage[i] = models.Entry{
			ID:        int64(1000 - i),
			EventID:   "evt",
			Category:  "auth",
			Type:      "login",
			Result:    "success",
			ActorType: "user",
			CreatedAt: testTime.Add(-time.Duration(i) * time.Minute),
		}
	}

	cancelCh := make(chan context.CancelFunc, 1)

	// The first (and only expected) List call cancels the server-side context
	// before returning a full page. The handler will process the page, loop
	// back, and hit the ctx.Err() check before making a second List call.
	gomock.InOrder(
		store.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ models.Filter) ([]models.Entry, error) {
				cancel := <-cancelCh
				cancel()
				return fullPage, nil
			},
		),
		store.EXPECT().List(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes(),
	)

	ii := []connect.Interceptor{
		interceptors.NewErrorMappingInterceptor(),
		cancelableAuthInjector{cancelCh: cancelCh},
	}
	opts := []connect.HandlerOption{connect.WithInterceptors(ii...)}
	mux := http.NewServeMux()
	mux.Handle(activityv1connect.NewActivityServiceHandler(h, opts...))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client := activityv1connect.NewActivityServiceClient(http.DefaultClient, srv.URL)

	stream, err := client.ExportActivities(context.Background(), connect.NewRequest(&pb.ExportActivitiesRequest{}))
	require.NoError(t, err)

	for stream.Receive() {
	}
	err = stream.Err()
	_ = stream.Close()

	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeCanceled, connectErr.Code())
}

func TestExportActivities_Unauthenticated(t *testing.T) {
	h, _ := newTestHandler(t)

	client := startTestServer(t, h, false)
	stream, err := client.ExportActivities(context.Background(), connect.NewRequest(&pb.ExportActivitiesRequest{}))
	if err == nil {
		for stream.Receive() {
		}
		err = stream.Err()
		_ = stream.Close()
	}

	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnauthenticated, connectErr.Code())
}
