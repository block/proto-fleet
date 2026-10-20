package main

import (
	"context"
	"errors"
	"time"

	"github.com/alecthomas/kong"
	kongyaml "github.com/alecthomas/kong-yaml"

	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
	fleet_telemetry "github.com/block/proto-fleet/server/internal/infrastructure/fleet-telemetry"
)

const tracerShutdownTimeout = 5 * time.Second

// version is overwritten at release build time via -ldflags "-X main.version=<tag>".
var version = "dev"

type Context struct {
	StateDir string
}

type CLI struct {
	StateDir          string  `help:"override state directory; defaults to $XDG_STATE_HOME/fleetnode or ~/.local/state/fleetnode" type:"path"`
	TracingEnabled    bool    `name:"tracing-enabled" help:"enable OpenTelemetry command traces" env:"FLEETNODE_TRACING_ENABLED"`
	TracingEndpoint   string  `name:"tracing-endpoint" help:"OTLP HTTP endpoint for command traces" default:"http://localhost:4318" env:"FLEETNODE_TRACING_ENDPOINT"`
	TracingSampleRate float64 `name:"tracing-sample-rate" help:"fraction of command traces to sample (0.0-1.0)" default:"1.0" env:"FLEETNODE_TRACING_SAMPLE_RATE"`

	Enroll  EnrollCmd  `cmd:"" help:"register this fleet node with a fleet server"`
	Status  StatusCmd  `cmd:"" help:"print local fleet node state"`
	Refresh RefreshCmd `cmd:"" help:"renew the session token using the stored api_key"`
	Run     RunCmd     `cmd:"" help:"run as a long-running daemon; maintain session and post heartbeats"`
}

func main() {
	var cli CLI
	kctx := kong.Parse(&cli,
		kong.Name("fleetnode"),
		kong.Description("Fleet node CLI: enroll, authenticate, refresh."),
		kong.Configuration(kongyaml.Loader, "/etc/fleetnode/config.yaml"),
	)
	stateDir, err := bootstrap.ResolveStateDir(cli.StateDir)
	kctx.FatalIfErrorf(err)
	shutdownTracer, err := fleet_telemetry.Setup(context.Background(), version, fleet_telemetry.Config{
		Enabled:     cli.TracingEnabled,
		Endpoint:    cli.TracingEndpoint,
		ServiceName: "fleetnode",
		SampleRate:  cli.TracingSampleRate,
	})
	kctx.FatalIfErrorf(err)

	runErr := kctx.Run(&Context{StateDir: stateDir})
	shutdownCtx, cancel := context.WithTimeout(context.Background(), tracerShutdownTimeout)
	shutdownErr := shutdownTracer(shutdownCtx)
	cancel()
	kctx.FatalIfErrorf(errors.Join(runErr, shutdownErr))
}
