package command

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	commandMocks "github.com/block/proto-fleet/server/internal/domain/command/mocks"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/passwordupdate"
	minerDomain "github.com/block/proto-fleet/server/internal/domain/miner"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	minerIfaceMocks "github.com/block/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	storeMocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	infraDB "github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	queueMocks "github.com/block/proto-fleet/server/internal/infrastructure/queue/mocks"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkMocks "github.com/block/proto-fleet/server/sdk/v1/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/proto"
)

const (
	testServiceMasterKey    = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	testMinerAuthPrivateKey = "z65ViaeDr/SF9jyoEJ/lp/Vsl8C4SrxehBbCCLez9OUA4ni3G8J1K/9db5tXyxx+xd3syUtei8Nw0Ml9QOVzGEvzsnVxp8B7G63VM8ls7i4rncYDrlRV4ietDPs="
)

func TestExecuteCommand_UpdateMinerPassword_UpdatesExistingCredentials(t *testing.T) {
	svc, db, encryptSvc, dbDeviceID, deviceIdentifier := setupPasswordCommandDevice(t, "antminer", true)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	wirePasswordCommandMocks(t, ctrl, svc, dbDeviceID, deviceIdentifier, "antminer")

	_, _, err := svc.executeCommandOnDevice(t.Context(), commandtype.UpdateMinerPassword, passwordUpdateMessage(t, dbDeviceID))

	require.NoError(t, err)
	username, password := storedCredentials(t, db, encryptSvc, dbDeviceID)
	assert.Equal(t, "existing-user", username)
	assert.Equal(t, "new-password", password)
}

func TestExecuteCommand_UpdateMinerPassword_InsertsMissingProtoCredentials(t *testing.T) {
	svc, db, encryptSvc, dbDeviceID, deviceIdentifier := setupPasswordCommandDevice(t, models.DriverNameProto, false)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice := sdkMocks.NewMockDevice(ctrl)
	mockDevice.EXPECT().
		UpdateMinerPassword(gomock.Any(), "old-password", "new-password").
		Return(nil)

	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockDriver.EXPECT().
		NewDevice(gomock.Any(), deviceIdentifier, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ sdk.DeviceInfo, secret sdk.SecretBundle) (sdk.NewDeviceResult, error) {
			userPass, ok := secret.Kind.(sdk.UsernamePassword)
			require.True(t, ok, "expected password-update resolver to synthesize username/password secret")
			assert.Equal(t, models.ProtoDefaultUsername, userPass.Username)
			assert.Equal(t, "old-password", userPass.Password)
			return sdk.NewDeviceResult{Device: mockDevice}, nil
		})

	filesSvc, err := files.NewService(files.Config{})
	require.NoError(t, err)
	svc.minerService = minerDomain.NewMinerService(
		db,
		sqlstores.NewSQLUserStore(db),
		encryptSvc,
		filesSvc,
		&commandTestPluginManager{driverName: models.DriverNameProto, driver: mockDriver},
	)
	svc.deviceStore = sqlstores.NewSQLDeviceStore(db)

	_, _, err = svc.executeCommandOnDevice(t.Context(), commandtype.UpdateMinerPassword, passwordUpdateMessage(t, dbDeviceID))

	require.NoError(t, err)
	username, password := storedCredentials(t, db, encryptSvc, dbDeviceID)
	assert.Equal(t, models.ProtoDefaultUsername, username)
	assert.Equal(t, "new-password", password)
}

func TestExecuteCommand_UpdateMinerPassword_DefaultPasswordDevicePersistsAndPairs(t *testing.T) {
	svc, db, encryptSvc, dbDeviceID, deviceIdentifier := setupPasswordCommandDeviceWithStatus(
		t,
		models.DriverNameProto,
		true,
		sqlc.PairingStatusEnumDEFAULTPASSWORD,
	)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDevice := sdkMocks.NewMockDevice(ctrl)
	mockDevice.EXPECT().
		UpdateMinerPassword(gomock.Any(), "old-password", "new-password").
		Return(nil)

	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockDriver.EXPECT().
		NewDevice(gomock.Any(), deviceIdentifier, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ sdk.DeviceInfo, secret sdk.SecretBundle) (sdk.NewDeviceResult, error) {
			userPass, ok := secret.Kind.(sdk.UsernamePassword)
			require.True(t, ok, "expected username/password secret for persisted default-password credentials")
			assert.Equal(t, "existing-user", userPass.Username)
			assert.Equal(t, "old-password", userPass.Password)
			return sdk.NewDeviceResult{Device: mockDevice}, nil
		})

	filesSvc, err := files.NewService(files.Config{})
	require.NoError(t, err)
	svc.minerService = minerDomain.NewMinerService(
		db,
		sqlstores.NewSQLUserStore(db),
		encryptSvc,
		filesSvc,
		&commandTestPluginManager{driverName: models.DriverNameProto, driver: mockDriver},
	)
	svc.deviceStore = sqlstores.NewSQLDeviceStore(db)

	_, _, err = svc.executeCommandOnDevice(t.Context(), commandtype.UpdateMinerPassword, passwordUpdateMessage(t, dbDeviceID))

	require.NoError(t, err)
	username, password := storedCredentials(t, db, encryptSvc, dbDeviceID)
	assert.Equal(t, "existing-user", username)
	assert.Equal(t, "new-password", password)
	pairingStatus, err := sqlc.New(db).GetDevicePairingStatusByDeviceDatabaseID(t.Context(), dbDeviceID)
	require.NoError(t, err)
	assert.Equal(t, sqlc.PairingStatusEnumPAIRED, pairingStatus)
}

func TestExecuteCommand_UpdateMinerPassword_FleetNodeMinerPersistsNodeEncryptedCredentials(t *testing.T) {
	svc, db, encryptSvc, dbDeviceID, deviceIdentifier := setupPasswordCommandDeviceWithStatus(
		t,
		models.DriverNameProto,
		false,
		sqlc.PairingStatusEnumDEFAULTPASSWORD,
	)
	bindCommandTestFleetNodeDevice(t, db, dbDeviceID)
	oldUsername := fleetNodeCredentialBlobForTest("old-user")
	oldPassword := fleetNodeCredentialBlobForTest("old-pass")
	storeCommandTestFleetNodeCredentials(t, db, dbDeviceID, oldUsername, oldPassword)
	updatedUsername := fleetNodeCredentialBlobForTest("user")
	updatedPassword := fleetNodeCredentialBlobForTest("pass")
	resultPayload, err := proto.Marshal(&gatewaypb.UpdateMinerPasswordResult{
		EncryptedCredentials: &gatewaypb.EncryptedCredentials{
			Username: updatedUsername,
			Password: updatedPassword,
		},
	})
	require.NoError(t, err)

	sender := &commandTestFleetNodeSender{
		ack: &gatewaypb.ControlAck{
			Succeeded: true,
			Code:      gatewaypb.AckCode_ACK_CODE_OK,
			Payload:   resultPayload,
		},
	}
	svc.deviceStore = sqlstores.NewSQLDeviceStore(db)
	filesSvc, err := files.NewService(files.Config{})
	require.NoError(t, err)
	svc.minerService = minerDomain.NewMinerService(
		db,
		sqlstores.NewSQLUserStore(db),
		encryptSvc,
		filesSvc,
		&commandTestPluginManager{driverName: models.DriverNameProto},
	).WithCommandSender(sender)

	_, _, err = svc.executeCommandOnDevice(t.Context(), commandtype.UpdateMinerPassword, encryptedPasswordUpdateMessage(t, dbDeviceID))

	require.NoError(t, err)
	assert.True(t, sender.called)
	var env gatewaypb.AgentCommand
	require.NoError(t, proto.Unmarshal(sender.cmd.GetPayload(), &env))
	action := env.GetMinerCommand().GetUpdateMinerPassword()
	require.NotNil(t, action)
	assert.Equal(t, passwordupdate.Algorithm, action.GetEncryptedPasswordUpdate().GetAlgorithm())
	target := env.GetMinerCommand().GetTarget()
	require.NotNil(t, target)
	assert.Equal(t, oldUsername, target.GetCredentialUsername())
	assert.Equal(t, oldPassword, target.GetCredentialPassword())

	username, password := storedCredentialCiphertext(t, db, dbDeviceID)
	assert.Equal(t, base64.StdEncoding.EncodeToString(updatedUsername), username)
	assert.Equal(t, base64.StdEncoding.EncodeToString(updatedPassword), password)
	pairingStatus, err := sqlc.New(db).GetDevicePairingStatusByDeviceDatabaseID(t.Context(), dbDeviceID)
	require.NoError(t, err)
	assert.Equal(t, sqlc.PairingStatusEnumPAIRED, pairingStatus)

	resolveSender := &commandTestFleetNodeSender{}
	minerSvc := minerDomain.NewMinerService(
		db,
		sqlstores.NewSQLUserStore(db),
		encryptSvc,
		filesSvc,
		&commandTestPluginManager{driverName: models.DriverNameProto},
	).WithCommandSender(resolveSender)

	resolved, err := minerSvc.GetMinerFromDeviceIdentifier(t.Context(), models.DeviceIdentifier(deviceIdentifier))
	require.NoError(t, err)
	require.NoError(t, resolved.Reboot(t.Context()))

	var resolvedEnv gatewaypb.AgentCommand
	require.NoError(t, proto.Unmarshal(resolveSender.cmd.GetPayload(), &resolvedEnv))
	resolvedTarget := resolvedEnv.GetMinerCommand().GetTarget()
	require.NotNil(t, resolvedTarget)
	assert.Equal(t, updatedUsername, resolvedTarget.GetCredentialUsername())
	assert.Equal(t, updatedPassword, resolvedTarget.GetCredentialPassword())
}

func TestExecuteCommand_UpdateMinerPassword_RejectsMalformedNodeEncryptedCredentials(t *testing.T) {
	svc, db, encryptSvc, dbDeviceID, _ := setupPasswordCommandDeviceWithStatus(
		t,
		models.DriverNameProto,
		false,
		sqlc.PairingStatusEnumDEFAULTPASSWORD,
	)
	bindCommandTestFleetNodeDevice(t, db, dbDeviceID)
	oldUsername := fleetNodeCredentialBlobForTest("old-user")
	oldPassword := fleetNodeCredentialBlobForTest("old-pass")
	storeCommandTestFleetNodeCredentials(t, db, dbDeviceID, oldUsername, oldPassword)
	resultPayload, err := proto.Marshal(&gatewaypb.UpdateMinerPasswordResult{
		EncryptedCredentials: &gatewaypb.EncryptedCredentials{
			Username: []byte("plain-user"),
			Password: []byte("plain-pass"),
		},
	})
	require.NoError(t, err)

	sender := &commandTestFleetNodeSender{
		ack: &gatewaypb.ControlAck{
			Succeeded: true,
			Code:      gatewaypb.AckCode_ACK_CODE_OK,
			Payload:   resultPayload,
		},
	}
	svc.deviceStore = sqlstores.NewSQLDeviceStore(db)
	filesSvc, err := files.NewService(files.Config{})
	require.NoError(t, err)
	svc.minerService = minerDomain.NewMinerService(
		db,
		sqlstores.NewSQLUserStore(db),
		encryptSvc,
		filesSvc,
		&commandTestPluginManager{driverName: models.DriverNameProto},
	).WithCommandSender(sender)

	_, _, err = svc.executeCommandOnDevice(t.Context(), commandtype.UpdateMinerPassword, encryptedPasswordUpdateMessage(t, dbDeviceID))

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "malformed encrypted credentials")
	username, password := storedCredentialCiphertext(t, db, dbDeviceID)
	assert.Equal(t, base64.StdEncoding.EncodeToString(oldUsername), username)
	assert.Equal(t, base64.StdEncoding.EncodeToString(oldPassword), password)
	pairingStatus, err := sqlc.New(db).GetDevicePairingStatusByDeviceDatabaseID(t.Context(), dbDeviceID)
	require.NoError(t, err)
	assert.Equal(t, sqlc.PairingStatusEnumDEFAULTPASSWORD, pairingStatus)
}

func TestExecuteCommand_UpdateMinerPassword_ResolverAuthErrorFailsPrecondition(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	const dbDeviceID int64 = 124

	mockMinerGetter := commandMocks.NewMockCachedMinerGetter(ctrl)
	svc := &ExecutionService{minerService: mockMinerGetter}
	mockMinerGetter.EXPECT().
		GetMinerForPasswordUpdate(gomock.Any(), dbDeviceID, "old-password").
		Return(nil, fleeterror.NewUnauthenticatedError("bad current password"))

	_, _, err := svc.executeCommandOnDevice(t.Context(), commandtype.UpdateMinerPassword, passwordUpdateMessage(t, dbDeviceID))

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "expected FailedPrecondition, got %v", err)
	assert.Contains(t, err.Error(), "bad current password")
}

func TestExecuteCommand_UpdateMinerPassword_ActionAuthErrorFailsPrecondition(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	const dbDeviceID int64 = 125
	const deviceIdentifier = "proto-auth-action"

	mockMinerGetter := commandMocks.NewMockCachedMinerGetter(ctrl)
	mockMiner := minerIfaceMocks.NewMockMiner(ctrl)
	svc := &ExecutionService{minerService: mockMinerGetter}

	mockMinerGetter.EXPECT().GetMinerForPasswordUpdate(gomock.Any(), dbDeviceID, "old-password").Return(mockMiner, nil)
	mockMiner.EXPECT().GetOrgID().Return(int64(1)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetID().Return(models.DeviceIdentifier(deviceIdentifier)).AnyTimes()
	mockMiner.EXPECT().UpdateMinerPassword(gomock.Any(), dto.UpdateMinerPasswordPayload{
		CurrentPassword: "old-password",
		NewPassword:     "new-password",
	}).Return(fleeterror.NewUnauthenticatedError("bad current password"))

	_, _, err := svc.executeCommandOnDevice(t.Context(), commandtype.UpdateMinerPassword, passwordUpdateMessage(t, dbDeviceID))

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "expected FailedPrecondition, got %v", err)
	assert.Contains(t, err.Error(), "bad current password")
}

func TestExecuteCommand_UpdateMinerPassword_MissingNonProtoCredentialsFails(t *testing.T) {
	svc, _, _, dbDeviceID, deviceIdentifier := setupPasswordCommandDevice(t, "antminer", false)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockQueue := queueMocks.NewMockMessageQueue(ctrl)
	mockMinerGetter := commandMocks.NewMockCachedMinerGetter(ctrl)
	mockMiner := minerIfaceMocks.NewMockMiner(ctrl)
	mockDeviceStore := storeMocks.NewMockDeviceStore(ctrl)
	svc.messageQueue = mockQueue
	svc.minerService = mockMinerGetter
	svc.deviceStore = mockDeviceStore

	mockMinerGetter.EXPECT().GetMinerForPasswordUpdate(gomock.Any(), dbDeviceID, "old-password").Return(mockMiner, nil)
	mockMiner.EXPECT().GetOrgID().Return(int64(1)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetDriverName().Return("antminer").AnyTimes()
	mockMiner.EXPECT().GetID().Return(models.DeviceIdentifier(deviceIdentifier)).AnyTimes()
	mockMiner.EXPECT().UpdateMinerPassword(gomock.Any(), dto.UpdateMinerPasswordPayload{
		CurrentPassword: "old-password",
		NewPassword:     "new-password",
	}).Return(nil)
	mockMinerGetter.EXPECT().InvalidateMiner(models.DeviceIdentifier(deviceIdentifier))

	_, _, err := svc.executeCommandOnDevice(t.Context(), commandtype.UpdateMinerPassword, passwordUpdateMessage(t, dbDeviceID))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "credential persistence failed")
}

func setupPasswordCommandDevice(t *testing.T, driverName string, withCredentials bool) (*ExecutionService, *sql.DB, *encrypt.Service, int64, string) {
	return setupPasswordCommandDeviceWithStatus(t, driverName, withCredentials, sqlc.PairingStatusEnumPAIRED)
}

func setupPasswordCommandDeviceWithStatus(t *testing.T, driverName string, withCredentials bool, pairingStatus sqlc.PairingStatusEnum) (*ExecutionService, *sql.DB, *encrypt.Service, int64, string) {
	t.Helper()

	conn := newCommandTestDB(t)
	_, err := conn.ExecContext(t.Context(), `
		INSERT INTO organization (id, org_id, name)
		VALUES (1, 'test-org-1', 'Test Organization 1')
		ON CONFLICT DO NOTHING
	`)
	require.NoError(t, err)

	encryptSvc, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: testServiceMasterKey})
	require.NoError(t, err)

	q := sqlc.New(conn)
	deviceIdentifier := "password-command-" + driverName
	discoveredID, err := q.UpsertDiscoveredDevice(t.Context(), sqlc.UpsertDiscoveredDeviceParams{
		OrgID:            1,
		DeviceIdentifier: "discovered-" + deviceIdentifier,
		Model:            sql.NullString{String: "TestMiner", Valid: true},
		Manufacturer:     sql.NullString{String: "TestCorp", Valid: true},
		DriverName:       driverName,
		IpAddress:        "192.0.2.10",
		Port:             "443",
		UrlScheme:        "https",
		IsActive:         true,
	})
	require.NoError(t, err)
	dbDeviceID, err := q.InsertDevice(t.Context(), sqlc.InsertDeviceParams{
		OrgID:              1,
		DiscoveredDeviceID: discoveredID,
		DeviceIdentifier:   deviceIdentifier,
		MacAddress:         "00:11:22:33:44:55",
		SerialNumber:       sql.NullString{String: "SN-" + deviceIdentifier, Valid: true},
	})
	require.NoError(t, err)
	_, err = q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{
		DeviceID:      dbDeviceID,
		PairingStatus: pairingStatus,
	})
	require.NoError(t, err)

	if withCredentials {
		usernameEnc, err := encryptSvc.Encrypt([]byte("existing-user"))
		require.NoError(t, err)
		passwordEnc, err := encryptSvc.Encrypt([]byte("old-password"))
		require.NoError(t, err)
		err = q.UpsertMinerCredentials(t.Context(), sqlc.UpsertMinerCredentialsParams{
			DeviceID:    dbDeviceID,
			UsernameEnc: usernameEnc,
			PasswordEnc: passwordEnc,
		})
		require.NoError(t, err)
	}

	svc := NewExecutionService(&Config{
		MaxWorkers: 1,
	}, conn, nil, encryptSvc, nil, nil, nil, nil, nil)
	return svc, conn, encryptSvc, dbDeviceID, deviceIdentifier
}

type commandTestPluginManager struct {
	driverName string
	driver     sdk.Driver
}

func (m *commandTestPluginManager) HasPluginForDriverName(driverName string) bool {
	return driverName == m.driverName
}

func (m *commandTestPluginManager) GetCapabilitiesForDriverName(string) sdk.Capabilities {
	return sdk.Capabilities{}
}

func (m *commandTestPluginManager) GetDriverByDriverName(driverName string) (sdk.Driver, error) {
	if driverName == m.driverName {
		return m.driver, nil
	}
	return nil, fmt.Errorf("no driver registered for %s", driverName)
}

type commandTestFleetNodeSender struct {
	called bool
	cmd    *gatewaypb.ControlCommand
	ack    *gatewaypb.ControlAck
}

func (s *commandTestFleetNodeSender) SendCommand(_ context.Context, _ int64, cmd *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
	s.called = true
	s.cmd = cmd
	if s.ack != nil {
		return s.ack, nil
	}
	return &gatewaypb.ControlAck{Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK}, nil
}

func newCommandTestDB(t *testing.T) *sql.DB {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping database-backed credential command test in short mode")
	}

	cli := struct {
		DB infraDB.Config `envprefix:"DB_" embed:""`
	}{}
	parser, err := kong.New(&cli)
	require.NoError(t, err)
	_, err = parser.Parse(nil)
	require.NoError(t, err)

	config := cli.DB
	dbName := commandTestDBName(t.Name())

	adminConfig := config
	adminConfig.Name = "postgres"
	adminConn, err := infraDB.ConnectToDatabase(&adminConfig)
	require.NoError(t, err)
	recreateCommandTestDatabase(t, adminConn, dbName)
	require.NoError(t, adminConn.Close())

	testDBConfig := config
	testDBConfig.Name = dbName
	conn, err := connectAndMigrateCommandTestDB(t, &testDBConfig, &adminConfig, dbName)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, conn.Close())
		adminConn, err := infraDB.ConnectToDatabase(&adminConfig)
		require.NoError(t, err)
		defer adminConn.Close()
		dropCommandTestDatabase(t, context.Background(), adminConn, dbName)
	})

	return conn
}

func connectAndMigrateCommandTestDB(
	t *testing.T,
	testDBConfig *infraDB.Config,
	adminConfig *infraDB.Config,
	dbName string,
) (*sql.DB, error) {
	t.Helper()

	var conn *sql.DB
	var lastErr error
	for attempt := 1; attempt <= 5; attempt++ {
		conn, lastErr = infraDB.ConnectAndMigrate(testDBConfig)
		if lastErr == nil {
			return conn, nil
		}
		if !isRetryableCommandMigrationError(lastErr) || attempt == 5 {
			return nil, lastErr
		}

		t.Logf("migration deadlock (attempt %d/5), retrying: %v", attempt, lastErr)
		adminConn, err := infraDB.ConnectToDatabase(adminConfig)
		require.NoError(t, err)
		recreateCommandTestDatabase(t, adminConn, dbName)
		require.NoError(t, adminConn.Close())
		time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
	}

	return nil, lastErr
}

func isRetryableCommandMigrationError(err error) bool {
	if infraDB.IsRetryablePostgresError(err) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, infraDB.PGDeadlockDetected) || strings.Contains(msg, infraDB.PGSerializationFailure)
}

func recreateCommandTestDatabase(t *testing.T, conn *sql.DB, dbName string) {
	t.Helper()
	dropCommandTestDatabase(t, t.Context(), conn, dbName)
	_, err := conn.ExecContext(t.Context(), fmt.Sprintf("CREATE DATABASE %s", dbName))
	require.NoError(t, err)
}

func dropCommandTestDatabase(t *testing.T, ctx context.Context, conn *sql.DB, dbName string) {
	t.Helper()
	_, _ = conn.ExecContext(ctx, fmt.Sprintf(`
		SELECT pg_terminate_backend(pg_stat_activity.pid)
		FROM pg_stat_activity
		WHERE pg_stat_activity.datname = '%s'
		AND pid <> pg_backend_pid()
	`, dbName))
	_, err := conn.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	require.NoError(t, err)
}

func commandTestDBName(testName string) string {
	sanitizedName := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			return r
		}
		return '_'
	}, testName)
	sanitizedName = strings.ToLower(sanitizedName)

	if len(sanitizedName) > 46 {
		sanitizedName = sanitizedName[:46]
	}

	return fmt.Sprintf("fleet_test_%s_%04x", sanitizedName, time.Now().UnixNano()&0xFFFF)
}

func wirePasswordCommandMocks(t *testing.T, ctrl *gomock.Controller, svc *ExecutionService, dbDeviceID int64, deviceIdentifier string, driverName string) {
	t.Helper()

	mockQueue := queueMocks.NewMockMessageQueue(ctrl)
	mockMinerGetter := commandMocks.NewMockCachedMinerGetter(ctrl)
	mockMiner := minerIfaceMocks.NewMockMiner(ctrl)
	mockDeviceStore := storeMocks.NewMockDeviceStore(ctrl)
	svc.messageQueue = mockQueue
	svc.minerService = mockMinerGetter
	svc.deviceStore = mockDeviceStore

	mockMinerGetter.EXPECT().GetMinerForPasswordUpdate(gomock.Any(), dbDeviceID, "old-password").Return(mockMiner, nil)
	mockMiner.EXPECT().GetOrgID().Return(int64(1)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetDriverName().Return(driverName).AnyTimes()
	mockMiner.EXPECT().GetID().Return(models.DeviceIdentifier(deviceIdentifier)).AnyTimes()
	mockMiner.EXPECT().UpdateMinerPassword(gomock.Any(), dto.UpdateMinerPasswordPayload{
		CurrentPassword: "old-password",
		NewPassword:     "new-password",
	}).Return(nil)
	mockDeviceStore.EXPECT().
		UpdateDevicePairingStatusByIdentifier(gomock.Any(), deviceIdentifier, string(sqlc.PairingStatusEnumPAIRED)).
		Return(nil)
	mockMinerGetter.EXPECT().InvalidateMiner(models.DeviceIdentifier(deviceIdentifier))
}

func passwordUpdateMessage(t *testing.T, dbDeviceID int64) queue.Message {
	t.Helper()
	payload, err := json.Marshal(dto.UpdateMinerPasswordPayload{
		CurrentPassword: "old-password",
		NewPassword:     "new-password",
	})
	require.NoError(t, err)
	return queue.Message{ID: 7, DeviceID: dbDeviceID, CommandType: commandtype.UpdateMinerPassword, Payload: payload}
}

func encryptedPasswordUpdateMessage(t *testing.T, dbDeviceID int64) queue.Message {
	t.Helper()
	payload, err := json.Marshal(dto.UpdateMinerPasswordPayload{
		EncryptedPasswordUpdate: &dto.NodeEncryptedPayload{
			Algorithm:       passwordupdate.Algorithm,
			EphemeralPubkey: []byte("12345678901234567890123456789012"),
			Nonce:           []byte("123456789012"),
			Ciphertext:      []byte("12345678901234567"),
		},
	})
	require.NoError(t, err)
	return queue.Message{ID: 7, DeviceID: dbDeviceID, CommandType: commandtype.UpdateMinerPassword, Payload: payload}
}

func storedCredentials(t *testing.T, db *sql.DB, encryptSvc *encrypt.Service, dbDeviceID int64) (string, string) {
	t.Helper()
	creds, err := sqlc.New(db).GetMinerCredentialsByDeviceID(t.Context(), dbDeviceID)
	require.NoError(t, err)
	username, err := encryptSvc.Decrypt(creds.UsernameEnc)
	require.NoError(t, err)
	password, err := encryptSvc.Decrypt(creds.PasswordEnc)
	require.NoError(t, err)
	return string(username), string(password)
}

func storedCredentialCiphertext(t *testing.T, db *sql.DB, dbDeviceID int64) (string, string) {
	t.Helper()
	creds, err := sqlc.New(db).GetMinerCredentialsByDeviceID(t.Context(), dbDeviceID)
	require.NoError(t, err)
	return creds.UsernameEnc, creds.PasswordEnc
}

func bindCommandTestFleetNodeDevice(t *testing.T, db *sql.DB, dbDeviceID int64) {
	t.Helper()

	var fleetNodeID int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		INSERT INTO fleet_node (org_id, name, identity_pubkey, encryption_pubkey, enrollment_status)
		VALUES (1, $1, $2, $3, 'CONFIRMED')
		RETURNING id`,
		fmt.Sprintf("password-command-node-%d", dbDeviceID),
		[]byte(fmt.Sprintf("identity-pubkey-%d", dbDeviceID)),
		[]byte("01234567890123456789012345678901"),
	).Scan(&fleetNodeID))
	_, err := db.ExecContext(t.Context(), `
		INSERT INTO fleet_node_device (fleet_node_id, device_id, org_id)
		VALUES ($1, $2, 1)`,
		fleetNodeID,
		dbDeviceID,
	)
	require.NoError(t, err)
}

func storeCommandTestFleetNodeCredentials(t *testing.T, db *sql.DB, dbDeviceID int64, username, password []byte) {
	t.Helper()

	err := sqlc.New(db).UpsertMinerCredentials(t.Context(), sqlc.UpsertMinerCredentialsParams{
		DeviceID:    dbDeviceID,
		UsernameEnc: base64.StdEncoding.EncodeToString(username),
		PasswordEnc: base64.StdEncoding.EncodeToString(password),
	})
	require.NoError(t, err)
}

func fleetNodeCredentialBlobForTest(label string) []byte {
	blob := make([]byte, 0, 1+len("PFNC")+12+len(label)+16)
	blob = append(blob, byte(1))
	blob = append(blob, []byte("PFNC")...)
	blob = append(blob, []byte("nonce-000001")...)
	blob = append(blob, []byte(label)...)
	blob = append(blob, []byte("test-tag-16-byte")...)
	return blob
}
