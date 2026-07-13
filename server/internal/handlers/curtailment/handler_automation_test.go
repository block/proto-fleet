package curtailment

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	domainCurtailment "github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

func TestHandler_ListCurtailmentAutomationRulesFiltersCompositeProfileSites(t *testing.T) {
	t.Parallel()

	store := newHandlerAutomationStore(
		handlerAutomationRule(101, "Whole org", nil),
		handlerAutomationRule(102, "Visible site", []byte(`{"site_ids":[8]}`)),
		handlerAutomationRule(103, "Hidden multi-site", []byte(`{"site_ids":[7,8]}`)),
	)
	h := NewHandlerWithAutomation(nil, nil, newHandlerAutomationService(t, store), nil)

	resp, err := h.ListCurtailmentAutomationRules(
		testSessionCtxWithAssignments(t, &session.Info{
			AuthMethod:     session.AuthMethodSession,
			OrganizationID: 42,
			Role:           "OPERATOR",
			SessionID:      "sess-automation-list",
		}, testOrgAssignment(authz.PermCurtailmentManage), testSiteAssignment(7)),
		connect.NewRequest(&pb.ListCurtailmentAutomationRulesRequest{}),
	)

	require.NoError(t, err)
	rules := resp.Msg.GetRules()
	require.Len(t, rules, 1)
	assert.Equal(t, int64(102), rules[0].GetRuleId())
}

func TestHandler_GetCurtailmentAutomationRuleChecksCompositeProfileSites(t *testing.T) {
	t.Parallel()

	store := newHandlerAutomationStore(handlerAutomationRule(101, "Hidden multi-site", []byte(`{"site_ids":[7,8]}`)))
	h := NewHandlerWithAutomation(nil, nil, newHandlerAutomationService(t, store), nil)

	_, err := h.GetCurtailmentAutomationRule(
		testSessionCtxWithAssignments(t, &session.Info{
			AuthMethod:     session.AuthMethodSession,
			OrganizationID: 42,
			Role:           "OPERATOR",
			SessionID:      "sess-automation-get",
		}, testOrgAssignment(authz.PermCurtailmentManage), testSiteAssignment(7)),
		connect.NewRequest(&pb.GetCurtailmentAutomationRuleRequest{RuleId: 101}),
	)

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

func newHandlerAutomationService(t *testing.T, store *handlerAutomationStore) *domainCurtailment.AutomationService {
	t.Helper()

	automation, err := domainCurtailment.NewAutomationService(domainCurtailment.AutomationServiceConfig{
		Store:       store,
		Profiles:    domainCurtailment.NewResponseProfileService(newHandlerResponseProfileStore()),
		SourceStore: &handlerMqttSettingsStore{},
		Curtailment: domainCurtailment.NewService(nil),
		Clock:       func() time.Time { return time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC) },
	})
	require.NoError(t, err)
	return automation
}

func handlerAutomationRule(id int64, name string, scopeJSON []byte) *models.AutomationRule {
	return &models.AutomationRule{
		ID:                       id,
		OrgID:                    42,
		RuleName:                 name,
		TriggerType:              models.AutomationTriggerTypeMQTT,
		MQTTSourceID:             501,
		MQTTSourceName:           "MaestroOS",
		ResponseProfileID:        601,
		ResponseProfileName:      name + " profile",
		ResponseProfileScopeJSON: scopeJSON,
		Enabled:                  true,
		CreatedAt:                time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC),
		UpdatedAt:                time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC),
	}
}

type handlerAutomationStore struct {
	rules []*models.AutomationRule
}

func newHandlerAutomationStore(rules ...*models.AutomationRule) *handlerAutomationStore {
	return &handlerAutomationStore{rules: rules}
}

func (s *handlerAutomationStore) ListAutomationRules(_ context.Context, orgID int64) ([]*models.AutomationRule, error) {
	out := make([]*models.AutomationRule, 0, len(s.rules))
	for _, rule := range s.rules {
		if rule.OrgID == orgID {
			out = append(out, rule)
		}
	}
	return out, nil
}

func (s *handlerAutomationStore) GetAutomationRule(_ context.Context, orgID, ruleID int64) (*models.AutomationRule, error) {
	for _, rule := range s.rules {
		if rule.OrgID == orgID && rule.ID == ruleID {
			return rule, nil
		}
	}
	return nil, fleeterror.NewNotFoundErrorf("curtailment automation rule not found: %d", ruleID)
}

func (*handlerAutomationStore) ListEnabledAutomationRulesByMQTTSource(context.Context, int64) ([]*models.AutomationRule, error) {
	panic("not used")
}

func (*handlerAutomationStore) CreateAutomationRule(context.Context, models.AutomationRule) (*models.AutomationRule, error) {
	panic("not used")
}

func (*handlerAutomationStore) UpdateAutomationRule(context.Context, models.AutomationRule) (*models.AutomationRule, error) {
	panic("not used")
}

func (*handlerAutomationStore) SetAutomationRuleEnabled(context.Context, int64, int64, bool) (*models.AutomationRule, error) {
	panic("not used")
}

func (*handlerAutomationStore) DeleteAutomationRule(context.Context, int64, int64) error {
	panic("not used")
}

func (*handlerAutomationStore) CountAutomationRulesByMQTTSource(context.Context, int64, int64) (int64, error) {
	panic("not used")
}

func (*handlerAutomationStore) RecordAutomationSignal(context.Context, int64, models.AutomationSignal, time.Time) error {
	panic("not used")
}

func (*handlerAutomationStore) SetAutomationActiveEvent(context.Context, int64, int64, uuid.UUID, time.Time) error {
	panic("not used")
}

func (*handlerAutomationStore) ClearAutomationActiveEvent(context.Context, int64, time.Time) error {
	panic("not used")
}

func (*handlerAutomationStore) RecordAutomationRestoreStarted(context.Context, int64, time.Time) error {
	panic("not used")
}

func (*handlerAutomationStore) RecordAutomationExecutionError(context.Context, int64, string, time.Time) error {
	panic("not used")
}
