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
