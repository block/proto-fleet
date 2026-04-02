package main

import (
	"log"

	"github.com/block/proto-fleet/plugin/antminer/internal/driver"
	"github.com/block/proto-fleet/plugin/antminer/internal/types"
	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer"
	"github.com/block/proto-fleet/server/sdk/v1"
	"github.com/hashicorp/go-plugin"
)

func main() {
	clientFactory := types.ClientFactory(func(host string, rpcPort, webPort int32, urlScheme string) (antminer.AntminerClient, error) {
		return antminer.NewClient(host, rpcPort, webPort, urlScheme)
	})

	antminerDriver, err := driver.New(clientFactory)
	if err != nil {
		log.Fatalf("Failed to create antminer driver: %v", err)
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: sdk.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"driver": &sdk.DriverPlugin{Impl: antminerDriver},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}
