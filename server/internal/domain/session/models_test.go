package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestActorConstantsAreDistinct pins the lowercase actor labels so future
// additions don't accidentally collide with existing values (the labels are
// compared by string equality in command preflight filters).
func TestActorConstantsAreDistinct(t *testing.T) {
	t.Parallel()
	actors := map[Actor]string{
		ActorScheduler:    "scheduler",
		ActorCurtailment:  "curtailment",
		ActorMinerChannel: "miner_channel",
	}
	seen := make(map[string]Actor, len(actors))
	for a, want := range actors {
		assert.Equal(t, want, string(a), "actor label mismatch")
		if prior, exists := seen[string(a)]; exists {
			t.Fatalf("duplicate actor label %q (%v vs %v)", a, prior, a)
		}
		seen[string(a)] = a
	}
}
