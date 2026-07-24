package command

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/sv2/translator"
	"github.com/block/proto-fleet/server/sdk/v1"
)

const translationTestSV2URL = "stratum2+tcp://pool.example.com:34254/9bXiEd8boQVhq7WddEcERUL5tyyJVFYdU8th3HfbNXK3Yw6GRXh"

type translationTestCapabilities struct {
	nativeDrivers map[string]bool
}

func (f translationTestCapabilities) GetRawCapabilitiesForDevice(
	_ context.Context,
	driverName, _, _ string,
) sdk.Capabilities {
	return sdk.Capabilities{
		sdk.CapabilityNativeStratumV2: f.nativeDrivers[driverName],
	}
}

type translationTestManager struct {
	endpoint   translator.Endpoint
	profile    *translator.Profile
	assignment translator.Assignment
	err        error
	calls      int
}

func (f *translationTestManager) ApplyAssignment(
	_ context.Context,
	profile *translator.Profile,
	assignment translator.Assignment,
) (translator.Endpoint, error) {
	f.calls++
	f.profile = profile
	f.assignment = assignment
	return f.endpoint, f.err
}

func (f *translationTestManager) Resume(context.Context) error {
	return f.err
}

func (f *translationTestManager) ActiveProfile() (translator.Profile, translator.Endpoint, bool) {
	return translator.Profile{}, "", false
}

func TestPrepareUpdateMiningPoolsDispatch_RoutesByNativeCapability(t *testing.T) {
	manager := &translationTestManager{
		endpoint: "stratum+tcp://10.0.0.5:34255",
	}
	service := &Service{
		pluginCaps: translationTestCapabilities{
			nativeDrivers: map[string]bool{"native": true},
		},
		translatorManager: manager,
	}
	devices := []resolvedDevice{
		{id: 11, identifier: "native-miner"},
		{id: 12, identifier: "sv1-miner"},
	}
	service.resolvePoolCapabilitiesOverride = func(
		_ context.Context,
		organizationID int64,
		got []resolvedDevice,
	) ([]poolCapabilityDevice, error) {
		assert.Equal(t, int64(7), organizationID)
		assert.Equal(t, devices, got)
		return []poolCapabilityDevice{
			{id: 11, identifier: "native-miner", driver: "native"},
			{id: 12, identifier: "sv1-miner", driver: "sv1"},
		}, nil
	}
	payload := &dto.UpdateMiningPoolsPayload{
		DefaultPool: dto.MiningPool{
			URL:      translationTestSV2URL,
			Username: "account",
		},
		Backup1Pool: &dto.MiningPool{
			Priority: 1,
			URL:      "stratum+tcp://backup.example.com:3333",
			Username: "account",
		},
	}

	messages, err := service.prepareUpdateMiningPoolsDispatch(
		context.Background(),
		7,
		devices,
		payload,
	)

	require.NoError(t, err)
	require.Equal(t, 1, manager.calls)
	require.NotNil(t, manager.profile)
	require.Equal(t, 1, len(manager.profile.Upstreams))
	assert.Equal(t, translationTestSV2URL, manager.profile.Upstreams[0].URL)
	assert.ElementsMatch(t, []string{"native-miner", "sv1-miner"}, manager.assignment.SelectedDeviceIdentifiers)
	assert.Equal(t, []string{"sv1-miner"}, manager.assignment.TranslatedDeviceIdentifiers)
	require.Equal(t, 2, len(messages))

	nativePayload, ok := messages[0].Payload.(dto.UpdateMiningPoolsPayload)
	require.True(t, ok)
	assert.Equal(t, int64(11), messages[0].DeviceID)
	assert.Equal(t, translationTestSV2URL, nativePayload.DefaultPool.URL)
	assert.True(t, nativePayload.ReleaseSV2Translation)
	require.NotNil(t, nativePayload.Backup1Pool)
	assert.Equal(t, "stratum+tcp://backup.example.com:3333", nativePayload.Backup1Pool.URL)

	sv1Payload, ok := messages[1].Payload.(dto.UpdateMiningPoolsPayload)
	require.True(t, ok)
	assert.Equal(t, int64(12), messages[1].DeviceID)
	assert.Equal(t, manager.endpoint.String(), sv1Payload.DefaultPool.URL)
	assert.False(t, sv1Payload.ReleaseSV2Translation)
	require.NotNil(t, sv1Payload.Backup1Pool)
	assert.Equal(t, "stratum+tcp://backup.example.com:3333", sv1Payload.Backup1Pool.URL)
}

func TestPrepareUpdateMiningPoolsDispatch_AllNativeLeavesSharedPayload(t *testing.T) {
	manager := &translationTestManager{endpoint: "stratum+tcp://10.0.0.5:34255"}
	service := &Service{
		pluginCaps: translationTestCapabilities{
			nativeDrivers: map[string]bool{"native": true},
		},
		translatorManager: manager,
	}
	devices := []resolvedDevice{{id: 11, identifier: "native-miner"}}
	service.resolvePoolCapabilitiesOverride = func(
		context.Context,
		int64,
		[]resolvedDevice,
	) ([]poolCapabilityDevice, error) {
		return []poolCapabilityDevice{{id: 11, identifier: "native-miner", driver: "native"}}, nil
	}
	payload := &dto.UpdateMiningPoolsPayload{
		DefaultPool: dto.MiningPool{URL: translationTestSV2URL},
	}

	messages, err := service.prepareUpdateMiningPoolsDispatch(
		context.Background(),
		7,
		devices,
		payload,
	)

	require.NoError(t, err)
	assert.Nil(t, messages)
	assert.Equal(t, 0, manager.calls)
	assert.True(t, payload.ReleaseSV2Translation)
}

func TestPrepareUpdateMiningPoolsDispatch_SV1AssignmentDefersTranslatedDeviceRelease(t *testing.T) {
	manager := &translationTestManager{}
	service := &Service{translatorManager: manager}
	devices := []resolvedDevice{{id: 12, identifier: "sv1-miner"}}
	payload := &dto.UpdateMiningPoolsPayload{
		DefaultPool: dto.MiningPool{URL: "stratum+tcp://pool.example.com:3333"},
	}

	messages, err := service.prepareUpdateMiningPoolsDispatch(
		context.Background(),
		7,
		devices,
		payload,
	)

	require.NoError(t, err)
	assert.Nil(t, messages)
	assert.Equal(t, 0, manager.calls)
	assert.True(t, payload.ReleaseSV2Translation)
}

func TestPrepareUpdateMiningPoolsDispatch_DoesNotQueueWhenTranslatorFails(t *testing.T) {
	manager := &translationTestManager{err: errors.New("container unavailable")}
	service := &Service{
		pluginCaps:        translationTestCapabilities{},
		translatorManager: manager,
	}
	devices := []resolvedDevice{{id: 12, identifier: "sv1-miner"}}
	service.resolvePoolCapabilitiesOverride = func(
		context.Context,
		int64,
		[]resolvedDevice,
	) ([]poolCapabilityDevice, error) {
		return []poolCapabilityDevice{{id: 12, identifier: "sv1-miner", driver: "sv1"}}, nil
	}

	messages, err := service.prepareUpdateMiningPoolsDispatch(
		context.Background(),
		7,
		devices,
		&dto.UpdateMiningPoolsPayload{
			DefaultPool: dto.MiningPool{URL: translationTestSV2URL},
		},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "container unavailable")
	assert.Nil(t, messages)
}
