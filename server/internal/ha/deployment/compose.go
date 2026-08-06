package deployment

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
)

// RunCompose prevents parent variables from overriding generated HA identity and secrets.
func RunCompose(ctx context.Context, args []string) error {
	protected := []string{"AUTH_CLIENT_SECRET_KEY", "ENCRYPT_SERVICE_MASTER_KEY"}
	for key := range allowedEnvKeys {
		protected = append(protected, key)
	}
	slices.Sort(protected)
	for _, key := range protected {
		if _, ok := os.LookupEnv(key); ok {
			return fmt.Errorf("parent environment must not set %s; unset it before running Fleet", key)
		}
	}

	commandArgs := append([]string{"compose"}, args...)
	command := exec.CommandContext(ctx, "docker", commandArgs...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("run Docker Compose: %w", err)
	}
	return nil
}
