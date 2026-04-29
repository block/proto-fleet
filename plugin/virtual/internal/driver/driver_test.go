package driver

import (
	"testing"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDescribeDriver_AdvertisesCurtailCapability(t *testing.T) {
	d := &Driver{}

	_, caps, err := d.DescribeDriver(t.Context())

	require.NoError(t, err)
	assert.True(t, caps[sdk.CapabilityCurtailFull])
	assert.False(t, caps[sdk.CapabilityCurtailEfficiency])
	assert.False(t, caps[sdk.CapabilityCurtailPartial])
}
