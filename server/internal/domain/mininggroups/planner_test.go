package mininggroups

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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
