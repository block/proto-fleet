package command

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestStripCompletedSuffix(t *testing.T) {
	cases := map[string]string{
		"reboot":                 "reboot",
		"reboot.completed":       "reboot",
		"update_mining_pools":    "update_mining_pools",
		"":                       "",
		"completed":              "completed",
		"anything.completed.foo": "anything.completed.foo", // only trims trailing suffix
	}
	for in, want := range cases {
		assert.Equal(t, want, stripCompletedSuffix(in), "input=%q", in)
	}
}

func TestNewCompletionReconciler_DefaultsConfig(t *testing.T) {
	cfg := &Config{}
	r := NewCompletionReconciler(nil, cfg, nil)
	assert.NotNil(t, r)
	assert.Equal(t, 5*time.Minute, cfg.ReconcilerInterval, "default interval applied")
	assert.Equal(t, 2*time.Minute, cfg.ReconcilerGracePeriod, "default grace applied")
	assert.Equal(t, 200, cfg.ReconcilerMaxBatches, "default batch cap applied")
}

func TestNewCompletionReconciler_RespectsOverrides(t *testing.T) {
	cfg := &Config{
		ReconcilerInterval:    30 * time.Second,
		ReconcilerGracePeriod: 5 * time.Second,
		ReconcilerMaxBatches:  42,
	}
	NewCompletionReconciler(nil, cfg, nil)
	assert.Equal(t, 30*time.Second, cfg.ReconcilerInterval)
	assert.Equal(t, 5*time.Second, cfg.ReconcilerGracePeriod)
	assert.Equal(t, 42, cfg.ReconcilerMaxBatches)
}
