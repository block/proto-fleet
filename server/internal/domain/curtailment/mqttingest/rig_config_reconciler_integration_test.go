package mqttingest

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

type rigConfigDelivery struct {
	allDevices  bool
	identifiers []string
	config      sdk.CurtailmentConfig
}

type recordingRigConfigDelivery struct {
	deliveries  []rigConfigDelivery
	duringApply func()
	err         error
}

func (a *recordingRigConfigDelivery) ApplyCurtailmentConfigToProtoRigs(_ context.Context, config sdk.CurtailmentConfig) error {
	return a.record(rigConfigDelivery{allDevices: true, config: config})
}

func (a *recordingRigConfigDelivery) ApplyCurtailmentConfigToDevices(_ context.Context, config sdk.CurtailmentConfig, identifiers []string) error {
	return a.record(rigConfigDelivery{identifiers: append([]string(nil), identifiers...), config: config})
}

func (a *recordingRigConfigDelivery) record(delivery rigConfigDelivery) error {
	a.deliveries = append(a.deliveries, delivery)
	if a.duringApply != nil {
		a.duringApply()
	}
	return a.err
}

func newRigConfigTestService(t *testing.T, fixture *rigConfigStoreFixture, applier *recordingRigConfigDelivery) *SettingsService {
	t.Helper()
	svc, err := NewSettingsService(SettingsServiceConfig{
		Store:            NewSQLCSettingsStore(fixture.queries),
		Cipher:           &fakeSettingsCipher{},
		RigConfigApplier: applier,
	})
	require.NoError(t, err)
	return svc
}

func TestRigConfigReconcilerPairingDoesNotFanOutToExistingRigs(t *testing.T) {
	for _, interleaved := range []bool{false, true} {
		name := "burst"
		if interleaved {
			name = "interleaved"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newRigConfigStoreFixture(t)
			for range 3 {
				fixture.createRig(t, "Proto", "PAIRED")
			}
			applier := &recordingRigConfigDelivery{}
			svc := newRigConfigTestService(t, fixture, applier)
			var paired []string
			for range 10 {
				rig := fixture.createRig(t, "Proto", "PAIRED")
				paired = append(paired, rig.identifier)
				svc.ReapplyRigConfigBestEffort(t.Context(), fixture.orgID, fixture.userID, []string{rig.identifier})
				if interleaved {
					svc.processDueRigConfigReconciliations(t.Context())
				}
			}
			svc.processDueRigConfigReconciliations(t.Context())
			var delivered []string
			for _, delivery := range applier.deliveries {
				require.False(t, delivery.allDevices)
				require.False(t, delivery.config.Enabled, "zero sources must still clear stale rig fallback config")
				delivered = append(delivered, delivery.identifiers...)
			}
			require.ElementsMatch(t, paired, delivered, "only newly paired rigs receive config, once each")
			if interleaved {
				require.Len(t, applier.deliveries, 10)
			} else {
				require.Len(t, applier.deliveries, 1)
			}
		})
	}
}

func TestRigConfigReconcilerPreservesTargetsRequestedDuringDelivery(t *testing.T) {
	fixture := newRigConfigStoreFixture(t)
	first := fixture.createRig(t, "Proto", "PAIRED")
	second := fixture.createRig(t, "Proto", "PAIRED")
	applier := &recordingRigConfigDelivery{}
	svc := newRigConfigTestService(t, fixture, applier)
	applier.duringApply = func() {
		if len(applier.deliveries) == 1 {
			svc.ReapplyRigConfigBestEffort(t.Context(), fixture.orgID, fixture.userID, []string{first.identifier, second.identifier})
		}
	}
	svc.ReapplyRigConfigBestEffort(t.Context(), fixture.orgID, fixture.userID, []string{first.identifier})
	svc.processDueRigConfigReconciliations(t.Context())
	require.Len(t, applier.deliveries, 2)
	require.Equal(t, []string{first.identifier}, applier.deliveries[0].identifiers)
	require.ElementsMatch(t, []string{first.identifier, second.identifier}, applier.deliveries[1].identifiers)
	for _, delivery := range applier.deliveries {
		require.False(t, delivery.allDevices)
	}
	require.Empty(t, fixture.targets(t, 2))
}

func TestRigConfigReconcilerSettingsChangeDuringTargetedDeliveryReachesAllRigs(t *testing.T) {
	fixture := newRigConfigStoreFixture(t)
	rig := fixture.createRig(t, "Proto", "PAIRED")
	applier := &recordingRigConfigDelivery{}
	svc := newRigConfigTestService(t, fixture, applier)
	applier.duringApply = func() {
		if len(applier.deliveries) == 1 {
			source := validSettingsSource()
			source.OrganizationID = fixture.orgID
			source.ServiceUserID = fixture.userID
			source.Enabled = true
			source.MQTTPasswordEncrypted = "enc:secret"
			_, err := svc.store.CreateSourceConfig(t.Context(), source)
			require.NoError(t, err)
		}
	}
	svc.ReapplyRigConfigBestEffort(t.Context(), fixture.orgID, fixture.userID, []string{rig.identifier})
	svc.processDueRigConfigReconciliations(t.Context())
	require.Len(t, applier.deliveries, 2)
	require.False(t, applier.deliveries[0].allDevices)
	require.True(t, applier.deliveries[1].allDevices)
	require.True(t, applier.deliveries[1].config.Enabled)
	require.Len(t, applier.deliveries[1].config.Providers, 1)
}

func TestRigConfigReconcilerUsesClaimedScopeWhenSettingsChangeBeforeDelivery(t *testing.T) {
	fixture := newRigConfigStoreFixture(t)
	rig := fixture.createRig(t, "Proto", "PAIRED")
	applier := &recordingRigConfigDelivery{}
	svc := newRigConfigTestService(t, fixture, applier)
	svc.ReapplyRigConfigBestEffort(t.Context(), fixture.orgID, fixture.userID, []string{rig.identifier})
	claim, err := svc.rigConfigStore.ClaimRigConfigReconciliation(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(0), claim.FullReconcileGeneration)

	source := validSettingsSource()
	source.OrganizationID = fixture.orgID
	source.ServiceUserID = fixture.userID
	source.Enabled = true
	source.MQTTPasswordEncrypted = "enc:secret"
	_, err = svc.store.CreateSourceConfig(t.Context(), source)
	require.NoError(t, err)

	svc.processRigConfigReconciliation(t.Context(), claim)
	require.Len(t, applier.deliveries, 1)
	require.False(t, applier.deliveries[0].allDevices)
	require.Equal(t, []string{rig.identifier}, applier.deliveries[0].identifiers)

	svc.processDueRigConfigReconciliations(t.Context())
	require.Len(t, applier.deliveries, 2)
	require.True(t, applier.deliveries[1].allDevices)
}

func TestRigConfigReconcilerRetriesOnlyRequestedTargets(t *testing.T) {
	fixture := newRigConfigStoreFixture(t)
	fixture.createRig(t, "Proto", "PAIRED")
	rig := fixture.createRig(t, "Proto", "PAIRED")
	applier := &recordingRigConfigDelivery{err: errors.New("queue unavailable")}
	svc := newRigConfigTestService(t, fixture, applier)
	svc.ReapplyRigConfigBestEffort(t.Context(), fixture.orgID, fixture.userID, []string{rig.identifier})
	svc.processDueRigConfigReconciliations(t.Context())
	require.Len(t, applier.deliveries, 1)
	require.Equal(t, []string{rig.identifier}, fixture.targets(t, 1))
	applier.err = nil
	_, err := fixture.db.Exec(`UPDATE curtailment_rig_config_reconciliation SET retry_at = CURRENT_TIMESTAMP WHERE organization_id = $1`, fixture.orgID)
	require.NoError(t, err)
	svc.processDueRigConfigReconciliations(t.Context())
	require.Len(t, applier.deliveries, 2)
	for _, delivery := range applier.deliveries {
		require.False(t, delivery.allDevices)
		require.Equal(t, []string{rig.identifier}, delivery.identifiers)
	}
	require.Empty(t, fixture.targets(t, 1))
}

func TestRigConfigReconcilerEmptyTargetScopeDoesNotBecomeFullDelivery(t *testing.T) {
	fixture := newRigConfigStoreFixture(t)
	fixture.createRig(t, "Proto", "PAIRED")
	rig := fixture.createRig(t, "Proto", "PAIRED")
	applier := &recordingRigConfigDelivery{}
	svc := newRigConfigTestService(t, fixture, applier)
	svc.ReapplyRigConfigBestEffort(t.Context(), fixture.orgID, fixture.userID, []string{rig.identifier})
	_, err := fixture.db.Exec(`UPDATE device_pairing SET pairing_status = 'UNPAIRED' WHERE device_id = $1`, rig.id)
	require.NoError(t, err)
	svc.processDueRigConfigReconciliations(t.Context())
	require.Empty(t, applier.deliveries)
	_, err = svc.rigConfigStore.ClaimRigConfigReconciliation(t.Context())
	require.ErrorIs(t, err, ErrRigConfigReconciliationNotFound)
}
