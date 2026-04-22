package command

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComposeFinalizers_Empty(t *testing.T) {
	assert.Nil(t, composeFinalizers())
	assert.Nil(t, composeFinalizers(nil, nil))
}

func TestComposeFinalizers_SingleCallbackPassedThrough(t *testing.T) {
	calls := 0
	cb := func() error { calls++; return nil }

	fn := composeFinalizers(nil, cb, nil)
	if fn == nil {
		t.Fatalf("expected non-nil finalizer")
	}

	assert.NoError(t, fn())
	assert.Equal(t, 1, calls)
}

func TestComposeFinalizers_RunsInOrderAndStopsOnError(t *testing.T) {
	order := []string{}
	first := func() error { order = append(order, "first"); return nil }
	second := func() error { order = append(order, "second"); return errors.New("boom") }
	third := func() error { order = append(order, "third"); return nil }

	fn := composeFinalizers(first, second, third)
	err := fn()
	assert.EqualError(t, err, "boom")
	assert.Equal(t, []string{"first", "second"}, order, "finalizer after failure must not run")
}

func TestComposeFinalizers_AllSucceed(t *testing.T) {
	order := []string{}
	cb := func(name string) onFinishedCallbackFunc {
		return func() error { order = append(order, name); return nil }
	}

	fn := composeFinalizers(cb("a"), cb("b"), cb("c"))
	assert.NoError(t, fn())
	assert.Equal(t, []string{"a", "b", "c"}, order)
}

func TestComposeFinalizers_RetrySkipsAlreadySucceededCallbacks(t *testing.T) {
	// Simulates the status routine's retry loop: the first run fails at
	// callback 2; the second run must not re-invoke callback 1, because its
	// side effects already landed. Mirrors the DownloadLogs bundle + activity
	// finalizer composition.
	firstCalls := 0
	secondCalls := 0
	secondAttempts := 0

	first := func() error {
		firstCalls++
		return nil
	}
	second := func() error {
		secondCalls++
		secondAttempts++
		if secondAttempts == 1 {
			return errors.New("transient blip")
		}
		return nil
	}

	fn := composeFinalizers(first, second)

	// First invocation: first succeeds, second errors.
	err := fn()
	assert.EqualError(t, err, "transient blip")
	assert.Equal(t, 1, firstCalls)
	assert.Equal(t, 1, secondCalls)

	// Retry: first must be skipped (already done), only second runs again.
	err = fn()
	assert.NoError(t, err)
	assert.Equal(t, 1, firstCalls, "succeeded callbacks must not re-run on retry")
	assert.Equal(t, 2, secondCalls)
}
