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

	settings, err := mqttingest.NewSettingsService(mqttingest.SettingsServiceConfig{
		Store:  &handlerMqttSettingsStore{},
		Cipher: &handlerMqttCipher{},
	})
	require.NoError(t, err)
	h := NewHandler(nil, settings)

	resp, err := h.CreateMqttCurtailmentSource(
		sessionCtxWithPerms(42, authz.PermCurtailmentManage),
		connect.NewRequest(&pb.CreateMqttCurtailmentSourceRequest{
			SourceName:              "maestro",
			Topic:                   "maestro/curtailment",
			BrokerPrimaryHost:       "10.0.0.1",
			BrokerSecondaryHost:     "10.0.0.2",
			MqttUsername:            "operator",
			MqttPassword:            "secret",
			CurtailMode:             "FULL_FLEET",
			PayloadFormat:           "target_timestamp",
			StalenessThresholdSec:   240,
			MinCurtailedDurationSec: 600,
			ServiceUserId:           99,
			Scope: &pb.MqttCurtailmentSourceScope{
				Type: pb.MqttCurtailmentSourceScopeType_MQTT_CURTAILMENT_SOURCE_SCOPE_TYPE_WHOLE_ORG,
			},
		}),
	)
	require.NoError(t, err)

	source := resp.Msg.GetSource()
	require.NotNil(t, source)
	assert.True(t, source.GetHasPassword())
	assert.Equal(t, "operator", source.GetMqttUsername())
	assert.False(t, source.GetEnabled(), "create defaults disabled unless enabled=true is explicitly sent")
}

type handlerMqttSettingsStore struct{}

func (handlerMqttSettingsStore) ListSourceConfigsByOrg(context.Context, int64) ([]mqttingest.SourceConfig, error) {
	panic("not used")
}

func (handlerMqttSettingsStore) ListSourceStatesByOrg(context.Context, int64) ([]mqttingest.SourceState, error) {
	return nil, nil
}

func (handlerMqttSettingsStore) GetSourceConfigByOrg(context.Context, int64, int64) (mqttingest.SourceConfig, error) {
	panic("not used")
}

func (handlerMqttSettingsStore) CreateSourceConfig(_ context.Context, source mqttingest.SourceConfig) (mqttingest.SourceConfig, error) {
	source.ID = 11
	source.CreatedAt = time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	source.UpdatedAt = source.CreatedAt
	return source, nil
}

func (handlerMqttSettingsStore) UpdateSourceConfig(context.Context, mqttingest.SourceConfig) (mqttingest.SourceConfig, error) {
	panic("not used")
}

func (handlerMqttSettingsStore) SetSourceConfigEnabled(context.Context, int64, int64, bool) (mqttingest.SourceConfig, error) {
	panic("not used")
}

func (handlerMqttSettingsStore) DeleteDisabledSourceConfig(context.Context, int64, int64) error {
	panic("not used")
}

func (handlerMqttSettingsStore) UserCanIngestCurtailment(context.Context, int64, int64) (bool, error) {
	return true, nil
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
