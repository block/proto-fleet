package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/ha/deployment"
	"github.com/block/proto-fleet/server/internal/updaterapi"
)

const (
	defaultNodeEnv          = "node.env"
	defaultFirewallTemplate = "firewall.nft.tmpl"
)

func main() {
	if runMain() != 0 {
		os.Exit(1)
	}
}

func runMain() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "fleet-ha: %v\n", err)
		return 1
	}
	return 0
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError()
	}
	switch args[0] {
	case "generate-secrets":
		if len(args) != 6 {
			return errors.New("usage: fleet-ha generate-secrets OUTPUT_DIR DB_A_IP DB_B_IP DCS_C_IP VIRTUAL_IP")
		}
		return deployment.GenerateSecrets(args[1], [3]string{args[2], args[3], args[4]}, args[5])
	case "preflight":
		if len(args) > 3 {
			return errors.New("usage: fleet-ha preflight [node.env] [firewall.nft.tmpl]")
		}
		envPath, templatePath := defaultNodeEnv, defaultFirewallTemplate
		if len(args) >= 2 {
			envPath = args[1]
		}
		if len(args) == 3 {
			templatePath = args[2]
		}
		config, err := deployment.Preflight(ctx, envPath, templatePath)
		if err == nil {
			fmt.Printf("HA preflight passed for %s (%s)\n", config.NodeName, config.NodeIP)
		}
		return err
	case "bootstrap-etcd-auth":
		if len(args) < 2 || len(args) > 3 {
			return errors.New("usage: fleet-ha bootstrap-etcd-auth [node.env] ETCD_ROOT_PASSWORD_FILE")
		}
		envPath, rootPasswordFile := defaultNodeEnv, args[1]
		if len(args) == 3 {
			envPath, rootPasswordFile = args[1], args[2]
		}
		if err := deployment.BootstrapEtcdAuth(ctx, envPath, rootPasswordFile); err != nil {
			return err
		}
		fmt.Println("etcd authentication enabled with Patroni read/write and Fleet read-only roles")
		return nil
	case "render-keepalived":
		if len(args) != 4 {
			return errors.New("usage: fleet-ha render-keepalived NODE_ENV TEMPLATE OUTPUT")
		}
		if err := deployment.RenderKeepalivedConfig(args[1], args[2], args[3]); err != nil {
			return err
		}
		fmt.Printf("keepalived configuration written to %s\n", args[3])
		return nil
	case "compose":
		if len(args) < 2 {
			return errors.New("usage: fleet-ha compose COMPOSE_ARGS...")
		}
		return deployment.RunCompose(ctx, args[1:])
	case "status":
		return runStatus(ctx, args[1:], os.Stdout, deployment.Status)
	case "install":
		return runInstall(ctx, args[1:])
	case "update":
		return runUpdate(ctx, args[1:], os.Stdout, deployment.RequirePassive, updaterapi.NewClient(defaultUpdaterSocket))
	case "start":
		return runStart(ctx, args[1:])
	case "stop":
		if len(args) != 2 {
			return errors.New("usage: fleet-ha stop NODE_ENV")
		}
		return deployment.StopInstalledServices(ctx, args[1])
	case "require-passive":
		if len(args) != 2 {
			return errors.New("usage: fleet-ha require-passive NODE_ENV")
		}
		return deployment.RequirePassive(ctx, args[1])
	case "update-preflight":
		if len(args) != 1 {
			return errors.New("usage: fleet-ha update-preflight")
		}
		root, err := deployment.ReleaseRoot()
		if err != nil {
			return err
		}
		return deployment.PrepareApplicationUpdate(ctx, root)
	case "app-stop":
		if len(args) != 1 {
			return errors.New("usage: fleet-ha app-stop")
		}
		root, err := deployment.ReleaseRoot()
		if err != nil {
			return err
		}
		return deployment.StopApplication(ctx, root)
	case "app-start":
		if len(args) != 2 {
			return errors.New("usage: fleet-ha app-start VERSION")
		}
		root, err := deployment.ReleaseRoot()
		if err != nil {
			return err
		}
		return deployment.StartApplication(ctx, root, args[1])
	default:
		return usageError()
	}
}

func usageError() error {
	return errors.New("usage: fleet-ha <generate-secrets|preflight|bootstrap-etcd-auth|render-keepalived|compose|status|install|start|stop|update> ...")
}

const (
	installedNodeEnv     = "/etc/proto-fleet/ha/node.env"
	defaultUpdaterSocket = "/run/proto-fleet-updater/updater.sock"
)

type updaterClient interface {
	Status(ctx context.Context) (updaterapi.StatusResponse, error)
	Trigger(ctx context.Context, operationID, targetVersion string) (updaterapi.Operation, error)
}

const (
	updatePollInterval         = 2 * time.Second
	updateCommunicationTimeout = 30 * time.Second
)

type updateTrigger func(context.Context, string, string) (updaterapi.Operation, error)

func runUpdate(
	ctx context.Context,
	args []string,
	output io.Writer,
	requirePassive func(context.Context, string) error,
	client updaterClient,
) error {
	if len(args) != 1 {
		return errors.New("usage: fleet-ha update VERSION")
	}
	if err := requirePassive(ctx, installedNodeEnv); err != nil {
		return err
	}
	operationID := uuid.NewString()
	operation, err := triggerUpdate(ctx, operationID, args[0], client.Trigger)
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
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for HA application update: %w", ctx.Err())
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
		if status.Operation == nil || status.Operation.ID != operationID || status.Operation.TargetVersion != args[0] {
			return errors.New("host updater lost the accepted operation")
		}
		operation = *status.Operation
	}
}

func triggerUpdate(ctx context.Context, operationID, targetVersion string, trigger updateTrigger) (updaterapi.Operation, error) {
	reconcileCtx, cancel := context.WithTimeout(ctx, updateCommunicationTimeout)
	defer cancel()
	for {
		operation, err := trigger(reconcileCtx, operationID, targetVersion)
		if err == nil {
			return operation, nil
		}
		if errors.Is(err, updaterapi.ErrUnavailable) {
			return updaterapi.Operation{}, err
		}
		var rejection *updaterapi.HTTPError
		if errors.As(err, &rejection) {
			return updaterapi.Operation{}, err
		}
		select {
		case <-reconcileCtx.Done():
			return updaterapi.Operation{}, fmt.Errorf("reconcile HA application update: %w", errors.Join(reconcileCtx.Err(), err))
		case <-time.After(updatePollInterval):
		}
	}
}

func runInstall(ctx context.Context, args []string) error {
	options, err := parseInstallOptions("install", args)
	if err != nil {
		return err
	}
	if err := deployment.Install(ctx, options); err != nil {
		return err
	}
	fmt.Println("Proto Fleet HA installation completed")
	return nil
}

func runStart(ctx context.Context, args []string) error {
	options, err := parseInstallOptions("start", args)
	if err != nil {
		return err
	}
	return deployment.StartInstalledServices(ctx, options.NodeEnvPath, options.EtcdRootPasswordFile)
}

func parseInstallOptions(command string, args []string) (deployment.InstallOptions, error) {
	usage := fmt.Sprintf("usage: fleet-ha %s NODE_ENV [--etcd-root-password-file PATH]", command)
	if len(args) != 1 && len(args) != 3 {
		return deployment.InstallOptions{}, errors.New(usage)
	}
	options := deployment.InstallOptions{NodeEnvPath: args[0]}
	if len(args) == 3 {
		if args[1] != "--etcd-root-password-file" || args[2] == "" {
			return deployment.InstallOptions{}, errors.New(usage)
		}
		options.EtcdRootPasswordFile = args[2]
	}
	return options, nil
}

type statusReader func(context.Context, string, bool) (deployment.StatusReport, error)

func runStatus(ctx context.Context, args []string, output io.Writer, read statusReader) error {
	envPath := defaultNodeEnv
	jsonOutput, check := false, false
	positional := 0
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOutput = true
		case "--check":
			check = true
		default:
			if len(arg) > 0 && arg[0] == '-' {
				return errors.New("usage: fleet-ha status [node.env] [--json] [--check]")
			}
			positional++
			if positional > 1 {
				return errors.New("usage: fleet-ha status [node.env] [--json] [--check]")
			}
			envPath = arg
		}
	}
	report, err := read(ctx, envPath, check)
	if err != nil {
		return err
	}
	if jsonOutput {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			return fmt.Errorf("write HA status: %w", err)
		}
	} else {
		lines := []string{
			fmt.Sprintf("Fleet HA: %s (%s)", report.Runtime.Role, report.Runtime.Observation),
			fmt.Sprintf("Endpoint: %s", report.Runtime.Endpoint),
		}
		if report.Control != nil {
			lines = append(lines,
				fmt.Sprintf("Control ready: %t", report.Control.ControlReady),
				fmt.Sprintf("Failover ready: %t", report.Control.FailoverReady),
			)
		}
		if report.Control != nil && len(report.Control.ReasonCodes) > 0 {
			lines = append(lines, fmt.Sprintf("Reasons: %v", report.Control.ReasonCodes))
		}
		if _, err := fmt.Fprintln(output, strings.Join(lines, "\n")); err != nil {
			return fmt.Errorf("write HA status: %w", err)
		}
	}
	if check && (report.Control == nil || !report.Control.FailoverReady) {
		return errors.New("HA failover readiness check failed")
	}
	return nil
}
