package main

import (
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

func TestResetPasswordCommandGeneratesPasswordByDefault(t *testing.T) {
	password, err := (&resetPasswordCommand{}).password(strings.NewReader(""))

	require.NoError(t, err)
	require.Len(t, password, 32)
}
