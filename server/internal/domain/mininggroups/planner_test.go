package mininggroups

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestPlannerReadsDefaultOnlyForInheritedBoards(t *testing.T) {
	miner := &defaultReturnMiner{simulatedGroups: &simulatedGroups{}, defaultReadErr: control.ErrCommandProtocolUpgradeRequired}
	service := &Service{miners: miner}
	observed := sdk.MiningGroupResult{Enabled: true, MaxGroups: 4, Groups: []sdk.MiningGroup{
		{ID: "default", Revision: 1, MACs: []string{"default-board"}},
		{ID: "other", Revision: 2, MACs: []string{"other-board"}},
	}}
	bindings, err := encode(map[string]binding{"default": {ConfigID: "inherited", Configured: true, Inherited: true}})
	require.NoError(t, err)
	row := sqlc.ContainerMiningGroupState{Bindings: bindings}
	for _, mac := range []string{"other-board", "default-board"} {
		desired := map[string]string{mac: "new-config"}
		snapshot, err := service.plannerSnapshot(t.Context(), nil, row, observed, desired)
		require.NoError(t, err)
		step, err := Plan(desired, snapshot)
		require.NoError(t, err)
		require.Equal(t, Create, step.Kind)
	}
	require.Empty(t, miner.calls)
	_, err = service.plannerSnapshot(t.Context(), nil, row, observed, map[string]string{"default-board": "inherited"})
	require.ErrorIs(t, err, errDefaultSettingsUnavailable)
	require.Equal(t, "Upgrade the Fleet Node and reconnect it before changing Default mining settings.", defaultReadBlockedMessage(err))
	require.Equal(t, []sdk.MiningGroupRequest{{Action: sdk.MiningGroupReadDefault}}, miner.calls)
}

func TestPlannerChangesWholeGroupInPlace(t *testing.T) {
	observed := Snapshot{Enabled: true, MaxGroups: 289, Groups: []Group{{ID: "g", Revision: 4, ConfigRevision: "old", Managed: true, Members: []string{"a", "b"}, Configured: true, Mining: true}}}
	step, err := Plan(map[string]string{"a": "new", "b": "new"}, observed)
	require.NoError(t, err)
	require.Equal(t, Configure, step.Kind)
	require.Equal(t, "g", step.GroupID)
	require.Equal(t, "new", step.ConfigRevision)
	require.EqualValues(t, 4, step.ExpectedRevision)
}
func TestPlannerSplitsOnlySelectedBoards(t *testing.T) {
	observed := Snapshot{Enabled: true, MaxGroups: 289, Groups: []Group{{ID: "g", Revision: 4, ConfigRevision: "old", Managed: true, Members: []string{"a", "b"}, Configured: true, Mining: true}, {ID: "dest", Revision: 2, ConfigRevision: "new", Managed: true, Configured: true, Mining: true}}}
	step, err := Plan(map[string]string{"a": "new", "b": "old"}, observed)
	require.NoError(t, err)
	require.Equal(t, Transfer, step.Kind)
	require.Equal(t, []string{"a"}, step.MACs)
	require.Equal(t, "g", step.SourceID)
	require.Equal(t, "dest", step.GroupID)
}
func TestPlannerDoesNotRepurposeUnmanagedOrPartiallySelectedGroups(t *testing.T) {
	for _, managed := range []bool{false, true} {
		observed := Snapshot{Enabled: true, MaxGroups: 289, Groups: []Group{{ID: "g", Revision: 4, Managed: managed, Members: []string{"a", "b"}, Configured: true, Mining: true}}}
		step, err := Plan(map[string]string{"a": "new"}, observed)
		require.NoError(t, err)
		require.Equal(t, Create, step.Kind)
	}
}
func TestPlannerStartsConfiguredGroupBeforeTransfer(t *testing.T) {
	observed := Snapshot{Enabled: true, MaxGroups: 289, Groups: []Group{{ID: "g", Revision: 4, ConfigRevision: "new", Managed: true, Configured: true}}}
	step, err := Plan(map[string]string{"a": "new"}, observed)
	require.NoError(t, err)
	require.Equal(t, Start, step.Kind)
}
func TestPlannerHoldsDuringTransferAndRejectsAmbiguousMembership(t *testing.T) {
	observed := Snapshot{Enabled: true, MaxGroups: 289, Pending: true}
	step, err := Plan(map[string]string{"a": "new"}, observed)
	require.NoError(t, err)
	require.Equal(t, Wait, step.Kind)
	observed.Pending = false
	observed.Groups = []Group{{ID: "g", Members: []string{"a"}}, {ID: "h", Members: []string{"a"}}}
	_, err = Plan(map[string]string{"a": "new"}, observed)
	require.Error(t, err)
}
func TestPlannerDoesNotDuplicateCustomPowerCap(t *testing.T) {
	observed := Snapshot{Enabled: true, MaxGroups: 289, Groups: []Group{{ID: "g", Managed: true, Members: []string{"a", "b"}, CustomPolicy: true}}}
	_, err := Plan(map[string]string{"a": "new"}, observed)
	require.ErrorContains(t, err, "operator policy")
}
func TestPlannerRespectsCapacityAndDoesNotMergeConfigurationIdentities(t *testing.T) {
	observed := Snapshot{Enabled: true, MaxGroups: 2, Groups: []Group{{ID: "default", Members: []string{"a"}}, {ID: "g", Managed: true, ConfigRevision: "alice", Configured: true, Mining: true, Members: []string{"b"}}}}
	_, err := Plan(map[string]string{"a": "bob", "b": "alice"}, observed)
	require.ErrorContains(t, err, "capacity")
}

func TestPlannerReusesEmptyOwnedGroupAtCapacity(t *testing.T) {
	observed := Snapshot{Enabled: true, MaxGroups: 2, Groups: []Group{{ID: "default", Members: []string{"a"}}, {ID: "reusable", Managed: true, ConfigRevision: "unused", Revision: 4}}}
	step, err := Plan(map[string]string{"a": "new"}, observed)
	require.NoError(t, err)
	require.Equal(t, Configure, step.Kind)
	require.Equal(t, "reusable", step.GroupID)
}

func TestPlannerRemovesOnlyUnusedEmptyManagedGroups(t *testing.T) {
	snapshot := Snapshot{Enabled: true, MaxGroups: 289, Groups: []Group{{ID: "default"}, {ID: "foreign"}, {ID: "owned", Managed: true, ConfigRevision: "obsolete", Revision: 7}, {ID: "policy", Managed: true, CustomPolicy: true}}}
	step, err := Plan(map[string]string{}, snapshot)
	require.NoError(t, err)
	require.Equal(t, Remove, step.Kind)
	require.Equal(t, "owned", step.GroupID)
}

func TestPlannerClearedGroupsStayStoppedAndAreReused(t *testing.T) {
	cleared := "pool-free:1"
	snapshot := Snapshot{Enabled: true, MaxGroups: 3, Groups: []Group{
		{ID: "first", Revision: 2, Managed: true, Configured: true, Mining: true, ConfigRevision: "config-1", Members: []string{"a", "b"}},
		{ID: "second", Revision: 2, Managed: true, Configured: true, Mining: true, ConfigRevision: "config-2", Members: []string{"c", "d"}},
	}}
	desired := map[string]string{"a": "config-1", "b": cleared, "c": "config-2", "d": "config-2"}
	require.NoError(t, ValidateAssignment(desired, snapshot))
	step, err := Plan(desired, snapshot)
	require.NoError(t, err)
	require.Equal(t, Create, step.Kind)
	snapshot.MaxGroups = 2
	require.ErrorContains(t, ValidateAssignment(desired, snapshot), "capacity")
	snapshot.MaxGroups = 3
	snapshot.Groups = append(snapshot.Groups, Group{ID: "empty", Revision: 1, Managed: true, Configured: true, Mining: true, ConfigRevision: cleared})
	step, err = Plan(desired, snapshot)
	require.NoError(t, err)
	require.Equal(t, Stop, step.Kind)
	snapshot.Groups[2].Mining = false
	step, err = Plan(desired, snapshot)
	require.NoError(t, err)
	require.Equal(t, Transfer, step.Kind)
	require.Equal(t, []string{"b"}, step.MACs)
	require.Equal(t, "empty", step.GroupID)
	snapshot.Groups[0].Members = []string{"a"}
	snapshot.Groups[2].Members = []string{"b"}
	step, err = Plan(desired, snapshot)
	require.NoError(t, err)
	require.Equal(t, Done, step.Kind)
	desired["a"] = cleared
	step, err = Plan(desired, snapshot)
	require.NoError(t, err)
	require.Equal(t, Transfer, step.Kind)
	require.Equal(t, "empty", step.GroupID, "reuse pool-free group in same account")
	desired["a"] = "pool-free:2"
	step, err = Plan(desired, snapshot)
	require.NoError(t, err)
	require.Equal(t, Configure, step.Kind)
	require.Equal(t, "first", step.GroupID, "keep independent accounts in separate pool-free groups")
}

func TestPlannerPreservesInheritedDefaultWhileChangingAnotherBoard(t *testing.T) {
	snapshot := Snapshot{Enabled: true, MaxGroups: 4, Groups: []Group{
		{ID: "default", ConfigRevision: "inherited", Managed: true, Inherited: true, Configured: true, Members: []string{"a", "b"}},
		{ID: "updated", ConfigRevision: "new", Managed: true, Configured: true, Mining: true},
	}}
	step, err := Plan(map[string]string{"a": "inherited", "b": "new"}, snapshot)
	require.NoError(t, err)
	require.Equal(t, Transfer, step.Kind)
	require.Equal(t, []string{"b"}, step.MACs)
	require.Equal(t, "default", step.SourceID)
	require.Equal(t, "updated", step.GroupID)
	snapshot.Groups[0].Members = []string{"a"}
	snapshot.Groups[1].Members = []string{"b"}
	step, err = Plan(map[string]string{"a": "inherited", "b": "new"}, snapshot)
	require.NoError(t, err)
	require.Equal(t, Done, step.Kind, "do not start or move the stopped default group")
}
