package alerts

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	alertsv1 "github.com/block/proto-fleet/server/generated/grpc/alerts/v1"
	"github.com/block/proto-fleet/server/internal/domain/alerts"
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

	_, err = h.SetRuleRouting(readOnly, connect.NewRequest(&alertsv1.SetRuleRoutingRequest{
		RuleId:  "pfu-1",
		Routing: &alertsv1.RuleRouting{Mode: alertsv1.RoutingMode_ROUTING_MODE_NONE},
	}))
	requirePermissionDenied(t, err)
}

// Rule create/update/routing additionally require org-wide miner:read (like
// channel mutations): they decide where per-device alerts fan out.
func TestRuleWritesRequireMinerRead(t *testing.T) {
	h := NewHandler(nil, nil)
	manageOnly := ctxWithPerms(authz.PermAlertManage)

	_, err := h.CreateRule(manageOnly, connect.NewRequest(&alertsv1.CreateRuleRequest{Config: offlineRuleConfig()}))
	requirePermissionDenied(t, err)

	_, err = h.UpdateRule(manageOnly, connect.NewRequest(&alertsv1.UpdateRuleRequest{Id: "pfu-1", Config: offlineRuleConfig()}))
	requirePermissionDenied(t, err)

	_, err = h.SetRuleRouting(manageOnly, connect.NewRequest(&alertsv1.SetRuleRoutingRequest{
		RuleId:  "pfu-1",
		Routing: &alertsv1.RuleRouting{Mode: alertsv1.RoutingMode_ROUTING_MODE_NONE},
	}))
	requirePermissionDenied(t, err)
}

// An unset routing mode is rejected in the handler, before the service is touched.
func TestSetRuleRoutingRejectsUnspecifiedMode(t *testing.T) {
	h := NewHandler(nil, nil)
	manage := ctxWithPerms(authz.PermAlertManage, authz.PermMinerRead)

	_, err := h.SetRuleRouting(manage, connect.NewRequest(&alertsv1.SetRuleRoutingRequest{
		RuleId:  "pfu-1",
		Routing: &alertsv1.RuleRouting{},
	}))
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))

	// Same for CreateRule: absent routing means default, but present-and-unspecified is a client bug.
	_, err = h.CreateRule(manage, connect.NewRequest(&alertsv1.CreateRuleRequest{
		Config:  offlineRuleConfig(),
		Routing: &alertsv1.RuleRouting{},
	}))
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

// A missing or template-less config is rejected in the handler, before the service is touched.
func TestRuleConfigMappingRejectsMissingTemplate(t *testing.T) {
	h := NewHandler(nil, nil)
	manage := ctxWithPerms(authz.PermAlertManage, authz.PermMinerRead)

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

// Unknown routing must be unset on the wire — serializing it as DEFAULT would invite the client to overwrite the real policy.
func TestRuleToProtoOmitsUnknownRouting(t *testing.T) {
	out := ruleToProto(alerts.Rule{ID: "r", RoutingUnknown: true})
	assert.Nil(t, out.Routing)

	out = ruleToProto(alerts.Rule{ID: "r"})
	require.NotNil(t, out.Routing, "a readable rule always carries an explicit mode")
	assert.Equal(t, alertsv1.RoutingMode_ROUTING_MODE_DEFAULT, out.Routing.Mode)
}
