package curtailment

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// FULL_FLEET curtails every eligible miner regardless of target_kw and persists
// the event with mode=FULL_FLEET in the normal PENDING lifecycle.
func TestService_Start_FullFleet_CurtailsAllEligible(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("a", 6000, 100, 40),
		minerWithEff("b", 5000, 100, 45),
	}
	svc := NewService(store)
	req := validStartRequest(orgID)
	req.Scope = Scope{Type: models.ScopeTypeWholeOrg}
	req.Mode = models.ModeFullFleet
	req.TargetKW = 0 // ignored by full_fleet

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err)
	assert.Len(t, plan.Selected, 2, "full_fleet curtails every eligible miner")
	assert.Equal(t, models.ModeFullFleet, store.lastInsertEvent.Mode)
	assert.Equal(t, models.EventStatePending, store.lastInsertEvent.State)
	assert.Len(t, store.lastInsertTargets, 2)
}

// The empty-eligible case is the chosen behavior: persist a vacuously COMPLETED
// event with no targets, not an insufficient-load rejection.
func TestService_Start_FullFleet_NoEligibleMinersPersistsCompleted(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	// candidatesByOrg[orgID] left unset: nothing eligible.
	svc := NewService(store)
	req := validStartRequest(orgID)
	req.Scope = Scope{Type: models.ScopeTypeWholeOrg}
	req.Mode = models.ModeFullFleet
	req.TargetKW = 0

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err, "empty full_fleet is valid, not an insufficient-load rejection")
	assert.Empty(t, plan.Selected)
	assert.Equal(t, models.ModeFullFleet, store.lastInsertEvent.Mode)
	assert.Equal(t, models.EventStateCompleted, store.lastInsertEvent.State,
		"nothing eligible == vacuously complete on arrival")
	assert.Empty(t, store.lastInsertTargets, "a completed-empty event has no targets")
}

// FIXED_KW still requires a positive target_kw; FULL_FLEET does not.
func TestService_Start_FullFleet_IgnoresTargetKwValidation(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{minerWithEff("a", 6000, 100, 40)}
	svc := NewService(store)

	fixed := validStartRequest(orgID)
	fixed.Scope = Scope{Type: models.ScopeTypeWholeOrg}
	fixed.Mode = models.ModeFixedKw
	fixed.TargetKW = 0
	_, err := svc.Start(t.Context(), fixed)
	require.Error(t, err, "FIXED_KW with target_kw=0 is rejected")
	assert.True(t, fleeterror.IsInvalidArgumentError(err))

	full := validStartRequest(orgID)
	full.Scope = Scope{Type: models.ScopeTypeWholeOrg}
	full.Mode = models.ModeFullFleet
	full.TargetKW = 0
	_, err = svc.Start(t.Context(), full)
	require.NoError(t, err, "FULL_FLEET ignores target_kw")
}
