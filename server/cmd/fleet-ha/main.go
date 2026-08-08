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
	"time"

	"github.com/alecthomas/kong"
	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/ha"
	"github.com/block/proto-fleet/server/internal/ha/deployment"
	"github.com/block/proto-fleet/server/internal/updaterapi"
)

const (
	defaultNodeEnv          = "node.env"
	defaultFirewallTemplate = "firewall.nft.tmpl"
	installedNodeEnv        = "/etc/proto-fleet/ha/node.env"
	defaultUpdaterSocket    = "/run/proto-fleet-updater/updater.sock"
)

type cli struct {
	Preflight         preflightCmd         `cmd:"" help:"validate an HA host before installation"`
	BootstrapEtcdAuth bootstrapEtcdAuthCmd `cmd:"" help:"enable etcd authentication and create service roles"`
	RenderKeepalived  renderKeepalivedCmd  `cmd:"" help:"render the keepalived configuration"`
	Compose           composeCmd           `cmd:"" help:"run Docker Compose with the installed HA environment" passthrough:""`
	Status            statusCmd            `cmd:"" help:"print local HA status as JSON"`
	Install           installCmd           `cmd:"" help:"prepare a new cluster or install a prepared host bundle"`
	Update            updateCmd            `cmd:"" help:"update the application services on a passive HA host"`
	Start             startCmd             `cmd:"" help:"start installed HA services"`
	Stop              stopCmd              `cmd:"" help:"stop installed HA services"`
	RequirePassive    requirePassiveCmd    `cmd:"" help:"verify that the local Fleet instance is passive"`
	RequireActive     requireActiveCmd     `cmd:"" help:"verify that the local Fleet instance is active"`
	UpdatePreflight   updatePreflightCmd   `cmd:"" help:"prepare the current release for an application update"`
	AppStop           appStopCmd           `cmd:"" help:"stop the Fleet application services"`
	AppStart          appStartCmd          `cmd:"" help:"start the Fleet application services"`
	WaitTakeover      waitTakeoverCmd      `cmd:"" help:"wait for the VIP to serve an application version"`
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

type updateCmd struct {
	Version  string `arg:"" help:"target application version"`
	Complete bool   `help:"complete the update through failover from the active host"`
}

func (c *updateCmd) Run(ctx context.Context) error {
	return runPassiveUpdate(ctx, c.Version, c.Complete, os.Stdout, validateHAUpdate, updaterapi.NewClient(defaultUpdaterSocket), deployment.Status)
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

type requirePassiveCmd struct {
	NodeEnv string `arg:"" type:"path" help:"node environment file"`
	Version string `arg:"" help:"target application version"`
}

func (c *requirePassiveCmd) Run(ctx context.Context) error {
	return deployment.ValidatePassiveUpdate(ctx, c.NodeEnv, c.Version)
}

type requireActiveCmd struct {
	NodeEnv string `arg:"" type:"path" help:"node environment file"`
}

func (c *requireActiveCmd) Run(ctx context.Context) error {
	return deployment.RequireActive(ctx, c.NodeEnv)
}

type updatePreflightCmd struct{}

func (*updatePreflightCmd) Run(ctx context.Context) error {
	root, err := deployment.ReleaseRoot()
	if err != nil {
		return err
	}
	return deployment.PrepareApplicationUpdate(ctx, root)
}

type appStopCmd struct {
	Role ha.RuntimeRole `arg:"" enum:"passive,active" help:"expected local HA role"`
}

func (c *appStopCmd) Run(ctx context.Context) error {
	root, err := deployment.ReleaseRoot()
	if err != nil {
		return err
	}
	return deployment.StopApplication(ctx, root, c.Role)
}

type appStartCmd struct {
	Version string `arg:"" help:"application version to start"`
	Mode    string `arg:"" optional:"" default:"passive" enum:"passive,any" help:"required HA role after startup"`
}

func (c *appStartCmd) Run(ctx context.Context) error {
	root, err := deployment.ReleaseRoot()
	if err != nil {
		return err
	}
	return deployment.StartApplication(ctx, root, c.Version, c.Mode == "passive")
}

type waitTakeoverCmd struct {
	Version string `arg:"" help:"application version expected on the VIP"`
}

func (c *waitTakeoverCmd) Run(ctx context.Context) error {
	return deployment.WaitForVIPVersion(ctx, installedNodeEnv, c.Version)
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

type updaterClient interface {
	Status(ctx context.Context) (updaterapi.StatusResponse, error)
	Trigger(ctx context.Context, operationID, targetVersion string) (updaterapi.Operation, error)
	TriggerComplete(ctx context.Context, operationID, targetVersion string) (updaterapi.Operation, error)
}

const (
	updatePollInterval         = 2 * time.Second
	updateCommunicationTimeout = 30 * time.Second
)

type updatePreflight func(context.Context, string, string, bool) error

func validateHAUpdate(ctx context.Context, envPath, targetVersion string, complete bool) error {
	if complete {
		_, err := deployment.ValidateActiveUpdate(ctx, envPath, targetVersion)
		return err
	}
	return deployment.ValidatePassiveUpdate(ctx, envPath, targetVersion)
}

func runPassiveUpdate(
	ctx context.Context,
	targetVersion string,
	complete bool,
	output io.Writer,
	preflight updatePreflight,
	client updaterClient,
	read statusReader,
) error {
	if err := runUpdate(ctx, targetVersion, complete, output, preflight, client); err != nil {
		return err
	}
	report, err := read(ctx, installedNodeEnv)
	if err != nil {
		return fmt.Errorf("update succeeded but local HA outcome could not be verified: %w", err)
	}
	if report.Control != nil && report.Control.FailoverReady {
		return nil
	}
	if deployment.ExpectedRollingVersionMismatch(report.Control) {
		_, err = fmt.Fprintln(output, "Update succeeded; failover readiness will recover after the peer is updated.")
		if err != nil {
			return fmt.Errorf("write update outcome: %w", err)
		}
		return nil
	}
	if _, err = fmt.Fprintln(output, "Update succeeded, but failover redundancy is degraded. Run fleet-ha status."); err != nil {
		return fmt.Errorf("write update outcome: %w", err)
	}
	return errors.New("update succeeded but failover readiness is degraded")
}

func runUpdate(
	ctx context.Context,
	targetVersion string,
	complete bool,
	output io.Writer,
	preflight updatePreflight,
	client updaterClient,
) error {
	if err := preflight(ctx, installedNodeEnv, targetVersion, complete); err != nil {
		return err
	}
	operationID := uuid.NewString()
	trigger := client.Trigger
	if complete {
		trigger = client.TriggerComplete
	}
	operation, err := triggerUpdate(ctx, operationID, targetVersion, trigger)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "Update operation %s accepted\n", operation.ID); err != nil {
		return fmt.Errorf("write update operation: %w", err)
	}
	lastPhase := updaterapi.Phase("")
	var statusFailureSince time.Time
	for {
		if operation.Phase != lastPhase {
			if _, err := fmt.Fprintf(output, "Update %s: %s\n", operation.TargetVersion, operation.Phase); err != nil {
				return fmt.Errorf("write update status: %w", err)
			}
			lastPhase = operation.Phase
		}
		if operation.Phase.Terminal() {
			if operation.Phase == updaterapi.PhaseFailed {
				message := "HA application update failed: " + operation.Error
				if operation.RecoveryCommand != "" {
					message += "\nRecovery: " + operation.RecoveryCommand
				}
				if operation.LogPath != "" {
					message += "\nLog: " + operation.LogPath
				}
				return errors.New(message)
			}
			if operation.Message != "" {
				if _, err := fmt.Fprintln(output, operation.Message); err != nil {
					return fmt.Errorf("write update result: %w", err)
				}
			}
			if operation.LogPath != "" {
				if _, err := fmt.Fprintln(output, "Log:", operation.LogPath); err != nil {
					return fmt.Errorf("write update log path: %w", err)
				}
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("stopped waiting for HA application update; the accepted host update may continue: %w", ctx.Err())
		case <-time.After(updatePollInterval):
		}
		status, err := client.Status(ctx)
		if err != nil {
			if statusFailureSince.IsZero() {
				statusFailureSince = time.Now()
			}
			if time.Since(statusFailureSince) >= updateCommunicationTimeout {
				return fmt.Errorf("host updater status remained unavailable for %s: %w", updateCommunicationTimeout, err)
			}
			continue
		}
		statusFailureSince = time.Time{}
		if status.Operation == nil || status.Operation.ID != operationID || status.Operation.TargetVersion != targetVersion {
			return errors.New("host updater lost the accepted operation")
		}
		operation = *status.Operation
	}
}

func triggerUpdate(ctx context.Context, operationID, targetVersion string, trigger func(context.Context, string, string) (updaterapi.Operation, error)) (updaterapi.Operation, error) {
	reconcileCtx, cancel := context.WithTimeout(ctx, updateCommunicationTimeout)
	defer cancel()
	ambiguous := false
	for {
		operation, err := trigger(reconcileCtx, operationID, targetVersion)
		if err == nil {
			return operation, nil
		}
		var transportError *updaterapi.TransportError
		var protocolError *updaterapi.ProtocolError
		if errors.As(err, &transportError) || errors.As(err, &protocolError) {
			ambiguous = true
		}
		if !ambiguous {
			if errors.Is(err, updaterapi.ErrUnavailable) {
				return updaterapi.Operation{}, err
			}
			var rejection *updaterapi.HTTPError
			if errors.As(err, &rejection) {
				return updaterapi.Operation{}, err
			}
		}
		select {
		case <-reconcileCtx.Done():
			if ambiguous {
				return updaterapi.Operation{}, fmt.Errorf(
					"reconcile HA application update %s; the operation may have been accepted and may continue: %w",
					operationID,
					errors.Join(reconcileCtx.Err(), err),
				)
			}
			return updaterapi.Operation{}, fmt.Errorf("reconcile HA application update: %w", errors.Join(reconcileCtx.Err(), err))
		case <-time.After(updatePollInterval):
		}
	}
}
