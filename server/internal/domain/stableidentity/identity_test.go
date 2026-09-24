package stableidentity

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIdentityMatches(t *testing.T) {
	tests := []struct {
		name      string
		candidate Identity
		paired    Identity
		want      bool
	}{
		{name: "normalized MAC", candidate: New("", "aa-bb-cc-dd-ee-ff"), paired: New("", "AA:BB:CC:DD:EE:FF"), want: true},
		{name: "trimmed serial", candidate: New(" SN-1 ", ""), paired: New("SN-1", ""), want: true},
		{name: "invalid MACs are not evidence", candidate: New("", "invalid-a"), paired: New("", "invalid-b"), want: false},
		{name: "one conflicting identifier rejects the candidate", candidate: New("SN-1", "AA:BB:CC:DD:EE:FF"), paired: New("SN-2", "AA:BB:CC:DD:EE:FF"), want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.candidate.Matches(tc.paired))
		})
	}
}

func TestIdentityUsable(t *testing.T) {
	require.False(t, New("", "").Usable())
	require.False(t, New("", "invalid").Usable())
	require.True(t, New("serial", "").Usable())
	require.True(t, New("", "AA:BB:CC:DD:EE:FF").Usable())
}
