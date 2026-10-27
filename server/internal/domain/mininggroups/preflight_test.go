package mininggroups

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAssignmentPreflightChecksEveryStep(t *testing.T) {
	for _, test := range []struct {
		name      string
		snapshot  Snapshot
		desired   map[string]string
		wantError string
	}{
		{name: "whole custom group in place", snapshot: Snapshot{Enabled: true, MaxGroups: 2, Groups: []Group{{ID: "owned", Managed: true, CustomPolicy: true, Members: []string{"a", "b"}}}}, desired: map[string]string{"a": "new", "b": "new"}},
		{name: "split ordinary group", snapshot: Snapshot{Enabled: true, MaxGroups: 2, Groups: []Group{{ID: "default", Members: []string{"a", "b"}}}}, desired: map[string]string{"a": "new"}},
		{name: "reuse empty group at capacity", snapshot: Snapshot{Enabled: true, MaxGroups: 2, Groups: []Group{{ID: "default", Members: []string{"a"}}, {ID: "empty", Managed: true, ConfigRevision: "unused"}}}, desired: map[string]string{"a": "new"}},
		{name: "policy after reuse and configure", snapshot: Snapshot{Enabled: true, MaxGroups: 2, Groups: []Group{{ID: "default", CustomPolicy: true, Members: []string{"a"}}, {ID: "empty", Managed: true, ConfigRevision: "unused"}}}, desired: map[string]string{"a": "new"}, wantError: "operator policy"},
		{name: "policy on a later source", snapshot: Snapshot{Enabled: true, MaxGroups: 3, Groups: []Group{{ID: "first", Members: []string{"a"}}, {ID: "last", CustomPolicy: true, Members: []string{"z"}}, {ID: "target", Managed: true, Configured: true, Mining: true, ConfigRevision: "new"}}}, desired: map[string]string{"a": "new", "z": "new"}, wantError: "operator policy"},
		{name: "capacity for a later profile", snapshot: Snapshot{Enabled: true, MaxGroups: 2, Groups: []Group{{ID: "default", Members: []string{"a", "b"}}}}, desired: map[string]string{"a": "alice", "b": "bob"}, wantError: "capacity"},
		{name: "pending controller transfer", snapshot: Snapshot{Enabled: true, MaxGroups: 2, Pending: true}, desired: map[string]string{"a": "new"}, wantError: "pending transfer"},
	} {
		t.Run(test.name, func(t *testing.T) {
			before, err := encode(test.snapshot)
			require.NoError(t, err)
			err = ValidateAssignment(test.desired, test.snapshot)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
			} else {
				require.NoError(t, err)
			}
			after, err := encode(test.snapshot)
			require.NoError(t, err)
			require.Equal(t, before, after, "preflight must not change observed membership or settings")
		})
	}
}

func TestAssignmentPreflightSupportsFullController(t *testing.T) {
	desired := map[string]string{}
	members := []string{}
	for i := range 288 {
		mac := fmt.Sprintf("board-%03d", i)
		members = append(members, mac)
		desired[mac] = fmt.Sprintf("profile-%03d", i)
	}
	snapshot := Snapshot{Enabled: true, MaxGroups: 289, Groups: []Group{{ID: "default", Members: members}}}
	require.NoError(t, ValidateAssignment(desired, snapshot))
}
