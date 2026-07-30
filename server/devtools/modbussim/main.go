// modbussim runs the development-only Modbus TCP simulator.
package main

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"

	modbussim "github.com/block/proto-fleet/server/devtools/modbussim/server"
)

const (
	defaultListenAddress = ":5502"
	listenAddressEnv     = "MODBUS_SIM_LISTEN_ADDRESS"
)

func main() {
	if err := run(); err != nil {
		slog.Error("modbus simulator stopped", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	address := listenAddress()
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen for Modbus TCP: %w", err)
	}

	simulator := modbussim.New()
	defer func() {
		_ = simulator.Close()
	}()

	slog.Info("modbus simulator starting", slog.String("address", listener.Addr().String()))
	return simulator.Serve(listener)
}

func listenAddress() string {
	if address := strings.TrimSpace(os.Getenv(listenAddressEnv)); address != "" {
		return address
	}
	return defaultListenAddress
}
