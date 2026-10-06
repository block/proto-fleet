package deployment

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootCommandsDoNotRequireSudoTTY(t *testing.T) {
	for _, tc := range []struct {
		name     string
		uid      int
		command  string
		args     []string
		want     string
		wantArgs []string
	}{
		{"root", 0, "sudo", []string{"docker", "ps"}, "docker", []string{"ps"}},
		{"operator", 501, "sudo", []string{"docker", "ps"}, "sudo", []string{"docker", "ps"}},
		{"user switch", 0, "sudo", []string{"-u", "postgres", "id"}, "sudo", []string{"-u", "postgres", "id"}},
		{"plain", 0, "docker", []string{"ps"}, "docker", []string{"ps"}},
		{"empty", 0, "sudo", nil, "sudo", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command, args := withoutRedundantSudo(tc.uid, tc.command, tc.args)
			require.Equal(t, tc.want, command)
			require.Equal(t, tc.wantArgs, args)
		})
	}
}
