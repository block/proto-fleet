package driver

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubController struct {
	validateErr error
}

func (s stubController) ValidateConfig(json.RawMessage) error                 { return s.validateErr }
func (s stubController) SetState(context.Context, Device, DesiredState) error { return nil }
func (s stubController) Capabilities() map[string]bool                        { return map[string]bool{"on_off": true} }

func TestRegistry_ResolvesRegisteredDriver(t *testing.T) {
	r := NewRegistry()
	r.Register("stub", func() Controller { return stubController{} })

	c, err := r.Controller("stub")
	require.NoError(t, err)
	assert.NotNil(t, c)
	assert.Equal(t, []string{"stub"}, r.DriverTypes())
}

func TestRegistry_UnknownDriverTypeFails(t *testing.T) {
	r := NewRegistry()
	r.Register("stub", func() Controller { return stubController{} })

	_, err := r.Controller("bacnet")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown infrastructure driver type "bacnet"`)
	assert.Contains(t, err.Error(), "stub", "error should list supported types")

	err = r.ValidateConfig("bacnet", nil)
	require.Error(t, err)
}

func TestRegistry_ValidateConfigDelegatesToAdapter(t *testing.T) {
	r := NewRegistry()
	r.Register("ok", func() Controller { return stubController{} })
	r.Register("bad", func() Controller { return stubController{validateErr: assert.AnError} })

	assert.NoError(t, r.ValidateConfig("ok", nil))
	assert.ErrorIs(t, r.ValidateConfig("bad", nil), assert.AnError)
}

func TestRegistry_DuplicateRegistrationPanics(t *testing.T) {
	r := NewRegistry()
	r.Register("stub", func() Controller { return stubController{} })
	assert.Panics(t, func() {
		r.Register("stub", func() Controller { return stubController{} })
	})
}
