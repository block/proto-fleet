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
		ApplyCurtailmentConfig,
		SetContainerControl,
		SetTankMining,
		SetModuleMining,
		SetBoardIdentify,
		ContainerMining,
		FrigateServerUpdate,
		ContainerAlarmAction,
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

func TestMayAffectPools(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind      Type
		conflicts bool
	}{
		{BlinkLED, false}, {SetBoardIdentify, false}, {DownloadLogs, false},
		{SetCoolingMode, false}, {SetPowerTarget, false}, {FrigateServerUpdate, false},
		{StartMining, true}, {StopMining, true}, {UpdateMiningPools, true},
		{Reboot, true}, {FirmwareUpdate, true}, {Unpair, true},
		{UpdateMinerPassword, true}, {Curtail, true}, {Uncurtail, true},
		{ApplyCurtailmentConfig, true}, {SetContainerControl, true},
		{ContainerAlarmAction, true}, {SetTankMining, true}, {SetModuleMining, true},
		{ContainerMining, true}, {Type(-1), true},
	} {
		t.Run(tc.kind.String(), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.conflicts, tc.kind.MayAffectPools())
		})
	}
}
