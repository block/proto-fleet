package curtailment

import (
	"context"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	domainAuth "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/mqttingest"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestHandler_MqttSettingsRequireManage(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil)
	_, err := h.ListMqttCurtailmentSources(
		sessionCtxWithPerms(42, authz.PermCurtailmentRead),
		connect.NewRequest(&pb.ListMqttCurtailmentSourcesRequest{}),
	)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

func TestHandler_CreateMqttCurtailmentSourceReturnsRedactedPassword(t *testing.T) {
	t.Parallel()

	store := &handlerMqttSettingsStore{}
	settings, err := mqttingest.NewSettingsService(mqttingest.SettingsServiceConfig{
		Store:  store,
		Cipher: &handlerMqttCipher{},
	})
	require.NoError(t, err)
	h := NewHandler(nil, settings)

	resp, err := h.CreateMqttCurtailmentSource(
		startSessionCtxWithPerms(t, 42, domainAuth.AdminRoleName, authz.PermCurtailmentManage),
		connect.NewRequest(&pb.CreateMqttCurtailmentSourceRequest{
			SourceName:            "maestro",
			Topic:                 "maestro/target",
			BrokerPrimaryHost:     "10.0.0.1",
			BrokerSecondaryHost:   "10.0.0.2",
			MqttUsername:          "operator",
			MqttPassword:          "secret",
			PayloadFormat:         "target_timestamp",
			StalenessThresholdSec: 240,
		}),
	)
	require.NoError(t, err)

	source := resp.Msg.GetSource()
	require.NotNil(t, source)
	assert.True(t, source.GetHasPassword())
	assert.Equal(t, "operator", source.GetMqttUsername())
	assert.True(t, source.GetEnabled())
	require.NotNil(t, store.created)
	assert.Equal(t, int64(9), store.created.ServiceUserID)
}

func TestHandler_CreateMqttCurtailmentSourceRequiresAdmin(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil)
	_, err := h.CreateMqttCurtailmentSource(
		sessionCtxWithPerms(42, authz.PermCurtailmentManage),
		connect.NewRequest(&pb.CreateMqttCurtailmentSourceRequest{}),
	)

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

func TestHandler_UpdateMqttCurtailmentSourceRequiresAdmin(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil)
	_, err := h.UpdateMqttCurtailmentSource(
		sessionCtxWithPerms(42, authz.PermCurtailmentManage),
		connect.NewRequest(&pb.UpdateMqttCurtailmentSourceRequest{SourceId: 11}),
	)

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

func TestHandler_EnableMqttCurtailmentSourceRequiresAdmin(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil)
	_, err := h.SetMqttCurtailmentSourceEnabled(
		sessionCtxWithPerms(42, authz.PermCurtailmentManage),
		connect.NewRequest(&pb.SetMqttCurtailmentSourceEnabledRequest{SourceId: 11, Enabled: true}),
	)

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

func TestHandler_DisableMqttCurtailmentSourceRequiresAdmin(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil)
	_, err := h.SetMqttCurtailmentSourceEnabled(
		sessionCtxWithPerms(42, authz.PermCurtailmentManage),
		connect.NewRequest(&pb.SetMqttCurtailmentSourceEnabledRequest{SourceId: 11, Enabled: false}),
	)

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

func TestHandler_DeleteMqttCurtailmentSourceAllowsManagePermission(t *testing.T) {
	t.Parallel()

	store := &handlerMqttSettingsStore{}
	settings, err := mqttingest.NewSettingsService(mqttingest.SettingsServiceConfig{
		Store:  store,
		Cipher: &handlerMqttCipher{},
	})
	require.NoError(t, err)
	h := NewHandler(nil, settings)

	_, err = h.DeleteMqttCurtailmentSource(
		sessionCtxWithPerms(42, authz.PermCurtailmentManage),
		connect.NewRequest(&pb.DeleteMqttCurtailmentSourceRequest{SourceId: 11}),
	)

	require.NoError(t, err)
	assert.Equal(t, int64(42), store.deletedOrgID)
	assert.Equal(t, int64(11), store.deletedSourceID)
}

func TestHandler_TestMqttCurtailmentSourceConnectionReturnsBrokerResults(t *testing.T) {
	t.Parallel()

	tester := &handlerMqttConnectionTester{
		out: mqttingest.TestSourceConnectionResult{Results: []mqttingest.BrokerConnectionResult{
			{
				Broker:     "10.0.0.1",
				Role:       mqttingest.BrokerPrimary,
				Connected:  true,
				Subscribed: true,
			},
			{
				Broker:    "10.0.0.2",
				Role:      mqttingest.BrokerSecondary,
				Connected: true,
				Error:     "not authorized",
			},
		}},
	}
	settings, err := mqttingest.NewSettingsService(mqttingest.SettingsServiceConfig{
		Store:            &handlerMqttSettingsStore{},
		Cipher:           &handlerMqttCipher{},
		ConnectionTester: tester,
	})
	require.NoError(t, err)
	h := NewHandler(nil, settings)

	resp, err := h.TestMqttCurtailmentSourceConnection(
		startSessionCtxWithPerms(t, 42, domainAuth.AdminRoleName, authz.PermCurtailmentManage),
		connect.NewRequest(&pb.TestMqttCurtailmentSourceConnectionRequest{
			Topic:               "maestro/target",
			BrokerPrimaryHost:   "10.0.0.1",
			BrokerSecondaryHost: "10.0.0.2",
			MqttUsername:        "operator",
			MqttPassword:        "secret",
			PayloadFormat:       "target_timestamp",
		}),
	)
	require.NoError(t, err)

	assert.False(t, resp.Msg.GetOk())
	require.Len(t, resp.Msg.GetResults(), 2)
	assert.Equal(t, "primary", resp.Msg.GetResults()[0].GetBrokerRole())
	assert.True(t, resp.Msg.GetResults()[0].GetSubscribed())
	assert.Equal(t, "secondary", resp.Msg.GetResults()[1].GetBrokerRole())
	assert.Equal(t, "not authorized", resp.Msg.GetResults()[1].GetError())

	assert.Equal(t, int64(42), tester.req.Source.OrganizationID)
	assert.Equal(t, int64(9), tester.req.Source.ServiceUserID)
	assert.Equal(t, defaultMqttTestBrokerPort(), tester.req.Source.BrokerPort)
	assert.Equal(t, "operator", tester.req.Source.MQTTUsername)
	assert.Equal(t, "secret", tester.req.PlaintextPassword)
}

func TestHandler_TestMqttCurtailmentSourceConnectionRequiresAdmin(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil)
	_, err := h.TestMqttCurtailmentSourceConnection(
		sessionCtxWithPerms(42, authz.PermCurtailmentManage),
		connect.NewRequest(&pb.TestMqttCurtailmentSourceConnectionRequest{}),
	)

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

type handlerMqttSettingsStore struct {
	created         *mqttingest.SourceConfig
	deletedOrgID    int64
	deletedSourceID int64
}

func (*handlerMqttSettingsStore) ListSourceConfigsByOrg(context.Context, int64) ([]mqttingest.SourceConfig, error) {
	panic("not used")
}

func (*handlerMqttSettingsStore) ListSourceStatesByOrg(context.Context, int64) ([]mqttingest.SourceState, error) {
	return nil, nil
}

func (*handlerMqttSettingsStore) GetSourceConfigByOrg(context.Context, int64, int64) (mqttingest.SourceConfig, error) {
	panic("not used")
}

func (s *handlerMqttSettingsStore) CreateSourceConfig(_ context.Context, source mqttingest.SourceConfig) (mqttingest.SourceConfig, error) {
	s.created = &source
	source.ID = 11
	source.CreatedAt = time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	source.UpdatedAt = source.CreatedAt
	return source, nil
}

func (*handlerMqttSettingsStore) UpdateSourceConfig(context.Context, mqttingest.SourceConfig) (mqttingest.SourceConfig, error) {
	panic("not used")
}

func (*handlerMqttSettingsStore) SetSourceConfigEnabled(context.Context, int64, int64, bool) (mqttingest.SourceConfig, error) {
	panic("not used")
}

func (s *handlerMqttSettingsStore) DeleteDisabledSourceConfig(_ context.Context, orgID, sourceID int64) error {
	s.deletedOrgID = orgID
	s.deletedSourceID = sourceID
	return nil
}

func (*handlerMqttSettingsStore) CountAutomationRulesByMQTTSource(context.Context, int64, int64) (int64, error) {
	return 0, nil
}

type handlerMqttCipher struct{}

func (handlerMqttCipher) Encrypt(plaintext []byte) (string, error) {
	return "enc:" + string(plaintext), nil
}

func (handlerMqttCipher) Decrypt(encrypted string) ([]byte, error) {
	if len(encrypted) < 4 || encrypted[:4] != "enc:" {
		return nil, fmt.Errorf("unexpected ciphertext")
	}
	return []byte(encrypted[4:]), nil
}

type handlerMqttConnectionTester struct {
	req mqttingest.TestSourceConnectionRequest
	out mqttingest.TestSourceConnectionResult
	err error
}

func (h *handlerMqttConnectionTester) TestConnection(_ context.Context, req mqttingest.TestSourceConnectionRequest) (mqttingest.TestSourceConnectionResult, error) {
	h.req = req
	return h.out, h.err
}

func defaultMqttTestBrokerPort() int32 {
	return 1883
}
