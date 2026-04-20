package errorquery

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	errorsv1 "github.com/block/proto-fleet/server/generated/grpc/errors/v1"
	"github.com/block/proto-fleet/server/internal/domain/diagnostics"
	"github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	storesMocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

const (
	testOrgID     = int64(123)
	testUserID    = int64(456)
	testSessionID = "test-session-id"
	testErrorID   = "01HZKM3X9E8Y7T6V5W4N3M2K1J"
	testDeviceID  = "device-001"
)

var testTime = time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

func setupTestContext() context.Context {
	ctx := context.Background()
	info := &session.Info{
		SessionID:      testSessionID,
		UserID:         testUserID,
		OrganizationID: testOrgID,
	}
	return authn.SetInfo(ctx, info)
}

func createTestErrorMessage() *models.ErrorMessage {
	componentID := "psu-1"
	return &models.ErrorMessage{
		ErrorID:           testErrorID,
		MinerError:        models.MinerError(1003),
		Summary:           "PSU fault detected",
		CauseSummary:      "Power supply unit is malfunctioning",
		RecommendedAction: "Check PSU connections and replace if necessary",
		Severity:          models.Severity(1),
		FirstSeenAt:       testTime.Add(-1 * time.Hour),
		LastSeenAt:        testTime,
		ClosedAt:          nil,
		VendorAttributes: map[string]string{
			"vendor_code": "E1003",
			"firmware":    "v2.1.0",
		},
		DeviceID:      testDeviceID,
		ComponentID:   &componentID,
		ComponentType: models.ComponentType(1),
		Impact:        "Mining stopped",
		VendorCode:    "E1003",
		Firmware:      "v2.1.0",
	}
}

func TestHandler_GetError(t *testing.T) {
	tests := []struct {
		name             string
		request          *errorsv1.GetErrorRequest
		setupMocks       func(*storesMocks.MockErrorStore)
		setupContext     func() context.Context
		expectedError    bool
		expectedCode     connect.Code
		errorContains    string
		validateResponse func(*testing.T, *errorsv1.GetErrorResponse)
	}{
		{
			name: "successful request",
			request: &errorsv1.GetErrorRequest{
				ErrorId: testErrorID,
			},
			setupMocks: func(mockStore *storesMocks.MockErrorStore) {
				mockStore.EXPECT().
					GetErrorByErrorID(gomock.Any(), testOrgID, testErrorID).
					Return(createTestErrorMessage(), nil)
			},
			setupContext:  setupTestContext,
			expectedError: false,
			validateResponse: func(t *testing.T, resp *errorsv1.GetErrorResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Error)
				require.Equal(t, testErrorID, resp.Error.ErrorId)
				require.Equal(t, errorsv1.MinerError_MINER_ERROR_PSU_FAULT_GENERIC, resp.Error.CanonicalError)
				require.Equal(t, "PSU fault detected", resp.Error.Summary)
				require.Equal(t, errorsv1.Severity_SEVERITY_CRITICAL, resp.Error.Severity)
				require.Equal(t, testDeviceID, resp.Error.DeviceIdentifier)
				require.NotNil(t, resp.Error.ComponentId)
				require.Equal(t, "psu-1", *resp.Error.ComponentId)
				require.Equal(t, "Mining stopped", resp.Error.Impact)
				require.Len(t, resp.Error.VendorAttributes, 2)
				require.Equal(t, "E1003", resp.Error.VendorAttributes["vendor_code"])
			},
		},
		{
			name: "missing error_id",
			request: &errorsv1.GetErrorRequest{
				ErrorId: "",
			},
			setupMocks:    func(mockStore *storesMocks.MockErrorStore) {},
			setupContext:  setupTestContext,
			expectedError: true,
			expectedCode:  connect.CodeInvalidArgument,
			errorContains: "error_id is required",
		},
		{
			name: "missing session info",
			request: &errorsv1.GetErrorRequest{
				ErrorId: testErrorID,
			},
			setupMocks:    func(mockStore *storesMocks.MockErrorStore) {},
			setupContext:  context.Background, // No session info
			expectedError: true,
			expectedCode:  connect.CodeUnauthenticated,
			errorContains: "Context does not have session info",
		},
		{
			name: "error not found",
			request: &errorsv1.GetErrorRequest{
				ErrorId: testErrorID,
			},
			setupMocks: func(mockStore *storesMocks.MockErrorStore) {
				mockStore.EXPECT().
					GetErrorByErrorID(gomock.Any(), testOrgID, testErrorID).
					Return(nil, fleeterror.NewNotFoundErrorf("error not found: %s", testErrorID))
			},
			setupContext:  setupTestContext,
			expectedError: true,
			expectedCode:  connect.CodeNotFound,
			errorContains: "error not found",
		},
		{
			name: "database error",
			request: &errorsv1.GetErrorRequest{
				ErrorId: testErrorID,
			},
			setupMocks: func(mockStore *storesMocks.MockErrorStore) {
				mockStore.EXPECT().
					GetErrorByErrorID(gomock.Any(), testOrgID, testErrorID).
					Return(nil, fleeterror.NewInternalError("database connection failed"))
			},
			setupContext:  setupTestContext,
			expectedError: true,
			expectedCode:  connect.CodeInternal,
			errorContains: "database connection failed",
		},
		{
			name: "error with closed_at timestamp",
			request: &errorsv1.GetErrorRequest{
				ErrorId: testErrorID,
			},
			setupMocks: func(mockStore *storesMocks.MockErrorStore) {
				errMsg := createTestErrorMessage()
				closedTime := testTime.Add(10 * time.Minute)
				errMsg.ClosedAt = &closedTime
				mockStore.EXPECT().
					GetErrorByErrorID(gomock.Any(), testOrgID, testErrorID).
					Return(errMsg, nil)
			},
			setupContext:  setupTestContext,
			expectedError: false,
			validateResponse: func(t *testing.T, resp *errorsv1.GetErrorResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Error)
				require.NotNil(t, resp.Error.ClosedAt)
				require.Equal(t, testTime.Add(10*time.Minute).Unix(), resp.Error.ClosedAt.AsTime().Unix())
			},
		},
		{
			name: "error without component",
			request: &errorsv1.GetErrorRequest{
				ErrorId: testErrorID,
			},
			setupMocks: func(mockStore *storesMocks.MockErrorStore) {
				errMsg := createTestErrorMessage()
				errMsg.ComponentID = nil
				mockStore.EXPECT().
					GetErrorByErrorID(gomock.Any(), testOrgID, testErrorID).
					Return(errMsg, nil)
			},
			setupContext:  setupTestContext,
			expectedError: false,
			validateResponse: func(t *testing.T, resp *errorsv1.GetErrorResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Error)
				require.Nil(t, resp.Error.ComponentId)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockErrorStore := storesMocks.NewMockErrorStore(ctrl)
			mockTransactor := storesMocks.NewMockTransactor(ctrl)
			tt.setupMocks(mockErrorStore)

			diagnosticsSvc := diagnostics.NewService(context.Background(), diagnostics.Config{}, mockErrorStore, mockTransactor)
			handler := NewHandler(diagnosticsSvc)

			ctx := tt.setupContext()
			req := connect.NewRequest(tt.request)

			resp, err := handler.GetError(ctx, req)

			if tt.expectedError {
				require.Error(t, err)
				require.Nil(t, resp)

				var connectErr *connect.Error
				require.ErrorAs(t, err, &connectErr)
				require.Equal(t, tt.expectedCode, connectErr.Code())
				if tt.errorContains != "" {
					require.Contains(t, connectErr.Message(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				if tt.validateResponse != nil {
					tt.validateResponse(t, resp.Msg)
				}
			}
		})
	}
}
