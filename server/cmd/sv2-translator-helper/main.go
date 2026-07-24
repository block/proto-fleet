package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/block/proto-fleet/server/internal/domain/sv2/translator"
)

func main() {
	os.Exit(run())
}

func run() int {
	listenSocket := flag.String(
		"listen-socket",
		translator.DefaultHelperSocket,
		"private Unix socket exposed to fleet-api",
	)
	dockerSocket := flag.String(
		"docker-socket",
		translator.DefaultDockerSocket,
		"Docker Engine Unix socket available only inside this helper",
	)
	flag.Parse()

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	if err := translator.ServeRuntimeHelper(ctx, translator.RuntimeHelperConfig{
		ListenSocket: *listenSocket,
		DockerSocket: *dockerSocket,
	}); err != nil {
		slog.Error("SV2 translator lifecycle helper stopped", "error", err)
		return 1
	}
	return 0
}
