package main

import (
	"github.com/alecthomas/kong"
	kongyaml "github.com/alecthomas/kong-yaml"
)

type Context struct {
	StateDir string
}

type CLI struct {
	StateDir string `help:"override state directory; defaults to $XDG_STATE_HOME/fleet-agent or ~/.local/state/fleet-agent" type:"path"`

	Enroll  EnrollCmd  `cmd:"" help:"register this agent with a fleet server"`
	Status  StatusCmd  `cmd:"" help:"print local agent state"`
	Refresh RefreshCmd `cmd:"" help:"renew the session token using the stored api_key"`
}

func main() {
	var cli CLI
	kctx := kong.Parse(&cli,
		kong.Name("fleet-agent"),
		kong.Description("Fleet agent CLI: enroll, authenticate, refresh."),
		kong.Configuration(kongyaml.Loader, "/etc/fleet-agent/config.yaml"),
	)
	stateDir, err := resolveStateDir(cli.StateDir)
	kctx.FatalIfErrorf(err)
	kctx.FatalIfErrorf(kctx.Run(&Context{StateDir: stateDir}))
}
