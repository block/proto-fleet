package commandtype

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStringFromStringRoundtrip(t *testing.T) {
	t.Parallel()
	all := []Type{
		StartMining,
		StopMining,
		SetCoolingMode,
		SetPowerTarget,
		UpdateMiningPools,
		DownloadLogs,
		Reboot,
		BlinkLED,
		FirmwareUpdate,
		Unpair,
		UpdateMinerPassword,
		Curtail,
		Uncurtail,
	}
	for _, want := range all {
		name := want.String()
		assert.NotEqual(t, "Undefined", name, "Type %d must have a String() representation", want)
		got, err := FromString(name)
		require.NoError(t, err, "FromString(%q) must round-trip", name)
		assert.Equal(t, want, got, "round-trip mismatch for %s", name)
	}
}

func TestCurtailAndUncurtailHaveStableLabels(t *testing.T) {
	t.Parallel()
	curtail := Curtail
	uncurtail := Uncurtail
	assert.Equal(t, "Curtail", curtail.String())
	assert.Equal(t, "Uncurtail", uncurtail.String())
}

func TestFromStringRejectsUnknown(t *testing.T) {
	t.Parallel()
	_, err := FromString("NotAValidType")
	require.Error(t, err)
}
