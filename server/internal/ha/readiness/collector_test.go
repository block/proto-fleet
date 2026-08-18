package readiness

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeEmitter struct {
	values []bool
}

func (f *fakeEmitter) EmitHAFailoverReady(_ context.Context, ready bool) {
	f.values = append(f.values, ready)
}

func TestCollectOnceEmitsTheCurrentFailoverReadiness(t *testing.T) {
	for _, test := range []struct {
		name  string
		check Check
		want  bool
	}{
		{name: "ready", check: func(context.Context) (bool, error) { return true, nil }, want: true},
		{name: "not ready", check: func(context.Context) (bool, error) { return false, nil }},
		{name: "check failed", check: func(context.Context) (bool, error) { return false, errors.New("unavailable") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			emitter := &fakeEmitter{}
			collector := New(test.check, emitter)

			collector.collectOnce(t.Context())

			require.Equal(t, []bool{test.want}, emitter.values)
		})
	}
}
