package main

import "github.com/urfave/cli/v3"

func manualCommands() []*cli.Command {
	return []*cli.Command{
		authCommand(),
		apiKeyCommand(),
		performanceCommand(),
		firmwareCommand(),
	}
}
