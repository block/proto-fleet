package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"
)

func TestFleetdBareInvocationSelectsServerCommand(t *testing.T) {
	configPath := writeFleetdConfigFile(t, `
auth:
  client:
    expiration-period: "1h"
    secret-key: "test-client-secret"
encrypt:
  service-master-key: "test-master-key"
http:
  address: "0.0.0.0:9090"
`)
	cli := &fleetdCLI{}
	parser, err := kong.New(
		cli,
		kong.Name("fleetd"),
		kong.Configuration(fleetdYAMLLoader, configPath),
	)
	require.NoError(t, err)

	ctx, err := parser.Parse(nil)

	require.NoError(t, err)
	require.Equal(t, "server", ctx.Command())
	require.Equal(t, "0.0.0.0:9090", cli.Server.HTTP.Address)
}

func TestFleetdAdminResetPasswordParsesRootConfigAndStdinFlag(t *testing.T) {
	configPath := writeFleetdConfigFile(t, `
db:
  address: "db.internal:5432"
`)
	cli := &fleetdCLI{}
	parser, err := kong.New(
		cli,
		kong.Name("fleetd"),
		kong.Configuration(fleetdYAMLLoader, configPath),
	)
	require.NoError(t, err)

	ctx, err := parser.Parse([]string{"admin", "reset-password", "--password-stdin"})

	require.NoError(t, err)
	require.Equal(t, "admin reset-password", ctx.Command())
	require.True(t, cli.Admin.ResetPassword.PasswordStdin)
	require.Equal(t, "db.internal:5432", cli.Admin.ResetPassword.DB.Address)
}

func TestNormalizeFleetdArgsPreservesCommandsAndRoutesBareFlags(t *testing.T) {
	require.Nil(t, normalizeFleetdArgs(nil))
	require.Equal(t, []string{"server"}, normalizeFleetdArgs([]string{"server"}))
	require.Equal(t, []string{"admin", "reset-password"}, normalizeFleetdArgs([]string{"admin", "reset-password"}))
	require.Equal(t, []string{"server", "--http-address=127.0.0.1:8081"},
		normalizeFleetdArgs([]string{"--http-address=127.0.0.1:8081"}))
}

func TestResetPasswordCommandReadsOnePasswordLine(t *testing.T) {
	cmd := resetPasswordCommand{PasswordStdin: true}

	password, err := cmd.password(strings.NewReader("supplied-secret\r\nignored"))

	require.NoError(t, err)
	require.Equal(t, "supplied-secret", password)
}

func TestResetPasswordCommandRejectsEmptyStdin(t *testing.T) {
	cmd := resetPasswordCommand{PasswordStdin: true}

	_, err := cmd.password(strings.NewReader("\n"))

	require.ErrorContains(t, err, "must not be empty")
}

func TestResetPasswordCommandRejectsInvalidStdinPassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  string
	}{
		{name: "too short", password: "short\n", wantErr: "at least 8 characters"},
		{name: "too many bytes", password: strings.Repeat("a", 73) + "\n", wantErr: "at most 72 bytes"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := resetPasswordCommand{PasswordStdin: true}

			_, err := cmd.password(strings.NewReader(test.password))

			require.ErrorContains(t, err, test.wantErr)
		})
	}
}

func TestResetPasswordCommandGeneratesPasswordByDefault(t *testing.T) {
	password, err := (&resetPasswordCommand{}).password(strings.NewReader(""))

	require.NoError(t, err)
	require.Len(t, password, 32)
}

func TestResetPasswordCommandDoesNotEchoStdinPassword(t *testing.T) {
	cmd := resetPasswordCommand{PasswordStdin: true}
	var output bytes.Buffer

	err := cmd.writeResult(&output, "owner", "supplied-secret")

	require.NoError(t, err)
	require.Equal(t, "Reset the password for SUPER_ADMIN \"owner\".\n", output.String())
	require.NotContains(t, output.String(), "supplied-secret")
}

func TestResetPasswordCommandPrintsGeneratedPassword(t *testing.T) {
	cmd := resetPasswordCommand{}
	var output bytes.Buffer

	err := cmd.writeResult(&output, "owner", "generated-secret")

	require.NoError(t, err)
	require.Contains(t, output.String(), "Temporary password: generated-secret")
}
