package command

import (
	"testing"
	"time"
)

// SetBoardASICStreamTimingForTest shortens the stream lifetime and idle
// timeout for one test.
func SetBoardASICStreamTimingForTest(t *testing.T, lifetime, idle time.Duration) {
	t.Helper()
	previousLifetime, previousIdle := boardASICStreamLifetime, boardASICStreamIdleTimeout
	boardASICStreamLifetime, boardASICStreamIdleTimeout = lifetime, idle
	t.Cleanup(func() { boardASICStreamLifetime, boardASICStreamIdleTimeout = previousLifetime, previousIdle })
}
