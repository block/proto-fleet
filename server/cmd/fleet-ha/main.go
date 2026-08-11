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

	"github.com/block/proto-fleet/server/internal/ha/deployment"
)

const (
	defaultNodeEnv          = "node.env"
	defaultFirewallTemplate = "firewall.nft.tmpl"
)

func main() {
	os.Exit(runMain())
}

func runMain() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
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
	case "start":
		return runStart(ctx, args[1:])
	case "stop":
		if len(args) != 2 {
			return errors.New("usage: fleet-ha stop NODE_ENV")
		}
		return deployment.StopInstalledServices(ctx, args[1])
	default:
		return usageError()
	}
}

func usageError() error {
	return errors.New("usage: fleet-ha <preflight|bootstrap-etcd-auth|render-keepalived|compose|status|install|start|stop> ...")
}

func runInstall(ctx context.Context, args []string) error {
	if len(args) > 1 {
		return errors.New("usage: fleet-ha install [HOST_BUNDLE]")
	}
	bundlePath := ""
	if len(args) == 1 {
		bundlePath = args[0]
	}
	if err := deployment.GuidedInstall(ctx, bundlePath); err != nil {
		return err
	}
	fmt.Println("Proto Fleet HA installation completed")
	return nil
}

func runStart(ctx context.Context, args []string) error {
	options, err := parseStartOptions(args)
	if err != nil {
		return err
	}
	return deployment.StartInstalledServices(ctx, options.NodeEnvPath, options.EtcdRootPasswordFile)
}

func parseStartOptions(args []string) (deployment.InstallOptions, error) {
	usage := "usage: fleet-ha start NODE_ENV [--etcd-root-password-file PATH]"
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

type statusReader func(context.Context, string) (deployment.StatusReport, error)

func runStatus(ctx context.Context, args []string, output io.Writer, read statusReader) error {
	envPath := defaultNodeEnv
	if len(args) > 1 || (len(args) == 1 && len(args[0]) > 0 && args[0][0] == '-') {
		return errors.New("usage: fleet-ha status [node.env]")
	}
	if len(args) == 1 {
		envPath = args[0]
	}

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
