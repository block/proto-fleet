package main

import (
	"context"
	"errors"
	"fmt"
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
		if len(args) != 5 {
			return errors.New("usage: fleet-ha generate-secrets OUTPUT_DIR DB_A_IP DB_B_IP DCS_C_IP")
		}
		return deployment.GenerateSecrets(args[1], [3]string{args[2], args[3], args[4]})
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
	default:
		return usageError()
	}
}

func usageError() error {
	return errors.New("usage: fleet-ha <generate-secrets|preflight|bootstrap-etcd-auth> ...")
}
