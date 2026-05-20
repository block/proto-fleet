package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCountNonTerminalTargets_TreatsUnknownStatesAsNonTerminal(t *testing.T) {
	t.Parallel()

	targets := []*Target{
		{State: TargetStateResolved},
		{State: TargetStateRestoreFailed},
		{State: TargetStateReleased},
		{State: TargetStatePending},
		{State: TargetState("future_state")},
	}

	assert.Equal(t, 2, CountNonTerminalTargets(targets))
}
