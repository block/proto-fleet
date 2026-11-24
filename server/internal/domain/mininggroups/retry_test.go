package mininggroups

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReconciliationFailureDelay(t *testing.T) {
	for failures, ceiling := range map[int32]time.Duration{
		0: 5 * time.Second, 1: 10 * time.Second, 2: 20 * time.Second, 3: 40 * time.Second,
		4: 80 * time.Second, 5: 160 * time.Second, 6: 5 * time.Minute, 7: 5 * time.Minute,
	} {
		for range 20 {
			delay := reconciliationFailureDelay(failures)
			require.GreaterOrEqual(t, delay, ceiling*4/5)
			require.LessOrEqual(t, delay, ceiling)
		}
	}
}
