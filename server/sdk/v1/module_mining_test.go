package sdk

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModuleMiningTargetValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		target ModuleMiningTarget
		valid  bool
	}{
		{"valid", ModuleMiningTarget{Tank: 1, Module: 2, ReportedID: "a"}, true},
		{"maximum identity length", ModuleMiningTarget{Tank: 1, Module: 2, ReportedID: strings.Repeat("a", 255)}, true},
		{"missing tank", ModuleMiningTarget{Module: 2, ReportedID: "a"}, false},
		{"negative module", ModuleMiningTarget{Tank: 1, Module: -2, ReportedID: "a"}, false},
		{"blank identity", ModuleMiningTarget{Tank: 1, Module: 2, ReportedID: " \t"}, false},
		{"oversized identity", ModuleMiningTarget{Tank: 1, Module: 2, ReportedID: strings.Repeat("a", 256)}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.valid, test.target.Validate() == nil)
			require.Equal(t, test.valid, (ModuleMiningRequest{Mining: new(false), Modules: []ModuleMiningTarget{test.target}}).Validate() == nil)
		})
	}
}

func TestModuleMiningRequestRejectsDuplicateCoordinatesAndIdentities(t *testing.T) {
	for _, second := range []ModuleMiningTarget{
		{Tank: 1, Module: 2, ReportedID: "b"},
		{Tank: 3, Module: 4, ReportedID: "a"},
	} {
		require.NoError(t, second.Validate())
		require.Error(t, (ModuleMiningRequest{
			Mining:  new(false),
			Modules: []ModuleMiningTarget{{Tank: 1, Module: 2, ReportedID: "a"}, second},
		}).Validate())
	}
}
