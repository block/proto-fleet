package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/alecthomas/kong"

	"github.com/block/proto-fleet/server/internal/ha/deployment"
)

const (
	defaultNodeEnv          = "node.env"
	defaultFirewallTemplate = "firewall.nft.tmpl"
)

type cli struct {
	Preflight         preflightCmd         `cmd:"" help:"validate an HA host before installation"`
	BootstrapEtcdAuth bootstrapEtcdAuthCmd `cmd:"" help:"enable etcd authentication and create service roles"`
	RenderKeepalived  renderKeepalivedCmd  `cmd:"" help:"render the keepalived configuration"`
	Compose           composeCmd           `cmd:"" help:"run Docker Compose with the installed HA environment" passthrough:""`
	Status            statusCmd            `cmd:"" help:"print local HA status as JSON"`
	Install           installCmd           `cmd:"" help:"prepare a new cluster or install a prepared host bundle"`
	Start             startCmd             `cmd:"" help:"start installed HA services"`
	Stop              stopCmd              `cmd:"" help:"stop installed HA services"`
}

type preflightCmd struct {
	NodeEnv          string `arg:"" optional:"" default:"${default_node_env}" type:"path" help:"node environment file"`
	FirewallTemplate string `arg:"" optional:"" default:"${default_firewall_template}" type:"path" help:"nftables template"`
}

func (c *preflightCmd) Run(ctx context.Context) error {
	config, err := deployment.Preflight(ctx, c.NodeEnv, c.FirewallTemplate)
	if err == nil {
		fmt.Printf("HA preflight passed for %s (%s)\n", config.NodeName, config.NodeIP)
	}
	return err
}

type bootstrapEtcdAuthCmd struct {
	RootPasswordFile string `arg:"" type:"path" help:"file containing the etcd root password"`
	NodeEnv          string `name:"node-env" default:"${default_node_env}" type:"path" help:"node environment file"`
}

func (c *bootstrapEtcdAuthCmd) Run(ctx context.Context) error {
	if err := deployment.BootstrapEtcdAuth(ctx, c.NodeEnv, c.RootPasswordFile); err != nil {
		return err
	}
	fmt.Println("etcd authentication enabled with Patroni read/write and Fleet read-only roles")
	return nil
}

type renderKeepalivedCmd struct {
	NodeEnv  string `arg:"" type:"path" help:"node environment file"`
	Template string `arg:"" type:"path" help:"keepalived template"`
	Output   string `arg:"" type:"path" help:"rendered configuration path"`
}

func (c *renderKeepalivedCmd) Run() error {
	if err := deployment.RenderKeepalivedConfig(c.NodeEnv, c.Template, c.Output); err != nil {
		return err
	}
	fmt.Printf("keepalived configuration written to %s\n", c.Output)
	return nil
}

type composeCmd struct {
	Args []string `arg:"" name:"compose-arg" help:"arguments passed to Docker Compose"`
}

func (c *composeCmd) Run(ctx context.Context) error {
	return deployment.RunCompose(ctx, c.Args)
}

type statusCmd struct {
	NodeEnv string `arg:"" optional:"" default:"${default_node_env}" type:"path" help:"node environment file"`
}

func (c *statusCmd) Run(ctx context.Context) error {
	return runStatus(ctx, c.NodeEnv, os.Stdout, deployment.Status)
}

type installCmd struct {
	HostBundle string `arg:"" optional:"" type:"path" help:"prepared host bundle; omit when preparing ha-a"`
}

func (c *installCmd) Run(ctx context.Context) error {
	if err := deployment.GuidedInstall(ctx, c.HostBundle); err != nil {
		return err
	}
	fmt.Println("Proto Fleet HA installation completed")
	return nil
}

type startCmd struct {
	NodeEnv              string `arg:"" type:"path" help:"node environment file"`
	EtcdRootPasswordFile string `name:"etcd-root-password-file" type:"path" help:"file containing the etcd root password"`
}

func (c *startCmd) Run(ctx context.Context) error {
	return deployment.StartInstalledServices(ctx, c.NodeEnv, c.EtcdRootPasswordFile)
}

type stopCmd struct {
	NodeEnv string `arg:"" type:"path" help:"node environment file"`
}

func (c *stopCmd) Run(ctx context.Context) error {
	return deployment.StopInstalledServices(ctx, c.NodeEnv)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
	defer stop()

	var cli cli
	kctx := kong.Parse(&cli,
		kong.Name("fleet-ha"),
		kong.Description("Install and operate a Proto Fleet HA cluster."),
		kong.BindTo(ctx, (*context.Context)(nil)),
		kong.Vars{
			"default_node_env":          defaultNodeEnv,
			"default_firewall_template": defaultFirewallTemplate,
		},
	)
	kctx.FatalIfErrorf(kctx.Run())
}

type statusReader func(context.Context, string) (deployment.StatusReport, error)

func runStatus(ctx context.Context, envPath string, output io.Writer, read statusReader) error {
	report, err := read(ctx, envPath)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("write HA status: %w", err)
	}
	if report.Control == nil || !report.Control.FailoverReady {
		return errors.New("HA failover readiness check failed")
	}
	return nil
}
