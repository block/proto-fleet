package reporting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAttributedMeterEnergyPreservesExactShareAndMissingEvidence(t *testing.T) {
	for _, tc := range []struct {
		quality, reading, share, want string
	}{
		{"measured", "10.0000000000000000002", "0.25", "2.50000000000000000005"},
		{"estimated", "12.5", "0.4", "5"},
		{"measured", "12.5", "", "12.5"},
		{"missing", "", "0.25", ""},
		{"reset", "", "0.25", ""},
	} {
		value, err := AttributedMeterEnergy(MeterInterval{Quality: tc.quality, KWh: tc.reading, AllocationFraction: tc.share})
		require.NoError(t, err)
		require.Equal(t, tc.want, value)
	}
}
