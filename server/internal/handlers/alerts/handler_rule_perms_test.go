package alerts

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	alertsv1 "github.com/block/proto-fleet/server/generated/grpc/alerts/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func offlineRuleConfig() *alertsv1.RuleConfig {
	return &alertsv1.RuleConfig{
		Name:            "Offline too long",
		DurationSeconds: 1800,
		TemplateConfig:  &alertsv1.RuleConfig_Offline{Offline: &alertsv1.OfflineConfig{}},
	}
}

func requirePermissionDenied(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var fe fleeterror.FleetError
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, connect.CodePermissionDenied, fe.GRPCCode)
}

// Rule mutations are gated on alert:manage before the service is touched (svc is nil).
func TestRuleMutationsRequireAlertManage(t *testing.T) {
	h := NewHandler(nil, nil)
	readOnly := ctxWithPerms(authz.PermAlertRead)

	_, err := h.CreateRule(readOnly, connect.NewRequest(&alertsv1.CreateRuleRequest{Config: offlineRuleConfig()}))
	requirePermissionDenied(t, err)

	_, err = h.UpdateRule(readOnly, connect.NewRequest(&alertsv1.UpdateRuleRequest{Id: "pfu-1", Config: offlineRuleConfig()}))
	requirePermissionDenied(t, err)

	_, err = h.DeleteRule(readOnly, connect.NewRequest(&alertsv1.DeleteRuleRequest{Id: "pfu-1"}))
	requirePermissionDenied(t, err)
}

// A missing or template-less config is rejected in the handler, before the service is touched.
func TestRuleConfigMappingRejectsMissingTemplate(t *testing.T) {
	h := NewHandler(nil, nil)
	manage := ctxWithPerms(authz.PermAlertManage)

	_, err := h.CreateRule(manage, connect.NewRequest(&alertsv1.CreateRuleRequest{}))
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))

	_, err = h.CreateRule(manage, connect.NewRequest(&alertsv1.CreateRuleRequest{
		Config: &alertsv1.RuleConfig{Name: "r", DurationSeconds: 600},
	}))
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))

	_, err = h.CreateRule(manage, connect.NewRequest(&alertsv1.CreateRuleRequest{
		Config: &alertsv1.RuleConfig{
			Name:            "r",
			DurationSeconds: 600,
			TemplateConfig:  &alertsv1.RuleConfig_Hashrate{Hashrate: &alertsv1.HashrateConfig{Value: 50}},
		},
	}))
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}
