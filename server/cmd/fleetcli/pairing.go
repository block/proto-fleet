package main

import (
	"context"
	"fmt"

	pairingv1 "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/urfave/cli/v3"
)

// pairingCommand stays handwritten: Discover is a server-streaming RPC the
// CLI generator does not cover, and pair reuses the shared miner selector UX.
func pairingCommand() *cli.Command {
	return &cli.Command{
		Name:  "pairing",
		Usage: "Discover and pair network devices",
		Commands: []*cli.Command{
			pairingDiscoverCommand(),
			pairingPairCommand(),
		},
	}
}

func pairingDiscoverCommand() *cli.Command {
	return &cli.Command{
		Name:  "discover",
		Usage: "Discover devices and print the aggregated stream results",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{Name: "ip", Usage: "IP address or hostname to check; repeatable"},
			&cli.StringSliceFlag{Name: "port", Usage: "Port to check on each IP; repeatable, defaults to plugin scan ports"},
			&cli.StringFlag{Name: "json", Usage: "Path to a DiscoverRequest JSON file for ip_range, mdns, or nmap modes, or - for stdin"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			req, err := buildDiscoverRequest(cmd.StringSlice("ip"), cmd.StringSlice("port"), cmd.String("json"))
			if err != nil {
				return err
			}

			client, err := openClient(ctx, cmd)
			if err != nil {
				return err
			}
			defer func() { _ = client.Close() }()

			resp, err := client.DiscoverDevices(ctx, req)
			if err != nil {
				return err
			}
			return printProto(resp)
		},
	}
}

func buildDiscoverRequest(ips, ports []string, jsonPath string) (*pairingv1.DiscoverRequest, error) {
	switch {
	case jsonPath != "" && (len(ips) > 0 || len(ports) > 0):
		return nil, fmt.Errorf("use either --json or --ip/--port, not both")
	case jsonPath != "":
		req := &pairingv1.DiscoverRequest{}
		if err := readProtoJSON(jsonPath, req); err != nil {
			return nil, err
		}
		if req.GetMode() == nil {
			return nil, fmt.Errorf("discover JSON must set one mode: ip_list, ip_range, mdns, or nmap")
		}
		return req, nil
	case len(ips) > 0:
		return &pairingv1.DiscoverRequest{
			Mode: &pairingv1.DiscoverRequest_IpList{
				IpList: &pairingv1.IPListModeRequest{
					IpAddresses: ips,
					Ports:       ports,
				},
			},
		}, nil
	default:
		return nil, fmt.Errorf("one of --ip or --json is required")
	}
}

func pairingPairCommand() *cli.Command {
	return &cli.Command{
		Name:  "pair",
		Usage: "Pair discovered devices selected by device, group, or rack",
		Flags: pairingPairFlags(),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			client, err := openClient(ctx, cmd)
			if err != nil {
				return err
			}
			defer func() { _ = client.Close() }()

			req, err := buildPairRequest(ctx, cmd, client)
			if err != nil {
				return err
			}

			resp, err := client.PairDevices(ctx, req)
			if err != nil {
				return err
			}
			return printProto(resp)
		},
	}
}

func pairingPairFlags() []cli.Flag {
	return append([]cli.Flag{
		&cli.StringFlag{Name: "device-username", Usage: "Device login username used during pairing; distinct from Fleet auth"},
		&cli.StringFlag{Name: "device-password", Usage: "Device login password used during pairing; distinct from Fleet auth"},
	}, generatedMinerSelectorFlags()...)
}

func buildPairRequest(ctx context.Context, cmd *cli.Command, client *Client) (*pairingv1.PairRequest, error) {
	selector, err := generatedBuildMinerSelector(ctx, cmd, client)
	if err != nil {
		return nil, err
	}
	return &pairingv1.PairRequest{
		Credentials:    pairDeviceCredentials(cmd),
		DeviceSelector: selector,
	}, nil
}

// pairDeviceCredentials returns device credentials only when at least one
// device credential flag was provided, so the server can fall back to plugin
// default credentials otherwise.
func pairDeviceCredentials(cmd *cli.Command) *pairingv1.Credentials {
	if !cmd.IsSet("device-username") && !cmd.IsSet("device-password") {
		return nil
	}
	credentials := &pairingv1.Credentials{Username: cmd.String("device-username")}
	if cmd.IsSet("device-password") {
		password := cmd.String("device-password")
		credentials.Password = &password
	}
	return credentials
}
