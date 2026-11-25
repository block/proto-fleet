package mqttingest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
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
	require.False(t, applier.deliveries[1].allDevices)
	require.False(t, applier.deliveries[1].config.Enabled, "a source without an enabled rule must not be configured on rigs")
	require.Empty(t, applier.deliveries[1].config.Providers)
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
	require.False(t, applier.deliveries[1].allDevices)
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

func TestRigConfigReconcilerScopesProvidersToGrantedRuleMembers(t *testing.T) {
	f := newRigConfigStoreFixture(t)
	selected := f.createRig(t, "Proto", "PAIRED")
	outside := f.createRig(t, "Proto", "PAIRED")
	source := validSettingsSource()
	source.OrganizationID = f.orgID
	source.ServiceUserID = f.userID
	source.Enabled = true
	source.MQTTPasswordEncrypted = "enc:secret"
	settings := NewSQLCSettingsStore(f.queries)
	saved, err := settings.CreateSourceConfig(t.Context(), source)
	require.NoError(t, err)
	var profileID, ruleID int64
	envelope := `{"schema_version":1,"selected_resource_site_ids":[],"current_member_site_ids":[],"miner_scope_unbounded":true,"facility_fan_site_ids":[],"facility_fan_scope_unbounded":false}`
	scope := fmt.Sprintf(`{"scope_schema_version":1,"device_identifiers":[%q]}`, selected.identifier)
	require.NoError(t, f.db.QueryRowContext(t.Context(), "INSERT INTO curtailment_response_profile(org_id,profile_name,mode,scope_json,authorization_envelope_jsonb) VALUES ($1,'scoped-profile','FULL_FLEET',$2::jsonb,$3::jsonb) RETURNING id", f.orgID, scope, envelope).Scan(&profileID))
	require.NoError(t, f.db.QueryRowContext(t.Context(), "INSERT INTO curtailment_automation_rule(org_id,rule_name,mqtt_source_id,response_profile_id,enabled) VALUES ($1,'scoped-rule',$2,$3,TRUE) RETURNING id", f.orgID, saved.ID, profileID).Scan(&ruleID))
	require.NoError(t, f.queries.UpsertCurtailmentAutomationExecutionGrant(t.Context(), sqlc.UpsertCurtailmentAutomationExecutionGrantParams{RuleID: ruleID, OrgID: f.orgID, AdminControlsGranted: true}))
	applier := &recordingRigConfigDelivery{}
	svc := newRigConfigTestService(t, f, applier)
	svc.processDueRigConfigReconciliations(t.Context())
	require.Len(t, applier.deliveries, 2)
	for _, delivery := range applier.deliveries {
		require.False(t, delivery.allDevices)
		if delivery.config.Enabled {
			require.Equal(t, []string{selected.identifier}, delivery.identifiers)
			require.Len(t, delivery.config.Providers, 1)
		} else {
			require.Equal(t, []string{outside.identifier}, delivery.identifiers)
			require.Empty(t, delivery.config.Providers)
		}
	}
	_, err = f.db.ExecContext(t.Context(), "UPDATE curtailment_automation_rule SET enabled=FALSE WHERE id=$1", ruleID)
	require.NoError(t, err)
	applier.deliveries = nil
	svc.processDueRigConfigReconciliations(t.Context())
	require.Len(t, applier.deliveries, 1)
	require.False(t, applier.deliveries[0].config.Enabled)
	require.ElementsMatch(t, []string{selected.identifier, outside.identifier}, applier.deliveries[0].identifiers)
}

type rigConfigScopeFixture struct {
	*rigConfigStoreFixture
	sourceID int64
	siteA    int64
	siteB    int64
	rigA     rigConfigTestRig
	rigB     rigConfigTestRig
	rigNone  rigConfigTestRig
}

func newRigConfigScopeFixture(t *testing.T) *rigConfigScopeFixture {
	t.Helper()
	f := &rigConfigScopeFixture{rigConfigStoreFixture: newRigConfigStoreFixture(t)}
	source := validSettingsSource()
	source.OrganizationID = f.orgID
	source.ServiceUserID = f.userID
	source.Enabled = true
	source.MQTTPasswordEncrypted = "enc:secret"
	saved, err := NewSQLCSettingsStore(f.queries).CreateSourceConfig(t.Context(), source)
	require.NoError(t, err)
	f.sourceID = saved.ID
	f.siteA = f.insertID(t, "INSERT INTO site (org_id,name,slug) VALUES ($1,'Site A','site-a') RETURNING id", f.orgID)
	f.siteB = f.insertID(t, "INSERT INTO site (org_id,name,slug) VALUES ($1,'Site B','site-b') RETURNING id", f.orgID)
	f.rigA = f.createRig(t, "Proto", "PAIRED")
	f.rigB = f.createRig(t, "Proto", "PAIRED")
	f.rigNone = f.createRig(t, "Proto", "PAIRED")
	f.exec(t, "UPDATE device SET site_id=$1 WHERE id=$2", f.siteA, f.rigA.id)
	f.exec(t, "UPDATE device SET site_id=$1 WHERE id=$2", f.siteB, f.rigB.id)
	return f
}

func (f *rigConfigStoreFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	_, err := f.db.ExecContext(t.Context(), query, args...)
	require.NoError(t, err)
}

func (f *rigConfigStoreFixture) insertID(t *testing.T, query string, args ...any) int64 {
	t.Helper()
	var id int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), query, args...).Scan(&id))
	return id
}

// bindRule creates an enabled rule for the profile and grants it the profile's
// current revision and authorization envelope.
func (f *rigConfigScopeFixture) bindRule(t *testing.T, scope, envelope string) int64 {
	t.Helper()
	profileID := f.insertID(t, "INSERT INTO curtailment_response_profile(org_id,profile_name,mode,scope_json,authorization_envelope_jsonb) VALUES ($1,$2,'FULL_FLEET',$3::jsonb,$4::jsonb) RETURNING id", f.orgID, uuid.NewString(), scope, envelope)
	ruleID := f.insertID(t, "INSERT INTO curtailment_automation_rule(org_id,rule_name,mqtt_source_id,response_profile_id,enabled) VALUES ($1,$2,$3,$4,TRUE) RETURNING id", f.orgID, uuid.NewString(), f.sourceID, profileID)
	require.NoError(t, f.queries.UpsertCurtailmentAutomationExecutionGrant(t.Context(), sqlc.UpsertCurtailmentAutomationExecutionGrantParams{RuleID: ruleID, OrgID: f.orgID, AdminControlsGranted: true}))
	return ruleID
}

func (f *rigConfigScopeFixture) providedRigs(t *testing.T) []string {
	t.Helper()
	rows, err := f.queries.ListRigConfigSourceScopes(t.Context(), sqlc.ListRigConfigSourceScopesParams{
		OrganizationID: f.orgID, ContainerDeviceClass: int32(commonpb.DeviceClass_DEVICE_CLASS_CONTAINER), DeviceIdentifiers: []string{},
	})
	require.NoError(t, err)
	require.Len(t, rows, 3, "every eligible rig receives a config, even an empty one")
	provided := []string{}
	for _, row := range rows {
		if len(row.SourceIds) > 0 {
			require.Equal(t, []int64{f.sourceID}, row.SourceIds)
			provided = append(provided, row.DeviceIdentifier)
		}
	}
	return provided
}

// ListRigConfigSourceScopes decides which rigs hold which MQTT fallback
// providers: a rig needs both the profile scope and the grant envelope.
func TestRigConfigSourceScopesFollowProfileScopeAndGrantEnvelope(t *testing.T) {
	const unbounded = `{"schema_version":1,"selected_resource_site_ids":[],"current_member_site_ids":[],"miner_scope_unbounded":true,"facility_fan_site_ids":[],"facility_fan_scope_unbounded":false}`
	envelope := func(selected, current []int64) string {
		encode := func(ids []int64) string {
			parts := make([]string, len(ids))
			for i, id := range ids {
				parts[i] = fmt.Sprint(id)
			}
			return "[" + strings.Join(parts, ",") + "]"
		}
		return fmt.Sprintf(`{"schema_version":1,"selected_resource_site_ids":%s,"current_member_site_ids":%s,"miner_scope_unbounded":false,"facility_fan_site_ids":[],"facility_fan_scope_unbounded":false}`, encode(selected), encode(current))
	}
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, f *rigConfigScopeFixture)
		want  func(f *rigConfigScopeFixture) []string
	}{
		{
			name:  "whole org with unbounded envelope",
			setup: func(t *testing.T, f *rigConfigScopeFixture) { f.bindRule(t, `{"whole_org":true}`, unbounded) },
			want: func(f *rigConfigScopeFixture) []string {
				return []string{f.rigA.identifier, f.rigB.identifier, f.rigNone.identifier}
			},
		},
		{
			name:  "empty legacy scope is canonicalized to whole org",
			setup: func(t *testing.T, f *rigConfigScopeFixture) { f.bindRule(t, `{}`, unbounded) },
			want: func(f *rigConfigScopeFixture) []string {
				return []string{f.rigA.identifier, f.rigB.identifier, f.rigNone.identifier}
			},
		},
		{
			name: "site_ids selects only matching rigs",
			setup: func(t *testing.T, f *rigConfigScopeFixture) {
				f.bindRule(t, fmt.Sprintf(`{"site_ids":[%d]}`, f.siteA), unbounded)
			},
			want: func(f *rigConfigScopeFixture) []string { return []string{f.rigA.identifier} },
		},
		{
			name: "legacy site_id selects only matching rigs",
			setup: func(t *testing.T, f *rigConfigScopeFixture) {
				f.bindRule(t, fmt.Sprintf(`{"site_id":%d}`, f.siteB), unbounded)
			},
			want: func(f *rigConfigScopeFixture) []string { return []string{f.rigB.identifier} },
		},
		{
			name: "rack_ids selects rack members",
			setup: func(t *testing.T, f *rigConfigScopeFixture) {
				rackID := f.insertID(t, "INSERT INTO device_set (org_id,type,label) VALUES ($1,'rack','Rack') RETURNING id", f.orgID)
				f.exec(t, "INSERT INTO device_set_membership (org_id,device_set_id,device_set_type,device_id,device_identifier) VALUES ($1,$2,'rack',$3,$4)", f.orgID, rackID, f.rigNone.id, f.rigNone.identifier)
				f.bindRule(t, fmt.Sprintf(`{"rack_ids":[%d]}`, rackID), unbounded)
			},
			want: func(f *rigConfigScopeFixture) []string { return []string{f.rigNone.identifier} },
		},
		{
			name: "selected-resource envelope bounds a whole-org scope",
			setup: func(t *testing.T, f *rigConfigScopeFixture) {
				f.bindRule(t, `{"whole_org":true}`, envelope([]int64{f.siteA}, nil))
			},
			want: func(f *rigConfigScopeFixture) []string { return []string{f.rigA.identifier} },
		},
		{
			name: "current-member envelope bounds a whole-org scope",
			setup: func(t *testing.T, f *rigConfigScopeFixture) {
				f.bindRule(t, `{"whole_org":true}`, envelope(nil, []int64{f.siteB}))
			},
			want: func(f *rigConfigScopeFixture) []string { return []string{f.rigB.identifier} },
		},
		{
			name: "rig outside both envelope site lists gets no provider",
			setup: func(t *testing.T, f *rigConfigScopeFixture) {
				f.bindRule(t, fmt.Sprintf(`{"site_ids":[%d]}`, f.siteA), envelope([]int64{f.siteB}, []int64{f.siteB}))
			},
			want: func(*rigConfigScopeFixture) []string { return nil },
		},
		{
			name: "stale grant revision grants nothing",
			setup: func(t *testing.T, f *rigConfigScopeFixture) {
				ruleID := f.bindRule(t, `{"whole_org":true}`, unbounded)
				f.exec(t, "UPDATE curtailment_automation_execution_grant SET response_profile_revision=$1 WHERE rule_id=$2", uuid.New(), ruleID)
			},
			want: func(*rigConfigScopeFixture) []string { return nil },
		},
		{
			name: "disabled source grants nothing",
			setup: func(t *testing.T, f *rigConfigScopeFixture) {
				f.bindRule(t, `{"whole_org":true}`, unbounded)
				f.exec(t, "UPDATE curtailment_mqtt_source_config SET enabled=FALSE WHERE id=$1", f.sourceID)
			},
			want: func(*rigConfigScopeFixture) []string { return nil },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRigConfigScopeFixture(t)
			tc.setup(t, f)
			require.ElementsMatch(t, tc.want(f), f.providedRigs(t))
		})
	}
}

// A bulk site reassignment is one UPDATE; delivery is requested once for the
// organization, covering every moved rig, and unrelated updates request none.
func TestRigConfigReconcilerBatchesBulkDevicePlacementChanges(t *testing.T) {
	f := newRigConfigScopeFixture(t)
	f.bindRule(t, fmt.Sprintf(`{"site_ids":[%d]}`, f.siteA), `{"schema_version":1,"selected_resource_site_ids":[],"current_member_site_ids":[],"miner_scope_unbounded":true,"facility_fan_site_ids":[],"facility_fan_scope_unbounded":false}`)
	applier := &recordingRigConfigDelivery{}
	svc := newRigConfigTestService(t, f.rigConfigStoreFixture, applier)
	svc.processDueRigConfigReconciliations(t.Context())
	generation := func() int64 {
		t.Helper()
		var desired int64
		require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT desired_generation FROM curtailment_rig_config_reconciliation WHERE organization_id=$1", f.orgID).Scan(&desired))
		return desired
	}
	before := generation()

	f.exec(t, "UPDATE device SET site_id=$1 WHERE id = ANY($2)", f.siteA, pq.Array([]int64{f.rigB.id, f.rigNone.id}))
	require.Equal(t, before+1, generation(), "one statement requests one delivery")
	require.ElementsMatch(t, []string{f.rigB.identifier, f.rigNone.identifier}, f.targets(t, before+1))
	applier.deliveries = nil
	svc.processDueRigConfigReconciliations(t.Context())
	require.Len(t, applier.deliveries, 1)
	require.True(t, applier.deliveries[0].config.Enabled)
	require.ElementsMatch(t, []string{f.rigB.identifier, f.rigNone.identifier}, applier.deliveries[0].identifiers)

	moved := generation()
	f.exec(t, "UPDATE device SET worker_name='renamed' WHERE id = ANY($1)", pq.Array([]int64{f.rigA.id, f.rigB.id}))
	f.exec(t, "UPDATE device SET site_id=site_id WHERE id=$1", f.rigA.id)
	require.Equal(t, moved, generation(), "updates that keep placement request nothing")

	f.exec(t, "UPDATE device SET site_id=NULL WHERE id=$1", f.rigB.id)
	applier.deliveries = nil
	svc.processDueRigConfigReconciliations(t.Context())
	require.Len(t, applier.deliveries, 1)
	require.False(t, applier.deliveries[0].config.Enabled, "leaving the selected site clears the provider")
	require.Equal(t, []string{f.rigB.identifier}, applier.deliveries[0].identifiers)
}

// Fallback providers depend on placement and membership, so changing either
// must reach the affected rig without editing the rule or re-pairing the rig.
func TestRigConfigReconcilerFollowsMembershipAndPlacementChanges(t *testing.T) {
	f := newRigConfigStoreFixture(t)
	rig := f.createRig(t, "Proto", "PAIRED")
	other := f.createRig(t, "Proto", "PAIRED")
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := f.db.ExecContext(t.Context(), query, args...)
		require.NoError(t, err)
	}
	insertID := func(query string, args ...any) int64 {
		t.Helper()
		var id int64
		require.NoError(t, f.db.QueryRowContext(t.Context(), query, args...).Scan(&id))
		return id
	}
	applier := &recordingRigConfigDelivery{}
	svc := newRigConfigTestService(t, f, applier)
	delivered := func() map[string]bool {
		t.Helper()
		applier.deliveries = nil
		svc.processDueRigConfigReconciliations(t.Context())
		out := map[string]bool{}
		for _, delivery := range applier.deliveries {
			require.False(t, delivery.allDevices)
			for _, identifier := range delivery.identifiers {
				out[identifier] = delivery.config.Enabled
			}
		}
		return out
	}

	groupID := insertID("INSERT INTO device_set (org_id,type,label) VALUES ($1,'group','Selected') RETURNING id", f.orgID)
	buildingID := insertID("INSERT INTO building (org_id,name) VALUES ($1,'Selected') RETURNING id", f.orgID)
	otherBuildingID := insertID("INSERT INTO building (org_id,name) VALUES ($1,'Other') RETURNING id", f.orgID)
	rackID := insertID("INSERT INTO device_set (org_id,type,label) VALUES ($1,'rack','Rack') RETURNING id", f.orgID)
	exec("INSERT INTO device_set_rack (device_set_id,org_id,building_id,rows,columns) VALUES ($1,$2,$3,1,1)", rackID, f.orgID, otherBuildingID)

	// Without an enabled rule no rig has a provider, so membership is irrelevant.
	exec("INSERT INTO device_set_membership (org_id,device_set_id,device_set_type,device_id,device_identifier) VALUES ($1,$2,'group',$3,$4)", f.orgID, groupID, rig.id, rig.identifier)
	exec("DELETE FROM device_set_membership WHERE device_set_id=$1", groupID)
	require.Empty(t, delivered())

	source := validSettingsSource()
	source.OrganizationID = f.orgID
	source.ServiceUserID = f.userID
	source.Enabled = true
	source.MQTTPasswordEncrypted = "enc:secret"
	saved, err := NewSQLCSettingsStore(f.queries).CreateSourceConfig(t.Context(), source)
	require.NoError(t, err)
	envelope := `{"schema_version":1,"selected_resource_site_ids":[],"current_member_site_ids":[],"miner_scope_unbounded":true,"facility_fan_site_ids":[],"facility_fan_scope_unbounded":false}`
	scope := fmt.Sprintf(`{"scope_schema_version":1,"group_ids":[%d],"building_ids":[%d]}`, groupID, buildingID)
	profileID := insertID("INSERT INTO curtailment_response_profile(org_id,profile_name,mode,scope_json,authorization_envelope_jsonb) VALUES ($1,'topology-profile','FULL_FLEET',$2::jsonb,$3::jsonb) RETURNING id", f.orgID, scope, envelope)
	ruleID := insertID("INSERT INTO curtailment_automation_rule(org_id,rule_name,mqtt_source_id,response_profile_id,enabled) VALUES ($1,'topology-rule',$2,$3,TRUE) RETURNING id", f.orgID, saved.ID, profileID)
	require.NoError(t, f.queries.UpsertCurtailmentAutomationExecutionGrant(t.Context(), sqlc.UpsertCurtailmentAutomationExecutionGrantParams{RuleID: ruleID, OrgID: f.orgID, AdminControlsGranted: true}))
	require.Equal(t, map[string]bool{rig.identifier: false, other.identifier: false}, delivered())

	exec("INSERT INTO device_set_membership (org_id,device_set_id,device_set_type,device_id,device_identifier) VALUES ($1,$2,'group',$3,$4)", f.orgID, groupID, rig.id, rig.identifier)
	require.Equal(t, map[string]bool{rig.identifier: true}, delivered(), "joining a selected group")
	exec("UPDATE device_set SET deleted_at=CURRENT_TIMESTAMP WHERE id=$1", groupID)
	require.Equal(t, map[string]bool{rig.identifier: false}, delivered(), "deleting the selected group")

	exec("UPDATE device SET building_id=$1 WHERE id=$2", buildingID, other.id)
	require.Equal(t, map[string]bool{other.identifier: true}, delivered(), "placing a rig in a selected building")

	exec("INSERT INTO device_set_membership (org_id,device_set_id,device_set_type,device_id,device_identifier) VALUES ($1,$2,'rack',$3,$4)", f.orgID, rackID, rig.id, rig.identifier)
	require.Equal(t, map[string]bool{rig.identifier: false}, delivered(), "joining a rack outside the selection")
	exec("UPDATE device_set_rack SET building_id=$1 WHERE device_set_id=$2", buildingID, rackID)
	require.Equal(t, map[string]bool{rig.identifier: true}, delivered(), "moving the rack into a selected building")

	exec("UPDATE building SET deleted_at=CURRENT_TIMESTAMP WHERE id=$1", buildingID)
	require.Equal(t, map[string]bool{rig.identifier: false, other.identifier: false}, delivered(), "deleting the selected building")
}
