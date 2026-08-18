package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// fakeScopeLookup returns only the ids marked live, mimicking the ByIDs diff contract.
type fakeScopeLookup struct {
	sites     map[int64]bool
	buildings map[int64]bool
	sets      map[string]map[int64]bool
	err       error
}

func (f fakeScopeLookup) filter(live map[int64]bool, ids []int64) ([]int64, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []int64
	for _, id := range ids {
		if live[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

func (f fakeScopeLookup) SitesByIDs(_ context.Context, _ int64, ids []int64) ([]int64, error) {
	return f.filter(f.sites, ids)
}

func (f fakeScopeLookup) BuildingsByIDs(_ context.Context, _ int64, ids []int64) ([]int64, error) {
	return f.filter(f.buildings, ids)
}

func (f fakeScopeLookup) DeviceSetsByIDs(_ context.Context, _ int64, setType string, ids []int64) ([]int64, error) {
	return f.filter(f.sets[setType], ids)
}

// fakeRuleConfigStore is a map-backed RuleConfigStore for lifecycle tests.
type fakeRuleConfigStore struct {
	configs   map[string]RuleConfig
	upsertErr error
	// Clear upsertErr after its first use, simulating a transient outage.
	upsertErrOnce bool
	// The upsert lands and then errors, simulating a timeout after commit.
	upsertErrAfterCommit bool
	// GetConfig errors only once an upsert was attempted: the pre-write read
	// succeeds but the post-failure probe cannot confirm the row's state.
	getErrAfterUpsert error
	upsertAttempted   bool
	// The first upsert lands and later ones fail, simulating an outage that
	// starts between the legacy config stage and the post-PUT publish.
	upsertErrAfterFirst bool
	upsertCount         int
}

func newFakeRuleConfigStore() *fakeRuleConfigStore {
	return &fakeRuleConfigStore{configs: map[string]RuleConfig{}}
}

func (f *fakeRuleConfigStore) UpsertConfig(_ context.Context, _ int64, ruleUID string, cfg RuleConfig) error {
	f.upsertAttempted = true
	if f.upsertErrAfterFirst && f.upsertCount > 0 {
		return fmt.Errorf("db down")
	}
	f.upsertCount++
	if f.upsertErrAfterCommit {
		f.configs[ruleUID] = cfg
		return fmt.Errorf("timeout after commit")
	}
	if f.upsertErr != nil {
		err := f.upsertErr
		if f.upsertErrOnce {
			f.upsertErr = nil
		}
		return err
	}
	f.configs[ruleUID] = cfg
	return nil
}

func (f *fakeRuleConfigStore) GetConfig(_ context.Context, _ int64, ruleUID string) (*RuleConfig, error) {
	if f.getErrAfterUpsert != nil && f.upsertAttempted {
		return nil, f.getErrAfterUpsert
	}
	cfg, ok := f.configs[ruleUID]
	if !ok {
		return nil, nil
	}
	return &cfg, nil
}

func (f *fakeRuleConfigStore) ListConfigs(_ context.Context, _ int64, ruleUIDs []string) (map[string]RuleConfig, error) {
	out := make(map[string]RuleConfig, len(ruleUIDs))
	for _, uid := range ruleUIDs {
		if cfg, ok := f.configs[uid]; ok {
			out[uid] = cfg
		}
	}
	return out, nil
}

func (f *fakeRuleConfigStore) DeleteConfig(_ context.Context, _ int64, ruleUID string) error {
	delete(f.configs, ruleUID)
	return nil
}

// The fake has no row timestamps, so it treats every orphan as aged.
func (f *fakeRuleConfigStore) SweepConfigs(_ context.Context, _ int64, liveRuleUIDs []string) (int64, error) {
	live := make(map[string]bool, len(liveRuleUIDs))
	for _, uid := range liveRuleUIDs {
		live[uid] = true
	}
	var n int64
	for uid := range f.configs {
		if !live[uid] {
			delete(f.configs, uid)
			n++
		}
	}
	return n, nil
}

func scopedOfflineConfig(scope *RuleScope) RuleConfig {
	cfg := offlineConfig("Offline too long", 1800)
	cfg.Scope = scope
	return cfg
}

// orgWideOfflineConfig is an update body that explicitly clears the scope, the
// way a scope-aware client expresses org-wide (see updateRuleSerialized).
func orgWideOfflineConfig(name string, durationSeconds int32) RuleConfig {
	cfg := offlineConfig(name, durationSeconds)
	cfg.ScopeOrgWideExplicit = true
	return cfg
}

func TestNormalizeRuleScope(t *testing.T) {
	assert.Nil(t, normalizeRuleScope(nil))
	assert.Nil(t, normalizeRuleScope(&RuleScope{}))
	got := normalizeRuleScope(&RuleScope{
		SiteIDs:     []int64{5, 3, 5},
		DeviceIDs:   []string{"dev-b", "dev-a", "dev-b"},
		BuildingIDs: []int64{2, 2},
		RackIDs:     []int64{8, 4},
		GroupIDs:    []int64{6},
	})
	assert.Equal(t, &RuleScope{
		SiteIDs:     []int64{3, 5},
		DeviceIDs:   []string{"dev-a", "dev-b"},
		BuildingIDs: []int64{2},
		RackIDs:     []int64{4, 8},
		GroupIDs:    []int64{6},
	}, got)
	// AllSites supersedes the explicit list so the compiled SQL has one site condition.
	assert.Equal(t, &RuleScope{AllSites: true},
		normalizeRuleScope(&RuleScope{AllSites: true, SiteIDs: []int64{3}}))
}

func TestValidateRuleScope(t *testing.T) {
	lookup := fakeScopeLookup{
		sites:     map[int64]bool{3: true, 5: true},
		buildings: map[int64]bool{2: true},
		sets: map[string]map[int64]bool{
			"rack":  {4: true},
			"group": {6: true},
		},
	}
	svc := NewService(nil, nil, nil, nil, nil, nil, lookup, DestinationPolicy{})

	manyIDs := make([]int64, maxRuleScopePlacementIDs+1)
	for i := range manyIDs {
		manyIDs[i] = int64(i + 1)
	}
	manyDevices := make([]string, maxRuleScopeDeviceIDs+1)
	for i := range manyDevices {
		manyDevices[i] = fmt.Sprintf("dev-%d", i)
	}

	cases := []struct {
		name    string
		scope   *RuleScope
		wantErr bool
	}{
		{name: "nil scope", scope: nil},
		{name: "org-owned sites", scope: &RuleScope{SiteIDs: []int64{3, 5}}},
		{name: "org-owned everything", scope: &RuleScope{SiteIDs: []int64{3}, BuildingIDs: []int64{2}, RackIDs: []int64{4}, GroupIDs: []int64{6}}},
		{name: "all sites needs no site lookup", scope: &RuleScope{AllSites: true}},
		{name: "devices pattern ok", scope: &RuleScope{DeviceIDs: []string{"a-1.b:c_d"}}},
		{name: "unknown site", scope: &RuleScope{SiteIDs: []int64{3, 4}}, wantErr: true},
		{name: "unknown building", scope: &RuleScope{BuildingIDs: []int64{3}}, wantErr: true},
		{name: "rack id that is a group", scope: &RuleScope{RackIDs: []int64{6}}, wantErr: true},
		{name: "unknown group", scope: &RuleScope{GroupIDs: []int64{4}}, wantErr: true},
		{name: "non-positive site id", scope: &RuleScope{SiteIDs: []int64{0}}, wantErr: true},
		{name: "too many sites", scope: &RuleScope{SiteIDs: manyIDs}, wantErr: true},
		{name: "too many groups", scope: &RuleScope{GroupIDs: manyIDs}, wantErr: true},
		{name: "too many devices", scope: &RuleScope{DeviceIDs: manyDevices}, wantErr: true},
		{name: "device id escapes quoting", scope: &RuleScope{DeviceIDs: []string{"a' OR '1'='1"}}, wantErr: true},
		{name: "device id too long", scope: &RuleScope{DeviceIDs: []string{strings.Repeat("a", maxDeviceIDLength+1)}}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.validateRuleScope(context.Background(), 7, tc.scope, nil)
			if tc.wantErr {
				require.Error(t, err)
				assert.True(t, fleeterror.IsInvalidArgumentError(err))
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// keep lets an update retain a stored placement id whose target was deleted
// after the rule was created, without loosening the check for added ids.
func TestValidateRuleScopeKeepsStoredIDs(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, nil, nil, fakeScopeLookup{sites: map[int64]bool{3: true}, sets: map[string]map[int64]bool{"group": {6: true}}}, DestinationPolicy{})
	keep := &RuleScope{SiteIDs: []int64{9}, GroupIDs: []int64{11}}
	require.NoError(t, svc.validateRuleScope(context.Background(), 7, &RuleScope{SiteIDs: []int64{3, 9}, GroupIDs: []int64{6, 11}}, keep))
	err := svc.validateRuleScope(context.Background(), 7, &RuleScope{SiteIDs: []int64{3, 9, 4}}, keep)
	require.Error(t, err, "adding another dead site id must still be rejected")
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

// A nil ScopeLookup (tests, partial wiring) skips only the ownership check; the
// compiled SQL stays org-filtered so a foreign placement id is inert, not a leak.
func TestValidateRuleScopeWithoutLookupSkipsOwnership(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, nil, nil, nil, DestinationPolicy{})
	require.NoError(t, svc.validateRuleScope(context.Background(), 7, &RuleScope{SiteIDs: []int64{999}, GroupIDs: []int64{999}}, nil))
	require.Error(t, svc.validateRuleScope(context.Background(), 7, &RuleScope{DeviceIDs: []string{"bad id"}}, nil))
}

// A store failure must fail closed as an internal error, not be misreported as
// the caller's bad input.
func TestValidateRuleScopeLookupError(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, nil, nil, fakeScopeLookup{err: fmt.Errorf("db down")}, DestinationPolicy{})
	err := svc.validateRuleScope(context.Background(), 7, &RuleScope{SiteIDs: []int64{3}}, nil)
	require.Error(t, err)
	assert.False(t, fleeterror.IsInvalidArgumentError(err))
}

// Unscoped rules must compile byte-identical to the pre-scope output: any drift changes
// instance labels, hence fingerprints and alert identity, for every existing user rule.
func TestCompileUnscopedSQLUnchanged(t *testing.T) {
	compiled, err := compileUserRule(7, "pfu-test", offlineConfig("Offline too long", 1800))
	require.NoError(t, err)
	assert.Equal(t, `SELECT
    organization_id,
    device_id,
    1 AS value
FROM notification_metric_sample
WHERE metric = 'fleet_device_online'
  AND organization_id = '7'
  AND time > NOW() - INTERVAL '10 minutes'
GROUP BY organization_id, device_id
HAVING last(value, time) = 0`, compiledSQL(t, compiled))
}

// Every scope dimension resolves through fleet_device_placement at eval time: a moved miner joins/
// leaves the scope immediately, and stale or soft-deleted samples can never satisfy the rule.
func TestCompileScopedOfflineSQL(t *testing.T) {
	cases := []struct {
		name     string
		scope    *RuleScope
		wantPred string
	}{
		{
			name:  "site only",
			scope: &RuleScope{SiteIDs: []int64{3, 5}},
			wantPred: `device_id IN (
    SELECT device_id FROM fleet_device_placement
    WHERE org_id = 7
      AND site_id IN (3, 5)
  )`,
		},
		{
			name:  "all sites",
			scope: &RuleScope{AllSites: true},
			wantPred: `device_id IN (
    SELECT device_id FROM fleet_device_placement
    WHERE org_id = 7
      AND site_id IS NOT NULL
  )`,
		},
		{
			name:  "every placement dimension",
			scope: &RuleScope{SiteIDs: []int64{3}, BuildingIDs: []int64{2}, RackIDs: []int64{4}, GroupIDs: []int64{6}},
			wantPred: `device_id IN (
    SELECT device_id FROM fleet_device_placement
    WHERE org_id = 7
      AND (site_id IN (3) OR building_id IN (2) OR rack_id IN (4) OR group_id IN (6))
  )`,
		},
		{
			name:  "device only",
			scope: &RuleScope{DeviceIDs: []string{"dev-a"}},
			wantPred: `device_id IN (
    SELECT device_id FROM fleet_device_placement
    WHERE org_id = 7
      AND device_id IN ('dev-a')
  )`,
		},
		{
			name:  "union of devices and placement",
			scope: &RuleScope{SiteIDs: []int64{3, 5}, DeviceIDs: []string{"dev-a"}},
			wantPred: `device_id IN (
    SELECT device_id FROM fleet_device_placement
    WHERE org_id = 7
      AND (device_id IN ('dev-a') OR site_id IN (3, 5))
  )`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := compileUserRule(7, "pfu-test", scopedOfflineConfig(tc.scope))
			require.NoError(t, err)
			assert.Equal(t, fmt.Sprintf(`SELECT
    organization_id,
    device_id,
    COALESCE((SELECT site_id::text FROM fleet_device_placement
     WHERE org_id = 7 AND device_id = notification_metric_sample.device_id LIMIT 1), '') AS site_id,
    1 AS value
FROM notification_metric_sample
WHERE metric = 'fleet_device_online'
  AND organization_id = '7'
  AND %s
  AND time > NOW() - INTERVAL '10 minutes'
GROUP BY organization_id, device_id
HAVING last(value, time) = 0`, tc.wantPred), compiledSQL(t, compiled))
		})
	}
}

func TestCompileScopedAbsoluteHashrateSQL(t *testing.T) {
	compiled, err := compileUserRule(7, "pfu-test", RuleConfig{
		Name: "Slow hashing", DurationSeconds: 600,
		Hashrate: &HashrateRuleConfig{Mode: HashrateModeAbsolute, Value: 90, Unit: HashrateUnitTerahash},
		Scope:    &RuleScope{DeviceIDs: []string{"dev-a", "dev-b"}},
	})
	require.NoError(t, err)
	assert.Equal(t, `WITH latest AS (
    SELECT
        organization_id,
        device_id,
        metric,
        last(value, time) AS latest_value
    FROM notification_metric_sample
    WHERE metric IN ('fleet_device_hashrate_terahash', 'fleet_device_hashing')
      AND organization_id = '7'
      AND device_id IN (
        SELECT device_id FROM fleet_device_placement
        WHERE org_id = 7
          AND device_id IN ('dev-a', 'dev-b')
      )
      AND time > NOW() - INTERVAL '10 minutes'
    GROUP BY organization_id, device_id, metric
)
SELECT
    obs.organization_id,
    obs.device_id,
    COALESCE((SELECT site_id::text FROM fleet_device_placement
     WHERE org_id = 7 AND device_id = obs.device_id LIMIT 1), '') AS site_id,
    1 AS value
FROM latest AS obs
JOIN latest AS gate
  ON gate.organization_id = obs.organization_id
 AND gate.device_id = obs.device_id
 AND gate.metric = 'fleet_device_hashing'
WHERE obs.metric = 'fleet_device_hashrate_terahash'
  AND (gate.latest_value < 1 OR obs.latest_value > 0)
  AND obs.latest_value < 90`, compiledSQL(t, compiled))
}

func TestCompileScopedTemperatureSQL(t *testing.T) {
	compiled, err := compileUserRule(7, "pfu-test", RuleConfig{
		Name: "Running hot", DurationSeconds: 900,
		Temperature: &TemperatureRuleConfig{MaxCelsius: 85},
		Scope:       &RuleScope{SiteIDs: []int64{3}},
	})
	require.NoError(t, err)
	assert.Equal(t, `WITH latest_per_kind AS (
    SELECT
        organization_id,
        device_id,
        sensor_kind,
        last(value, time) AS latest_temp,
        max(time) AS last_sample_time
    FROM notification_metric_sample
    WHERE metric = 'fleet_device_temperature_max_celsius'
      AND organization_id = '7'
      AND device_id IN (
        SELECT device_id FROM fleet_device_placement
        WHERE org_id = 7
          AND site_id IN (3)
      )
      AND time > NOW() - INTERVAL '10 minutes'
    GROUP BY organization_id, device_id, sensor_kind
)
SELECT
    organization_id,
    device_id,
    COALESCE((SELECT site_id::text FROM fleet_device_placement
     WHERE org_id = 7 AND device_id = latest_per_kind.device_id LIMIT 1), '') AS site_id,
    max(latest_temp) AS latest_temp
FROM latest_per_kind
WHERE last_sample_time > NOW() - INTERVAL '3 minutes'
GROUP BY organization_id, device_id
HAVING max(latest_temp) > 85`, compiledSQL(t, compiled))
}

// Scope must survive the config-store round trip; the compiled rule itself carries no
// config annotation (Grafana copies annotations onto every alert instance).
func TestScopeRoundTripsThroughConfigStore(t *testing.T) {
	cfg := scopedOfflineConfig(&RuleScope{
		SiteIDs: []int64{3}, DeviceIDs: []string{"dev-a"},
		BuildingIDs: []int64{2}, RackIDs: []int64{4}, GroupIDs: []int64{6},
	})
	fake := &fakeGrafanaRules{}
	configs := newFakeRuleConfigStore()
	svc := NewService(fake.server(t), nil, nil, configs, nil, nil, fakeScopeLookup{
		sites:     map[int64]bool{3: true},
		buildings: map[int64]bool{2: true},
		sets:      map[string]map[int64]bool{"rack": {4: true}, "group": {6: true}},
	}, DestinationPolicy{})

	created, err := svc.CreateRule(context.Background(), 7, cfg, RouteModeDefault, nil)
	require.NoError(t, err)
	require.NotNil(t, created.Config)
	assert.Equal(t, cfg, *created.Config)
	assert.NotContains(t, fake.created.Annotations, "proto_fleet_config")
	assert.Equal(t, cfg, configs.configs[created.ID])

	// Lists read the config back from the store.
	fake.listed = []GrafanaAlertRule{*fake.created}
	rules, err := svc.ListRules(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, rules, 1)
	require.NotNil(t, rules[0].Config)
	assert.Equal(t, cfg, *rules[0].Config)
}

// The union predicate and site column render at per-template indents; cover every template
// with both lists set so a drifted indent can't hide behind the single-sided goldens above.
func TestCompileScopedUnionSQLAllTemplates(t *testing.T) {
	scope := &RuleScope{SiteIDs: []int64{3}, DeviceIDs: []string{"dev-a"}}
	cteUnion := `      AND device_id IN (
        SELECT device_id FROM fleet_device_placement
        WHERE org_id = 7
          AND (device_id IN ('dev-a') OR site_id IN (3))
      )
`

	t.Run("pct_expected hashrate", func(t *testing.T) {
		compiled, err := compileUserRule(7, "pfu-test", RuleConfig{
			Name: "Slow hashing", DurationSeconds: 600,
			Hashrate: &HashrateRuleConfig{Mode: HashrateModePctExpected, Value: 75},
			Scope:    scope,
		})
		require.NoError(t, err)
		assert.Equal(t, `SELECT
    organization_id,
    device_id,
    COALESCE((SELECT site_id::text FROM fleet_device_placement
     WHERE org_id = 7 AND device_id = notification_metric_sample.device_id LIMIT 1), '') AS site_id,
    1 AS value
FROM notification_metric_sample
WHERE metric = 'fleet_device_hashing'
  AND organization_id = '7'
  AND device_id IN (
    SELECT device_id FROM fleet_device_placement
    WHERE org_id = 7
      AND (device_id IN ('dev-a') OR site_id IN (3))
  )
  AND time > NOW() - INTERVAL '10 minutes'
GROUP BY organization_id, device_id
HAVING last(value, time) < 0.75`, compiledSQL(t, compiled))
	})

	t.Run("absolute hashrate", func(t *testing.T) {
		compiled, err := compileUserRule(7, "pfu-test", RuleConfig{
			Name: "Slow hashing", DurationSeconds: 600,
			Hashrate: &HashrateRuleConfig{Mode: HashrateModeAbsolute, Value: 90, Unit: HashrateUnitTerahash},
			Scope:    scope,
		})
		require.NoError(t, err)
		sql := compiledSQL(t, compiled)
		assert.Contains(t, sql, cteUnion)
		assert.Contains(t, sql, "\n    COALESCE((SELECT site_id::text FROM fleet_device_placement\n     WHERE org_id = 7 AND device_id = obs.device_id LIMIT 1), '') AS site_id,\n")
	})

	t.Run("temperature", func(t *testing.T) {
		compiled, err := compileUserRule(7, "pfu-test", RuleConfig{
			Name: "Running hot", DurationSeconds: 900,
			Temperature: &TemperatureRuleConfig{MaxCelsius: 85},
			Scope:       scope,
		})
		require.NoError(t, err)
		sql := compiledSQL(t, compiled)
		assert.Contains(t, sql, cteUnion)
		assert.Contains(t, sql, "\n    COALESCE((SELECT site_id::text FROM fleet_device_placement\n     WHERE org_id = 7 AND device_id = latest_per_kind.device_id LIMIT 1), '') AS site_id,\n")
	})
}

func TestCreateRuleRejectsForeignSiteScope(t *testing.T) {
	fake := &fakeGrafanaRules{}
	svc := NewService(fake.server(t), nil, nil, nil, nil, nil, fakeScopeLookup{sites: map[int64]bool{3: true}}, DestinationPolicy{})

	_, err := svc.CreateRule(context.Background(), 7, scopedOfflineConfig(&RuleScope{SiteIDs: []int64{4}}), RouteModeDefault, nil)
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Nil(t, fake.created)
}

func TestCreateRuleNormalizesAndCompilesScope(t *testing.T) {
	fake := &fakeGrafanaRules{}
	svc := NewService(fake.server(t), nil, nil, nil, nil, nil, fakeScopeLookup{sites: map[int64]bool{3: true, 5: true}}, DestinationPolicy{})

	rule, err := svc.CreateRule(context.Background(), 7, scopedOfflineConfig(&RuleScope{SiteIDs: []int64{5, 3, 5}}), RouteModeDefault, nil)
	require.NoError(t, err)
	require.NotNil(t, fake.created)
	assert.Contains(t, compiledSQL(t, *fake.created), "AND site_id IN (3, 5)")
	require.NotNil(t, rule.Config)
	assert.Equal(t, &RuleScope{SiteIDs: []int64{3, 5}}, rule.Config.Scope)
}

// A site deleted after a rule was scoped to it must not brick unrelated edits: the update may
// keep the stored id (visible-but-inert for that site) while newly added ids are still validated.
func TestUpdateRuleKeepsScopeWithDeletedSite(t *testing.T) {
	storedCfg := scopedOfflineConfig(&RuleScope{SiteIDs: []int64{9}})
	stored, err := compileUserRule(7, "pfu-mine", storedCfg)
	require.NoError(t, err)
	fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{stored}}
	configs := newFakeRuleConfigStore()
	configs.configs["pfu-mine"] = storedCfg
	// Site 9 no longer resolves (deleted); only site 3 is live.
	svc := NewService(fake.server(t), nil, nil, configs, nil, nil, fakeScopeLookup{sites: map[int64]bool{3: true}}, DestinationPolicy{})

	renamed := scopedOfflineConfig(&RuleScope{SiteIDs: []int64{9}})
	renamed.Name = "Renamed"
	updated, err := svc.UpdateRule(context.Background(), 7, "pfu-mine", renamed)
	require.NoError(t, err)
	assert.Equal(t, "Renamed", updated.Name)

	added := scopedOfflineConfig(&RuleScope{SiteIDs: []int64{9, 4}})
	_, err = svc.UpdateRule(context.Background(), 7, "pfu-mine", added)
	require.Error(t, err, "adding another dead site id must still be rejected")
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

// Clearing the scope is the documented rollback path: an explicit org-wide update must
// compile back to the unscoped SQL and drop the scope from the stored config.
func TestUpdateRuleClearsScope(t *testing.T) {
	stored, err := compileUserRule(7, "pfu-mine", scopedOfflineConfig(&RuleScope{SiteIDs: []int64{3}}))
	require.NoError(t, err)
	fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{stored}}
	svc := NewService(fake.server(t), nil, nil, nil, nil, nil, fakeScopeLookup{sites: map[int64]bool{3: true}}, DestinationPolicy{})

	updated, err := svc.UpdateRule(context.Background(), 7, "pfu-mine", orgWideOfflineConfig("Offline too long", 1800))
	require.NoError(t, err)
	require.NotNil(t, fake.updated)
	sql := compiledSQL(t, *fake.updated)
	assert.NotContains(t, sql, "site_id")
	require.NotNil(t, updated.Config)
	assert.Nil(t, updated.Config.Scope)
}

// A stale pre-scope client's update arrives with no scope at all; accepting it would silently
// widen a scoped rule to every miner. Explicit org-wide intent still unscopes.
func TestUpdateRuleRejectsScopelessWriteOfScopedRule(t *testing.T) {
	storedCfg := scopedOfflineConfig(&RuleScope{SiteIDs: []int64{3}})
	stored, err := compileUserRule(7, "pfu-mine", storedCfg)
	require.NoError(t, err)
	fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{stored}}
	configs := newFakeRuleConfigStore()
	configs.configs["pfu-mine"] = storedCfg
	svc := NewService(fake.server(t), nil, nil, configs, nil, nil, fakeScopeLookup{sites: map[int64]bool{3: true}}, DestinationPolicy{})

	_, err = svc.UpdateRule(context.Background(), 7, "pfu-mine", offlineConfig("Renamed", 900))
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Nil(t, fake.updated, "the stale write must not reach Grafana")
	assert.Equal(t, storedCfg, configs.configs["pfu-mine"], "the stored scope survives")

	updated, err := svc.UpdateRule(context.Background(), 7, "pfu-mine", orgWideOfflineConfig("Renamed", 900))
	require.NoError(t, err)
	require.NotNil(t, updated.Config)
	assert.Nil(t, updated.Config.Scope)
}

// The config row follows the confirmed Grafana outcome: a rejected update leaves it untouched, a
// commit Grafana errored after still publishes — it must never report a config Grafana isn't evaluating.
func TestUpdateRuleConfigFollowsGrafanaOutcome(t *testing.T) {
	storedCfg := scopedOfflineConfig(&RuleScope{SiteIDs: []int64{3}})
	stored, err := compileUserRule(7, "pfu-mine", storedCfg)
	require.NoError(t, err)

	t.Run("rejected update leaves prior row untouched", func(t *testing.T) {
		fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{stored}, updateErr: true}
		configs := newFakeRuleConfigStore()
		configs.configs["pfu-mine"] = storedCfg
		svc := NewService(fake.server(t), nil, nil, configs, nil, nil, fakeScopeLookup{sites: map[int64]bool{3: true}}, DestinationPolicy{})

		_, err := svc.UpdateRule(context.Background(), 7, "pfu-mine", orgWideOfflineConfig("Renamed", 900))
		require.Error(t, err)
		assert.Equal(t, storedCfg, configs.configs["pfu-mine"])
	})

	t.Run("rejected update writes no row", func(t *testing.T) {
		fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{stored}, updateErr: true}
		configs := newFakeRuleConfigStore()
		svc := NewService(fake.server(t), nil, nil, configs, nil, nil, fakeScopeLookup{sites: map[int64]bool{3: true}}, DestinationPolicy{})

		_, err := svc.UpdateRule(context.Background(), 7, "pfu-mine", offlineConfig("Renamed", 900))
		require.Error(t, err)
		assert.NotContains(t, configs.configs, "pfu-mine")
	})

	// A PUT error does not prove the update was rejected: when the live rule already carries the
	// change (timeout after commit), the config must publish or reads would misreport the scope.
	t.Run("committed despite error publishes new config", func(t *testing.T) {
		fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{stored}, updateErrAfterCommit: true}
		configs := newFakeRuleConfigStore()
		configs.configs["pfu-mine"] = storedCfg
		svc := NewService(fake.server(t), nil, nil, configs, nil, nil, fakeScopeLookup{sites: map[int64]bool{3: true}}, DestinationPolicy{})

		newCfg := orgWideOfflineConfig("Renamed", 900)
		_, err := svc.UpdateRule(context.Background(), 7, "pfu-mine", newCfg)
		require.Error(t, err, "the caller still sees the PUT failure")
		assert.Equal(t, newCfg, configs.configs["pfu-mine"])
	})

	// An upsert error does not prove the row was not written: when the probe shows both sides
	// already agree, restoring Grafana would CREATE the divergence, so the update reports success.
	t.Run("upsert committed despite error converges", func(t *testing.T) {
		fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{stored}}
		configs := newFakeRuleConfigStore()
		configs.configs["pfu-mine"] = storedCfg
		configs.upsertErrAfterCommit = true
		svc := NewService(fake.server(t), nil, nil, configs, nil, nil, fakeScopeLookup{sites: map[int64]bool{3: true}}, DestinationPolicy{})

		newCfg := orgWideOfflineConfig("Renamed", 900)
		updated, err := svc.UpdateRule(context.Background(), 7, "pfu-mine", newCfg)
		require.NoError(t, err)
		assert.Equal(t, "Renamed", updated.Name)
		assert.Equal(t, newCfg, configs.configs["pfu-mine"])
		require.NotNil(t, fake.updated)
		assert.Equal(t, "Renamed", fake.updated.Title, "no restore may follow a converged write")
	})

	// The upsert is idempotent, so a transient failure converges by retrying
	// inside the same request: the caller sees a clean success.
	t.Run("transient upsert failure converges by retrying", func(t *testing.T) {
		fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{stored}}
		configs := newFakeRuleConfigStore()
		configs.configs["pfu-mine"] = storedCfg
		configs.upsertErr = fmt.Errorf("db blip")
		configs.upsertErrOnce = true
		svc := NewService(fake.server(t), nil, nil, configs, nil, nil, fakeScopeLookup{sites: map[int64]bool{3: true}}, DestinationPolicy{})

		newCfg := orgWideOfflineConfig("Renamed", 900)
		updated, err := svc.UpdateRule(context.Background(), 7, "pfu-mine", newCfg)
		require.NoError(t, err)
		assert.Equal(t, "Renamed", updated.Name)
		assert.Equal(t, newCfg, configs.configs["pfu-mine"])
	})

	// When the row's state cannot be confirmed, restoring is as likely to create the divergence
	// as to fix it: leave both sides for the out-of-sync flag and tell the caller to re-save.
	t.Run("unknown row state skips the restore", func(t *testing.T) {
		fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{stored}}
		configs := newFakeRuleConfigStore()
		configs.configs["pfu-mine"] = storedCfg
		configs.upsertErr = fmt.Errorf("db down")
		configs.getErrAfterUpsert = fmt.Errorf("db still down")
		svc := NewService(fake.server(t), nil, nil, configs, nil, nil, fakeScopeLookup{sites: map[int64]bool{3: true}}, DestinationPolicy{})

		_, err := svc.UpdateRule(context.Background(), 7, "pfu-mine", orgWideOfflineConfig("Renamed", 900))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "re-save")
		require.NotNil(t, fake.updated)
		assert.Equal(t, "Renamed", fake.updated.Title, "no restore may run against an unknown row state")
	})

	// The compensating restore can itself fail: the caller must not be told the update was rolled
	// back (Grafana may keep evaluating the new SQL), and the untouched row lets reads flag it.
	t.Run("failed restore is not reported as a rollback", func(t *testing.T) {
		fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{stored}, updateErrAfterFirst: true}
		configs := newFakeRuleConfigStore()
		configs.configs["pfu-mine"] = storedCfg
		configs.upsertErr = fmt.Errorf("db down")
		svc := NewService(fake.server(t), nil, nil, configs, nil, nil, fakeScopeLookup{sites: map[int64]bool{3: true}}, DestinationPolicy{})

		_, err := svc.UpdateRule(context.Background(), 7, "pfu-mine", orgWideOfflineConfig("Renamed", 900))
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "rolled back")
		assert.Contains(t, err.Error(), "re-save")
		assert.Equal(t, storedCfg, configs.configs["pfu-mine"], "the row still holds the old config")
	})

	// The ambiguous-failure probe can itself fail: the row must stay untouched (only ever BEHIND
	// the live rule), and once the commit becomes visible the next list flags the divergence.
	t.Run("failed probe leaves the row behind and reads flag it", func(t *testing.T) {
		fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{stored}, updateErrAfterCommit: true, getRuleErrAfterUpdate: true}
		configs := newFakeRuleConfigStore()
		configs.configs["pfu-mine"] = storedCfg
		svc := NewService(fake.server(t), nil, nil, configs, nil, nil, fakeScopeLookup{sites: map[int64]bool{3: true}}, DestinationPolicy{})

		_, err := svc.UpdateRule(context.Background(), 7, "pfu-mine", orgWideOfflineConfig("Renamed", 900))
		require.Error(t, err)
		assert.Equal(t, storedCfg, configs.configs["pfu-mine"], "an unproven commit must not publish the new config")

		rules, err := svc.ListRules(context.Background(), 7)
		require.NoError(t, err)
		require.Len(t, rules, 1)
		assert.True(t, rules[0].ConfigOutOfSync)
	})

	// The rule committed but the config write failed: restore the previous rule body so the
	// live SQL keeps matching the stored config, and surface a retriable error.
	t.Run("committed but config write failed restores the rule", func(t *testing.T) {
		fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{stored}}
		configs := newFakeRuleConfigStore()
		configs.configs["pfu-mine"] = storedCfg
		configs.upsertErr = fmt.Errorf("db down")
		svc := NewService(fake.server(t), nil, nil, configs, nil, nil, fakeScopeLookup{sites: map[int64]bool{3: true}}, DestinationPolicy{})

		_, err := svc.UpdateRule(context.Background(), 7, "pfu-mine", orgWideOfflineConfig("Renamed", 900))
		require.Error(t, err)
		require.NotNil(t, fake.updated, "the compensating restore is the last write")
		assert.Equal(t, stored.Title, fake.updated.Title, "the previous rule body is restored")
		assert.Equal(t, storedCfg, configs.configs["pfu-mine"], "the row keeps matching the restored rule")
	})
}

// An interrupted save must surface on reads: the stored config is only authoritative
// while it compiles to the SQL the live rule evaluates.
func TestListRulesFlagsConfigOutOfSync(t *testing.T) {
	liveCfg := scopedOfflineConfig(&RuleScope{SiteIDs: []int64{3}})
	live, err := compileUserRule(7, "pfu-mine", liveCfg)
	require.NoError(t, err)
	fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{live}}
	configs := newFakeRuleConfigStore()
	svc := NewService(fake.server(t), nil, nil, configs, nil, nil, fakeScopeLookup{sites: map[int64]bool{3: true}}, DestinationPolicy{})

	// Stored config matches the live SQL: no flag.
	configs.configs["pfu-mine"] = liveCfg
	rules, err := svc.ListRules(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, rules, 1)
	assert.False(t, rules[0].ConfigOutOfSync)

	// Stored config diverges (e.g. the rule write landed but the config write
	// was the interrupted half): flagged, config still returned for the re-save.
	configs.configs["pfu-mine"] = scopedOfflineConfig(&RuleScope{SiteIDs: []int64{3, 5}})
	rules, err = svc.ListRules(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, rules, 1)
	assert.True(t, rules[0].ConfigOutOfSync)
	require.NotNil(t, rules[0].Config)
}

// A mutation response must not erase a detected divergence: its best-effort
// decoration applies the same config/SQL comparison as the list path.
func TestPauseRuleKeepsConfigOutOfSyncFlag(t *testing.T) {
	live, err := compileUserRule(7, "pfu-mine", scopedOfflineConfig(&RuleScope{SiteIDs: []int64{3}}))
	require.NoError(t, err)
	fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{live}}
	configs := newFakeRuleConfigStore()
	// The stored config diverges from the SQL the live rule evaluates.
	configs.configs["pfu-mine"] = scopedOfflineConfig(&RuleScope{SiteIDs: []int64{3, 5}})
	svc := NewService(fake.server(t), nil, nil, configs, nil, nil, fakeScopeLookup{sites: map[int64]bool{3: true, 5: true}}, DestinationPolicy{})

	paused, err := svc.PauseRule(context.Background(), 7, "pfu-mine", "alice")
	require.NoError(t, err)
	assert.True(t, paused.ConfigOutOfSync)
}

// Name/duration edits change Title/For but not SQL, and equivalent thresholds (1 PH/s vs 1000 TH/s)
// share SQL with different annotations: an interrupted save must flag all of them, not just SQL drift.
func TestListRulesFlagsConfigOutOfSyncBeyondSQL(t *testing.T) {
	liveCfg := offlineConfig("Offline too long", 1800)
	phCfg := RuleConfig{
		Name: "Low hashrate", DurationSeconds: 1800,
		Hashrate: &HashrateRuleConfig{Mode: HashrateModeAbsolute, Value: 1, Unit: HashrateUnitPetahash},
	}
	thCfg := phCfg
	thCfg.Hashrate = &HashrateRuleConfig{Mode: HashrateModeAbsolute, Value: 1000, Unit: HashrateUnitTerahash}

	renamed := liveCfg
	renamed.Name = "Renamed"
	longer := liveCfg
	longer.DurationSeconds = 3600

	for name, tc := range map[string]struct{ live, stored RuleConfig }{
		"name only":        {live: liveCfg, stored: renamed},
		"duration only":    {live: liveCfg, stored: longer},
		"equivalent PH/TH": {live: phCfg, stored: thCfg},
	} {
		t.Run(name, func(t *testing.T) {
			live, err := compileUserRule(7, "pfu-mine", tc.live)
			require.NoError(t, err)
			fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{live}}
			configs := newFakeRuleConfigStore()
			configs.configs["pfu-mine"] = tc.stored
			svc := NewService(fake.server(t), nil, nil, configs, nil, nil, nil, DestinationPolicy{})

			rules, err := svc.ListRules(context.Background(), 7)
			require.NoError(t, err)
			require.Len(t, rules, 1)
			assert.True(t, rules[0].ConfigOutOfSync)
		})
	}
}

// Rule data with no recognizable refId-A rawSql is itself a divergence — a config always
// compiles to non-empty SQL — so it must flag, not read as in sync with no query at all.
func TestListRulesFlagsConfigOutOfSyncOnMissingSQL(t *testing.T) {
	liveCfg := offlineConfig("Offline too long", 1800)
	for name, data := range map[string]json.RawMessage{
		"missing data":   nil,
		"malformed data": json.RawMessage(`{"not":"a list"}`),
		"no refId A":     json.RawMessage(`[{"refId":"B","model":{}}]`),
	} {
		t.Run(name, func(t *testing.T) {
			live, err := compileUserRule(7, "pfu-mine", liveCfg)
			require.NoError(t, err)
			live.Data = data
			fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{live}}
			configs := newFakeRuleConfigStore()
			configs.configs["pfu-mine"] = liveCfg
			svc := NewService(fake.server(t), nil, nil, configs, nil, nil, nil, DestinationPolicy{})

			rules, err := svc.ListRules(context.Background(), 7)
			require.NoError(t, err)
			require.Len(t, rules, 1)
			assert.True(t, rules[0].ConfigOutOfSync)
		})
	}
}

// Ambiguous create failures deliberately keep config rows (see CreateRule); a successful
// authoritative list reclaims the never-created ones without touching live rules' rows.
func TestListRulesSweepsOrphanConfigs(t *testing.T) {
	liveCfg := offlineConfig("Offline too long", 1800)
	live, err := compileUserRule(7, "pfu-mine", liveCfg)
	require.NoError(t, err)
	fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{live}}
	configs := newFakeRuleConfigStore()
	configs.configs["pfu-mine"] = liveCfg
	configs.configs["pfu-orphan"] = offlineConfig("Never created", 1800)
	svc := NewService(fake.server(t), nil, nil, configs, nil, nil, nil, DestinationPolicy{})

	rules, err := svc.ListRules(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, rules, 1)
	assert.Contains(t, configs.configs, "pfu-mine")
	assert.NotContains(t, configs.configs, "pfu-orphan")
}

// Pre-000135 rules hold their config only in the legacy annotation: a missing store row must
// fall back to it so those rules stay editable, while a store row wins over the annotation.
func TestListRulesFallsBackToLegacyAnnotationConfig(t *testing.T) {
	legacyCfg := offlineConfig("Offline too long", 1800)
	live, err := compileUserRule(7, "pfu-legacy", legacyCfg)
	require.NoError(t, err)
	legacyJSON, err := json.Marshal(legacyCfg)
	require.NoError(t, err)
	live.Annotations[ruleAnnotationConfig] = string(legacyJSON)
	fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{live}}
	configs := newFakeRuleConfigStore()
	svc := NewService(fake.server(t), nil, nil, configs, nil, nil, nil, DestinationPolicy{})

	rules, err := svc.ListRules(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, rules, 1)
	require.NotNil(t, rules[0].Config)
	assert.Equal(t, legacyCfg, *rules[0].Config)
	assert.False(t, rules[0].ConfigOutOfSync)

	stored := offlineConfig("Renamed", 900)
	configs.configs["pfu-legacy"] = stored
	rules, err = svc.ListRules(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, rules, 1)
	require.NotNil(t, rules[0].Config)
	assert.Equal(t, stored, *rules[0].Config)
}

// A corrupt legacy annotation degrades to "no editable config" rather than failing the list. A
// template mismatch (a manual Grafana edit a re-save would replay onto the rule) is hidden the same way.
func TestListRulesIgnoresInvalidLegacyAnnotationConfig(t *testing.T) {
	live, err := compileUserRule(7, "pfu-legacy", offlineConfig("Offline too long", 1800))
	require.NoError(t, err)
	for name, raw := range map[string]string{
		"malformed JSON":               `{"name":`,
		"fails validation":             `{"name":"x","duration_seconds":1}`,
		"template disagrees with rule": `{"name":"Wrong template","duration_seconds":1800,"temperature":{"max_celsius":80}}`,
	} {
		t.Run(name, func(t *testing.T) {
			live.Annotations[ruleAnnotationConfig] = raw
			fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{live}}
			svc := NewService(fake.server(t), nil, nil, newFakeRuleConfigStore(), nil, nil, nil, DestinationPolicy{})

			rules, err := svc.ListRules(context.Background(), 7)
			require.NoError(t, err)
			require.Len(t, rules, 1)
			assert.Nil(t, rules[0].Config)
		})
	}
}

// A legacy rule's first update must stage the annotation config as a store row before touching
// Grafana: proceeding past a failed stage would let a committed PUT strip the rule's only config.
func TestUpdateRuleAbortsWhenLegacyConfigStageFails(t *testing.T) {
	legacyCfg := offlineConfig("Offline too long", 1800)
	live, err := compileUserRule(7, "pfu-legacy", legacyCfg)
	require.NoError(t, err)
	legacyJSON, err := json.Marshal(legacyCfg)
	require.NoError(t, err)
	live.Annotations[ruleAnnotationConfig] = string(legacyJSON)
	fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{live}}
	configs := newFakeRuleConfigStore()
	configs.upsertErr = fmt.Errorf("db down")
	svc := NewService(fake.server(t), nil, nil, configs, nil, nil, nil, DestinationPolicy{})

	_, err = svc.UpdateRule(context.Background(), 7, "pfu-legacy", offlineConfig("Renamed", 900))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stage legacy rule config")
	assert.Nil(t, fake.updated, "Grafana must not be written when the stage fails")
	assert.Equal(t, string(legacyJSON), fake.listed[0].Annotations[ruleAnnotationConfig], "the annotation must survive an aborted update")
}

// The PUT commits (stripping the annotation) and every config write after it fails: the staged row
// is the rule's only surviving config — reads must serve it and flag the divergence, not render config-less.
func TestUpdateRuleKeepsStagedLegacyConfigThroughPublishFailure(t *testing.T) {
	legacyCfg := offlineConfig("Offline too long", 1800)
	live, err := compileUserRule(7, "pfu-legacy", legacyCfg)
	require.NoError(t, err)
	legacyJSON, err := json.Marshal(legacyCfg)
	require.NoError(t, err)
	live.Annotations[ruleAnnotationConfig] = string(legacyJSON)
	fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{live}, updateErrAfterCommit: true}
	configs := newFakeRuleConfigStore()
	configs.upsertErrAfterFirst = true
	svc := NewService(fake.server(t), nil, nil, configs, nil, nil, nil, DestinationPolicy{})

	_, err = svc.UpdateRule(context.Background(), 7, "pfu-legacy", offlineConfig("Renamed", 900))
	require.Error(t, err, "the caller still sees the PUT failure")
	assert.Equal(t, legacyCfg, configs.configs["pfu-legacy"], "the staged row must survive the failed publish")

	rules, err := svc.ListRules(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, rules, 1)
	require.NotNil(t, rules[0].Config, "the rule must stay editable")
	assert.Equal(t, legacyCfg, *rules[0].Config)
	assert.True(t, rules[0].ConfigOutOfSync, "reads must flag the divergence for a converging re-save")
}

// The best-effort mutation decoration applies the same legacy fallback as the
// list path, so pausing a pre-migration rule still returns its config.
func TestPauseRuleFallsBackToLegacyAnnotationConfig(t *testing.T) {
	legacyCfg := offlineConfig("Offline too long", 1800)
	live, err := compileUserRule(7, "pfu-legacy", legacyCfg)
	require.NoError(t, err)
	legacyJSON, err := json.Marshal(legacyCfg)
	require.NoError(t, err)
	live.Annotations[ruleAnnotationConfig] = string(legacyJSON)
	fake := &fakeGrafanaRules{listed: []GrafanaAlertRule{live}}
	svc := NewService(fake.server(t), nil, nil, newFakeRuleConfigStore(), nil, nil, nil, DestinationPolicy{})

	paused, err := svc.PauseRule(context.Background(), 7, "pfu-legacy", "alice")
	require.NoError(t, err)
	require.NotNil(t, paused.Config)
	assert.Equal(t, legacyCfg, *paused.Config)
}
