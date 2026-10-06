package fleetinventory

import (
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestProjectModuleIssues(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name         string
		source, seen time.Time
		complete     bool
		stale        bool
	}{
		{"fresh", now, now, true, false},
		{"stale source", now.Add(-2 * time.Minute), now, true, true},
		{"controller missing", now, now.Add(-3 * time.Minute), true, true},
		{"future", now.Add(time.Minute), now, true, true},
		{"missing", time.Time{}, now, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := projectModuleIssues(&sdk.ContainerModuleIssues{ObservedAt: tc.source, Complete: tc.complete, Items: []sdk.ContainerModuleIssue{{Component: "psu", Cause: "fault", Severity: "fault", ObservedAt: tc.source}}}, &tc.seen, now)
			require.Equal(t, tc.stale, got.Stale)
			if tc.stale || tc.source.IsZero() {
				require.Empty(t, got.Items)
				require.False(t, got.Complete)
			} else {
				require.Len(t, got.Items, 1)
			}
		})
	}
	require.Nil(t, projectModuleIssues(nil, &now, now))
}
