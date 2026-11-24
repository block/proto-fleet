package control

import (
	"testing"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryConfigFollowsConnection(t *testing.T) {
	// Arrange
	r := NewRegistry()
	old := r.Register(7)
	old.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.1"}})
	require.Equal(t, []string{"10.0.0.1"}, r.DiscoveryConfig(7).GetRanges())

	// Act
	current := r.Register(7)

	// Assert
	require.Nil(t, r.DiscoveryConfig(7))

	t.Run("evicted connection cannot replace current configuration", func(t *testing.T) {
		// Act
		old.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.2"}})

		// Assert
		require.Nil(t, r.DiscoveryConfig(7))
	})

	t.Run("snapshots are copies", func(t *testing.T) {
		// Arrange
		current.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.3"}})
		snapshot := r.DiscoveryConfig(7)

		// Act
		snapshot.Ranges[0] = "10.0.0.4"

		// Assert
		require.Equal(t, []string{"10.0.0.3"}, r.DiscoveryConfig(7).GetRanges())
	})

	t.Run("unregistering drops the configuration", func(t *testing.T) {
		// Act
		current.Unregister()

		// Assert
		require.Nil(t, r.DiscoveryConfig(7))
	})
}

func TestDiscoveryConfigNilMarksLegacyNode(t *testing.T) {
	// Arrange
	r := NewRegistry()
	stream := r.Register(7)
	stream.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.1"}})

	// Act
	stream.SetDiscoveryConfig(nil)

	// Assert
	require.Nil(t, r.DiscoveryConfig(7))
}

func TestRegisterAuthenticatedPublishesDiscoveryConfigWithConnection(t *testing.T) {
	tests := []struct {
		name   string
		config *gatewaypb.DiscoveryConfig
		want   *gatewaypb.DiscoveryConfig
	}{
		{name: "ranges", config: &gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.1"}}, want: &gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.1"}}},
		{name: "empty config is reported", config: &gatewaypb.DiscoveryConfig{}, want: &gatewaypb.DiscoveryConfig{}},
		{name: "missing config is legacy", config: nil, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			r := NewRegistry()

			// Act
			stream, err := r.RegisterAuthenticated(7, "", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V7, WithDiscoveryConfig(tt.config))

			// Assert
			require.NoError(t, err)
			require.NotNil(t, stream)
			got := r.DiscoveryConfig(7)
			if tt.want == nil {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.Equal(t, tt.want.GetRanges(), got.GetRanges())
		})
	}
}

func TestWithDiscoveryConfigCopiesInput(t *testing.T) {
	// Arrange
	r := NewRegistry()
	config := &gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.1"}}
	_, err := r.RegisterAuthenticated(7, "", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V7, WithDiscoveryConfig(config))
	require.NoError(t, err)

	// Act
	config.Ranges[0] = "10.0.0.9"

	// Assert
	require.Equal(t, []string{"10.0.0.1"}, r.DiscoveryConfig(7).GetRanges())
}
