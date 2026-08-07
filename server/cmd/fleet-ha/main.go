package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/block/proto-fleet/server/internal/ha/deployment"
)

const (
	defaultNodeEnv          = "node.env"
	defaultFirewallTemplate = "firewall.nft.tmpl"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "fleet-ha: %v\n", err)
		os.Exit(1)
	}
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
	return errors.New("usage: fleet-ha <generate-secrets|preflight|bootstrap-etcd-auth|render-keepalived|compose|status|install> ...")
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
