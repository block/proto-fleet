package main

import (
	"github.com/alecthomas/kong"
	kongyaml "github.com/alecthomas/kong-yaml"

	"github.com/block/proto-fleet/server/internal/fleetnodebootstrap"
)

type Context struct {
	StateDir string
}

type CLI struct {
	StateDir string `help:"override state directory; defaults to $XDG_STATE_HOME/fleetnode or ~/.local/state/fleetnode" type:"path"`

	Enroll  EnrollCmd  `cmd:"" help:"register this fleet node with a fleet server"`
	Status  StatusCmd  `cmd:"" help:"print local fleet node state"`
	Refresh RefreshCmd `cmd:"" help:"renew the session token using the stored api_key"`
}

func main() {
	var cli CLI
	kctx := kong.Parse(&cli,
		kong.Name("fleetnode"),
		kong.Description("Fleet node CLI: enroll, authenticate, refresh."),
		kong.Configuration(kongyaml.Loader, "/etc/fleetnode/config.yaml"),
	)
	stateDir, err := fleetnodebootstrap.ResolveStateDir(cli.StateDir)
	kctx.FatalIfErrorf(err)
	kctx.FatalIfErrorf(kctx.Run(&Context{StateDir: stateDir}))
}
