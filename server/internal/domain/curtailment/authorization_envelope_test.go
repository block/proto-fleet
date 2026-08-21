package curtailment

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
)

func TestAuthorizationEnvelopeFromJSON(t *testing.T) {
	t.Parallel()

	valid := `{
		"schema_version":1,
		"selected_resource_site_ids":[7],
		"current_member_site_ids":[8],
		"miner_scope_unbounded":false,
		"facility_fan_site_ids":[9],
		"facility_fan_scope_unbounded":false
	}`

	envelope, err := AuthorizationEnvelopeFromJSON([]byte(valid))
	require.NoError(t, err)
	assert.Equal(t, models.AuthorizationEnvelope{
		SchemaVersion:           1,
		SelectedResourceSiteIDs: []int64{7},
		CurrentMemberSiteIDs:    []int64{8},
		FacilityFanSiteIDs:      []int64{9},
	}, envelope)
}

func TestAuthorizationEnvelopeFromJSONRejectsInvalidCoverage(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"missing envelope":       ``,
		"null envelope":          `null`,
		"unknown field":          `{"schema_version":1,"selected_resource_site_ids":[7],"current_member_site_ids":[],"miner_scope_unbounded":false,"facility_fan_site_ids":[],"facility_fan_scope_unbounded":false,"extra":true}`,
		"missing field":          `{"schema_version":1,"selected_resource_site_ids":[7],"current_member_site_ids":[],"miner_scope_unbounded":false,"facility_fan_site_ids":[]}`,
		"unsupported version":    `{"schema_version":2,"selected_resource_site_ids":[7],"current_member_site_ids":[],"miner_scope_unbounded":false,"facility_fan_site_ids":[],"facility_fan_scope_unbounded":false}`,
		"null site IDs":          `{"schema_version":1,"selected_resource_site_ids":null,"current_member_site_ids":[],"miner_scope_unbounded":false,"facility_fan_site_ids":[],"facility_fan_scope_unbounded":false}`,
		"non-positive site ID":   `{"schema_version":1,"selected_resource_site_ids":[0],"current_member_site_ids":[],"miner_scope_unbounded":false,"facility_fan_site_ids":[],"facility_fan_scope_unbounded":false}`,
		"missing miner coverage": `{"schema_version":1,"selected_resource_site_ids":[],"current_member_site_ids":[],"miner_scope_unbounded":false,"facility_fan_site_ids":[],"facility_fan_scope_unbounded":false}`,
		"invalid boolean":        `{"schema_version":1,"selected_resource_site_ids":[],"current_member_site_ids":[],"miner_scope_unbounded":"true","facility_fan_site_ids":[],"facility_fan_scope_unbounded":false}`,
	}

	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := AuthorizationEnvelopeFromJSON([]byte(raw))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "curtailment authorization envelope")
		})
	}
}
