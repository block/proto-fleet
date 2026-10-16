package main

import (
	"fmt"

	"github.com/alecthomas/kong"
	kongyaml "github.com/alecthomas/kong-yaml"

	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
	"github.com/block/proto-fleet/server/internal/releaseinfo"
)

var version = "dev"

type Context struct {
	StateDir string
}

type CLI struct {
	Version                kong.VersionFlag           `help:"print running binary version"`
	PrintReleaseRepository printReleaseRepositoryFlag `help:"print the embedded publishing repository and exit"`
	StateDir               string                     `help:"override state directory; defaults to $XDG_STATE_HOME/fleetnode or ~/.local/state/fleetnode" type:"path"`

	Enroll  EnrollCmd  `cmd:"" help:"register this fleet node with a fleet server"`
	Status  StatusCmd  `cmd:"" help:"print local fleet node state"`
	Refresh RefreshCmd `cmd:"" help:"renew the session token using the stored api_key"`
	Run     RunCmd     `cmd:"" help:"run as a long-running daemon; maintain session and post heartbeats"`
}

type printReleaseRepositoryFlag bool

func (printReleaseRepositoryFlag) BeforeReset(app *kong.Kong) error {
	if err := releaseinfo.ValidateRepository(releaseinfo.Repository); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(app.Stdout, releaseinfo.Repository)
	app.Exit(0)
	return nil
}

func main() {
	var cli CLI
	kctx := kong.Parse(&cli,
		kong.Name("fleetnode"),
		kong.Vars{"version": version},
		kong.Description("Fleet node CLI: enroll, authenticate, refresh."),
		kong.Configuration(kongyaml.Loader, "/etc/fleetnode/config.yaml"),
	)
	stateDir, err := bootstrap.ResolveStateDir(cli.StateDir)
	kctx.FatalIfErrorf(err)
	kctx.FatalIfErrorf(kctx.Run(&Context{StateDir: stateDir}))
}
