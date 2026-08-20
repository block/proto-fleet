package sqlstores

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

type authorizationEnvelopeQuerier struct {
	sqlc.Querier
	deviceRows         []sqlc.LockCurtailmentResponseProfileDeviceSitesByOrgRow
	topologyMemberRows []sqlc.LockCurtailmentTopologyMemberDeviceSitesByOrgRow
	buildingRows       []sqlc.ListCurtailmentBuildingScopeCoverageRow
}

func (q authorizationEnvelopeQuerier) LockBuildingForWrite(
	_ context.Context,
	params sqlc.LockBuildingForWriteParams,
) (int64, error) {
	return params.ID, nil
}

func (q authorizationEnvelopeQuerier) LockRackPlacementForWrite(
	context.Context,
	sqlc.LockRackPlacementForWriteParams,
) (sqlc.LockRackPlacementForWriteRow, error) {
	return sqlc.LockRackPlacementForWriteRow{}, nil
}

func (q authorizationEnvelopeQuerier) LockCurtailmentTopologyMemberDeviceSitesByOrg(
	context.Context,
	sqlc.LockCurtailmentTopologyMemberDeviceSitesByOrgParams,
) ([]sqlc.LockCurtailmentTopologyMemberDeviceSitesByOrgRow, error) {
	return q.topologyMemberRows, nil
}

func (q authorizationEnvelopeQuerier) LockCurtailmentResponseProfileDeviceSitesByOrg(
	context.Context,
	sqlc.LockCurtailmentResponseProfileDeviceSitesByOrgParams,
) ([]sqlc.LockCurtailmentResponseProfileDeviceSitesByOrgRow, error) {
	return q.deviceRows, nil
}

func (q authorizationEnvelopeQuerier) ListCurtailmentBuildingScopeCoverage(
	context.Context,
	sqlc.ListCurtailmentBuildingScopeCoverageParams,
) ([]sqlc.ListCurtailmentBuildingScopeCoverageRow, error) {
	return q.buildingRows, nil
}

func TestBuildAuthorizationEnvelopeJSONSeparatesTopologyAndFanCoverage(t *testing.T) {
	q := authorizationEnvelopeQuerier{buildingRows: []sqlc.ListCurtailmentBuildingScopeCoverageRow{
		{SelectorID: 7, ResourceSiteID: sql.NullInt64{Int64: 11, Valid: true}},
		{MemberSiteID: sql.NullInt64{Int64: 12, Valid: true}, MemberDeviceID: sql.NullInt64{Int64: 101, Valid: true}},
		{MemberSiteID: sql.NullInt64{Int64: 11, Valid: true}, MemberDeviceID: sql.NullInt64{Int64: 102, Valid: true}},
	}}

	raw, err := buildAuthorizationEnvelopeJSON(
		t.Context(), q, 42, models.ScopeTypeMixed,
		[]byte(`{"scope_schema_version":1,"building_ids":[7]}`), []int64{14, 13, 14}, nil, nil,
	)
	require.NoError(t, err)

	var envelope models.AuthorizationEnvelope
	require.NoError(t, json.Unmarshal(raw, &envelope))
	assert.Equal(t, models.AuthorizationEnvelopeSchemaVersion, envelope.SchemaVersion)
	assert.Equal(t, []int64{11}, envelope.SelectedResourceSiteIDs)
	assert.Equal(t, []int64{11, 12}, envelope.CurrentMemberSiteIDs)
	assert.False(t, envelope.MinerScopeUnbounded)
	assert.Equal(t, []int64{13, 14}, envelope.FacilityFanSiteIDs)
	assert.False(t, envelope.FacilityFanScopeUnbounded)
}

func TestBuildAuthorizationEnvelopeJSONLocksTopologyTargetsAndIncludesTheirSites(t *testing.T) {
	q := authorizationEnvelopeQuerier{
		topologyMemberRows: []sqlc.LockCurtailmentTopologyMemberDeviceSitesByOrgRow{
			{DeviceIdentifier: "miner-a", SiteID: sql.NullInt64{Int64: 12, Valid: true}},
		},
		buildingRows: []sqlc.ListCurtailmentBuildingScopeCoverageRow{
			{SelectorID: 7, ResourceSiteID: sql.NullInt64{Int64: 11, Valid: true}},
		},
	}

	raw, err := buildAuthorizationEnvelopeJSON(
		t.Context(), q, 42, models.ScopeTypeMixed,
		[]byte(`{"scope_schema_version":1,"building_ids":[7]}`), nil, nil, []string{"miner-a"},
	)
	require.NoError(t, err)

	var envelope models.AuthorizationEnvelope
	require.NoError(t, json.Unmarshal(raw, &envelope))
	assert.Equal(t, []int64{11}, envelope.SelectedResourceSiteIDs)
	assert.Equal(t, []int64{12}, envelope.CurrentMemberSiteIDs)
}

func TestBuildAuthorizationEnvelopeJSONRejectsTargetThatLeftTopology(t *testing.T) {
	q := authorizationEnvelopeQuerier{
		topologyMemberRows: []sqlc.LockCurtailmentTopologyMemberDeviceSitesByOrgRow{
			{DeviceIdentifier: "miner-a", SiteID: sql.NullInt64{Int64: 12, Valid: true}},
		},
		buildingRows: []sqlc.ListCurtailmentBuildingScopeCoverageRow{
			{SelectorID: 7, ResourceSiteID: sql.NullInt64{Int64: 11, Valid: true}},
		},
	}

	_, err := buildAuthorizationEnvelopeJSON(
		t.Context(), q, 42, models.ScopeTypeMixed,
		[]byte(`{"scope_schema_version":1,"building_ids":[7]}`), nil, nil, []string{"miner-b"},
	)
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "topology changed before save")
}

func TestBuildAuthorizationEnvelopeJSONMarksUnassignedMinerScopeUnbounded(t *testing.T) {
	q := authorizationEnvelopeQuerier{deviceRows: []sqlc.LockCurtailmentResponseProfileDeviceSitesByOrgRow{
		{DeviceIdentifier: "miner-a", SiteID: sql.NullInt64{Int64: 21, Valid: true}},
		{DeviceIdentifier: "miner-b"},
	}}

	raw, err := buildAuthorizationEnvelopeJSON(
		t.Context(), q, 42, models.ScopeTypeDeviceList,
		[]byte(`{"device_identifiers":["miner-b","miner-a"]}`), nil, nil, nil,
	)
	require.NoError(t, err)

	var envelope models.AuthorizationEnvelope
	require.NoError(t, json.Unmarshal(raw, &envelope))
	assert.Empty(t, envelope.SelectedResourceSiteIDs)
	assert.Equal(t, []int64{21}, envelope.CurrentMemberSiteIDs)
	assert.True(t, envelope.MinerScopeUnbounded)
	assert.NotNil(t, envelope.SelectedResourceSiteIDs)
	assert.NotNil(t, envelope.FacilityFanSiteIDs)
	assert.Empty(t, envelope.FacilityFanSiteIDs)
}

func TestBuildAuthorizationEnvelopeJSONRejectsChangedDeviceAuthorization(t *testing.T) {
	expectedSiteID := int64(22)
	q := authorizationEnvelopeQuerier{deviceRows: []sqlc.LockCurtailmentResponseProfileDeviceSitesByOrgRow{
		{DeviceIdentifier: "miner-a", SiteID: sql.NullInt64{Int64: 21, Valid: true}},
	}}

	_, err := buildAuthorizationEnvelopeJSON(
		t.Context(), q, 42, models.ScopeTypeDeviceList,
		[]byte(`{"device_identifiers":["miner-a"]}`), nil,
		map[string]*int64{"miner-a": &expectedSiteID},
		nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authorization changed")
}

func TestBuildAuthorizationEnvelopeJSONKeepsMissingSnapshotTargetsUnbounded(t *testing.T) {
	raw, err := buildAuthorizationEnvelopeJSON(
		t.Context(), authorizationEnvelopeQuerier{}, 42, models.ScopeTypeDeviceList,
		[]byte(`{"device_identifiers":["missing-miner"]}`), nil,
		map[string]*int64{},
		nil,
	)
	require.NoError(t, err)

	var envelope models.AuthorizationEnvelope
	require.NoError(t, json.Unmarshal(raw, &envelope))
	assert.True(t, envelope.MinerScopeUnbounded)
	assert.NotNil(t, envelope.CurrentMemberSiteIDs)
	assert.Empty(t, envelope.CurrentMemberSiteIDs)
}

func TestBuildAuthorizationEnvelopeJSONFailsClosedForMissingSelector(t *testing.T) {
	_, err := buildAuthorizationEnvelopeJSON(
		t.Context(), authorizationEnvelopeQuerier{}, 42, "", []byte(`{"scope_schema_version":1}`), nil, nil, nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must include a recognized selector")
}

func TestBuildAuthorizationEnvelopeJSONRejectsInvalidScopeContract(t *testing.T) {
	tests := []struct {
		name      string
		scopeJSON string
		errorText string
	}{
		{
			name:      "unsupported schema version",
			scopeJSON: `{"scope_schema_version":999,"site_id":7}`,
			errorText: "unsupported scope_schema_version",
		},
		{
			name:      "unknown selector",
			scopeJSON: `{"site_id":7,"future_selector_ids":[8]}`,
			errorText: "unknown field",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := buildAuthorizationEnvelopeJSON(
				t.Context(), authorizationEnvelopeQuerier{}, 42, "", []byte(test.scopeJSON), nil, nil, nil,
			)
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.errorText)
		})
	}
}

func TestBuildAuthorizationEnvelopeJSONTreatsOnlyEmptyObjectAsImplicitWholeOrg(t *testing.T) {
	raw, err := buildAuthorizationEnvelopeJSON(
		t.Context(), authorizationEnvelopeQuerier{}, 42, "", []byte(`{ }`), nil, nil, nil,
	)
	require.NoError(t, err)

	var envelope models.AuthorizationEnvelope
	require.NoError(t, json.Unmarshal(raw, &envelope))
	assert.True(t, envelope.MinerScopeUnbounded)
	assert.NotNil(t, envelope.SelectedResourceSiteIDs)
	assert.NotNil(t, envelope.CurrentMemberSiteIDs)
	assert.NotNil(t, envelope.FacilityFanSiteIDs)
	assert.Empty(t, envelope.SelectedResourceSiteIDs)
}
