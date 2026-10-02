package rollout

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/validate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	pb "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/block/proto-fleet/server/generated/grpc/rollout/v1/rolloutv1connect"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	"github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/command"
	domain "github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/handlers/firmware"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/block/proto-fleet/server/internal/testutil"
)

type restLifecycle struct {
	mux         *http.ServeMux
	database    *sql.DB
	rawKey      string
	keyID       int64
	identifiers []string
}

func newRESTLifecycle(t *testing.T) *restLifecycle {
	t.Helper()
	if testing.Short() || os.Getenv("DB_PASSWORD") == "" {
		t.Skip("REST lifecycle integration test needs a database (DB_PASSWORD)")
	}
	t.Chdir(t.TempDir())
	config, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, config)
	owner := database.CreateSuperAdminUser()
	userStore := sqlstores.NewSQLUserStore(database.DB)
	user, err := userStore.GetUserByID(t.Context(), owner.DatabaseID)
	require.NoError(t, err)
	activitySvc := activity.NewService(sqlstores.NewSQLActivityStore(database.DB))
	keySvc := apikey.NewService(sqlstores.NewSQLApiKeyStore(database.DB), activitySvc)
	rawKey, _, err := keySvc.Create(t.Context(), owner.DatabaseID, owner.OrganizationID, user.UserID, owner.Username, "REST rollout controller", nil)
	require.NoError(t, err)
	key, err := keySvc.Validate(t.Context(), rawKey)
	require.NoError(t, err)
	require.NotZero(t, key.ID)
	sessions := session.NewService(session.Config{CookieName: "fleet_session", Duration: time.Hour, IDBytes: 32}, sqlstores.NewSQLSessionStore(database.DB))
	auth := interceptors.NewAuthInterceptor(sessions, userStore, userStore, keySvc, authz.NewPermissionResolver(database.DB), nil, nil, nil)
	fileSvc, err := files.NewService(files.Config{})
	require.NoError(t, err)
	deviceStore := sqlstores.NewSQLDeviceStore(database.DB)
	messageQueue := queue.NewDatabaseMessageQueue(&queue.Config{}, database.DB)
	// Admission and SQL enqueue are real. Zero worker slots keep commands
	// pending so this API test never contacts a miner or races delivery.
	commandConfig := &command.Config{MaxWorkers: 0, MasterPollingInterval: time.Hour, BatchStatusUpdatePollingInterval: 10 * time.Millisecond}
	execution := command.NewExecutionService(commandConfig, database.DB, messageQueue, nil, nil, nil, deviceStore, nil, fileSvc)
	require.NoError(t, execution.Start(t.Context()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, execution.Stop(ctx))
	})
	commands := command.NewService(commandConfig, database.DB, execution, messageQueue, command.NewStatusService(database.DB, messageQueue), nil, fileSvc, deviceStore, userStore, nil, nil, nil, activitySvc)
	commands.RegisterFilter(command.NewCurtailmentActiveFilter(sqlstores.NewSQLCurtailmentStore(database.DB)))
	commands.RegisterFilter(command.NewReleaseChannelFirmwareFilter(database.DB))
	t.Cleanup(func() {
		// Let the command status poller finish before its database is closed.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := database.DB.ExecContext(ctx, `UPDATE queue_message SET status = 'FAILED'`)
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			var unfinished int
			err := database.DB.QueryRowContext(ctx, `SELECT count(*) FROM command_batch_log WHERE status != 'FINISHED'`).Scan(&unfinished)
			return err == nil && unfinished == 0
		}, time.Second, 10*time.Millisecond)
	})
	queries := sqlstores.NewSQLConnectionManager(database.DB)
	svc := domain.NewService(&queries, sqlstores.NewSQLTransactor(database.DB), commands, fileSvc, activitySvc)
	_, rpc := rolloutv1connect.NewRolloutServiceHandler(NewHandler(svc), connect.WithInterceptors(interceptors.NewErrorMappingInterceptor(), auth, validate.NewInterceptor()))
	mux := http.NewServeMux()
	RegisterRESTRoutes(mux, rpc)
	mux.Handle("POST /api/v1/firmware/upload", firmware.NewUploadHandler(fileSvc, auth, activitySvc))
	env := &restLifecycle{mux: mux, database: database.DB, rawKey: rawKey, keyID: key.ID}
	for range 3 {
		env.identifiers = append(env.identifiers, database.CreateDevice(owner.OrganizationID, "proto").ID)
	}
	return env
}

func (e *restLifecycle) request(t *testing.T, method, path string, body proto.Message) *httptest.ResponseRecorder {
	t.Helper()
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = protojson.Marshal(body)
		require.NoError(t, err)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+e.rawKey)
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, r)
	return w
}

func (e *restLifecycle) call(t *testing.T, method, path string, body, response proto.Message) {
	t.Helper()
	w := e.request(t, method, path, body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, protojson.Unmarshal(w.Body.Bytes(), response))
}

func (e *restLifecycle) upload(t *testing.T) string {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range map[string]string{"target_manufacturer": "TestCorp", "target_model": "TestMiner", "firmware_version": "2.0.0"} {
		require.NoError(t, writer.WriteField(name, value))
	}
	part, err := writer.CreateFormFile("file", "rest-update.swu")
	require.NoError(t, err)
	_, err = part.Write([]byte("REST lifecycle firmware payload"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	r := httptest.NewRequest(http.MethodPost, "/api/v1/firmware/upload", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.Header.Set("Authorization", "Bearer "+e.rawKey)
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var uploaded struct {
		FileID string `json:"firmware_file_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &uploaded))
	require.NotEmpty(t, uploaded.FileID)
	return uploaded.FileID
}

func requireRESTReason(t *testing.T, response *httptest.ResponseRecorder, reason pb.RolloutErrorReason) *pb.RolloutErrorInfo {
	t.Helper()
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	var failure struct {
		Code    string `json:"code"`
		Details []struct {
			Type  string          `json:"type"`
			Debug json.RawMessage `json:"debug"`
		} `json:"details"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &failure))
	require.Equal(t, "failed_precondition", failure.Code)
	for _, detail := range failure.Details {
		if detail.Type == "rollout.v1.RolloutErrorInfo" {
			var info pb.RolloutErrorInfo
			require.NoError(t, protojson.Unmarshal(detail.Debug, &info))
			require.Equal(t, reason, info.Reason)
			return &info
		}
	}
	t.Fatal("REST failure omitted RolloutErrorInfo")
	return nil
}

func TestRESTAPIKeyDelegatedLifecycle(t *testing.T) {
	e := newRESTLifecycle(t)
	fileID := e.upload(t)
	created := &pb.CreateReleaseChannelResponse{}
	e.call(t, http.MethodPost, "/api/v1/release-channels", &pb.CreateReleaseChannelRequest{
		Name: "REST controlled channel", Scope: &pb.ReleaseChannelScope{DeviceIdentifiers: e.identifiers},
		Behavior: &pb.RolloutBehavior{Method: pb.RolloutMethod_ROLLOUT_METHOD_DELEGATED},
	}, created)
	channelPath := fmt.Sprintf("/api/v1/release-channels/%d", created.Channel.Id)
	assignments := []*pb.FirmwareAssignment{{Manufacturer: "TestCorp", Model: "TestMiner", FirmwareFileId: fileID}}
	preview := &pb.PreviewReleaseChannelFirmwareResponse{}
	e.call(t, http.MethodPost, channelPath+"/firmware/preview", &pb.PreviewReleaseChannelFirmwareRequest{Assignments: assignments}, preview)
	require.Len(t, preview.Plans, 1)
	assert.EqualValues(t, 3, preview.Plans[0].TargetCount)
	applied := &pb.ApplyReleaseChannelFirmwareResponse{}
	e.call(t, http.MethodPost, channelPath+"/firmware/apply", &pb.ApplyReleaseChannelFirmwareRequest{Assignments: assignments}, applied)
	require.Len(t, applied.StartedRollouts, 1)
	rollout := applied.StartedRollouts[0]
	assert.Equal(t, pb.RolloutState_ROLLOUT_STATE_WAITING_FOR_CONTROLLER, rollout.State)
	rolloutPath := fmt.Sprintf("/api/v1/rollouts/%d", rollout.Id)
	devices := &pb.ListRolloutDevicesResponse{}
	e.call(t, http.MethodGet, rolloutPath+"/devices", nil, devices)
	require.Len(t, devices.Devices, 3)
	queueCount := func() int {
		var count int
		require.NoError(t, e.database.QueryRowContext(t.Context(), `SELECT count(*) FROM queue_message`).Scan(&count))
		return count
	}
	require.Zero(t, queueCount(), "delegated apply must not enqueue firmware commands")
	advance := &pb.AdvanceRolloutRequest{Selection: &pb.AdvanceRolloutRequest_Count{Count: 1}, ExpectedRevision: rollout.Revision, Note: "Canary approved"}
	advanced := &pb.AdvanceRolloutResponse{}
	e.call(t, http.MethodPost, rolloutPath+"/advance", advance, advanced)
	require.Len(t, advanced.DeviceIdentifiers, 1)
	require.Equal(t, 1, queueCount())
	stale := requireRESTReason(t, e.request(t, http.MethodPost, rolloutPath+"/advance", advance), pb.RolloutErrorReason_ROLLOUT_ERROR_REASON_STALE_REVISION)
	assert.Equal(t, advanced.Rollout.Revision, stale.CurrentRevision)
	assert.Equal(t, 1, queueCount(), "retrying a stale request must not queue another command")
	paused := &pb.PauseRolloutResponse{}
	e.call(t, http.MethodPost, rolloutPath+"/pause", &pb.PauseRolloutRequest{ExpectedRevision: advanced.Rollout.Revision}, paused)
	requireRESTReason(t, e.request(t, http.MethodPost, rolloutPath+"/advance", &pb.AdvanceRolloutRequest{
		Selection: &pb.AdvanceRolloutRequest_Count{Count: 1}, ExpectedRevision: paused.Rollout.Revision,
	}), pb.RolloutErrorReason_ROLLOUT_ERROR_REASON_PAUSED)
	assert.Equal(t, 1, queueCount())
	resumed := &pb.ResumeRolloutResponse{}
	e.call(t, http.MethodPost, rolloutPath+"/resume", &pb.ResumeRolloutRequest{ExpectedRevision: paused.Rollout.Revision}, resumed)
	var remaining []string
	for _, identifier := range e.identifiers {
		if identifier != advanced.DeviceIdentifiers[0] {
			remaining = append(remaining, identifier)
		}
	}
	skipped := &pb.SkipRolloutDevicesResponse{}
	e.call(t, http.MethodPost, rolloutPath+"/skip", &pb.SkipRolloutDevicesRequest{
		Devices: &pb.RolloutDeviceSelection{DeviceIdentifiers: remaining}, ExpectedRevision: resumed.Rollout.Revision, Note: "Hold remaining miners",
	}, skipped)
	assert.EqualValues(t, 2, skipped.Rollout.DeviceCounts.Skipped)
	requireRESTReason(t, e.request(t, http.MethodPost, rolloutPath+"/complete", &pb.CompleteRolloutRequest{
		ExpectedRevision: skipped.Rollout.Revision, Note: "Requested completion",
	}), pb.RolloutErrorReason_ROLLOUT_ERROR_REASON_UPDATES_IN_FLIGHT)
	canceled := &pb.CancelRolloutResponse{}
	e.call(t, http.MethodPost, rolloutPath+"/cancel", &pb.CancelRolloutRequest{
		ExpectedRevision: skipped.Rollout.Revision, Note: "Operator ended rollout",
	}, canceled)
	assert.Equal(t, pb.RolloutStatus_ROLLOUT_STATUS_CANCELED, canceled.Rollout.Status)
	assert.Equal(t, 1, queueCount(), "cancel must retain an already admitted command")
	events := &pb.ListRolloutEventsResponse{}
	e.call(t, http.MethodGet, rolloutPath+"/events", nil, events)
	require.Len(t, events.Events, 6, "rejected calls must not create lifecycle events")
	assert.NotEmpty(t, events.Cursor)
	var types []pb.RolloutEventType
	notes := map[pb.RolloutEventType]string{
		pb.RolloutEventType_ROLLOUT_EVENT_TYPE_ADVANCED: "Canary approved", pb.RolloutEventType_ROLLOUT_EVENT_TYPE_DEVICES_SKIPPED: "Hold remaining miners",
		pb.RolloutEventType_ROLLOUT_EVENT_TYPE_CANCELED: "Operator ended rollout",
	}
	for i, event := range events.Events {
		types = append(types, event.Type)
		require.NotNil(t, event.Actor)
		assert.Equal(t, pb.RolloutActorType_ROLLOUT_ACTOR_TYPE_API_KEY, event.Actor.Type)
		assert.Equal(t, e.keyID, event.Actor.Id)
		assert.Equal(t, "REST rollout controller", event.Actor.Name)
		assert.Equal(t, notes[event.Type], event.Note)
		if i > 0 {
			assert.Greater(t, event.Id, events.Events[i-1].Id)
			assert.Greater(t, event.RolloutRevision, events.Events[i-1].RolloutRevision)
		}
	}
	assert.Equal(t, []pb.RolloutEventType{
		pb.RolloutEventType_ROLLOUT_EVENT_TYPE_STARTED, pb.RolloutEventType_ROLLOUT_EVENT_TYPE_ADVANCED,
		pb.RolloutEventType_ROLLOUT_EVENT_TYPE_PAUSED, pb.RolloutEventType_ROLLOUT_EVENT_TYPE_RESUMED,
		pb.RolloutEventType_ROLLOUT_EVENT_TYPE_DEVICES_SKIPPED, pb.RolloutEventType_ROLLOUT_EVENT_TYPE_CANCELED,
	}, types)
}
