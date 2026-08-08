package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

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
	default:
		return usageError()
	}
}

func usageError() error {
	return errors.New("usage: fleet-ha <generate-secrets|preflight|bootstrap-etcd-auth|render-keepalived|compose|status> ...")
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
