package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/alecthomas/kong"
	kongyaml "github.com/alecthomas/kong-yaml"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	authDomain "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"gopkg.in/yaml.v3"
)

type fleetdCLI struct {
	Server serverCommand `cmd:"" default:"1" help:"Run the Fleet server."`
	Admin  adminCommand  `cmd:"" help:"Run an offline administrative operation."`
}

type serverCommand struct {
	Config `embed:""`
}

type adminCommand struct {
	ResetPassword resetPasswordCommand `cmd:"" name:"reset-password" help:"Reset the sole SUPER_ADMIN password."`
}

type resetPasswordCommand struct {
	DB            db.Config `embed:"" prefix:"db-" envprefix:"DB_"`
	PasswordStdin bool      `help:"Read the replacement password from standard input."`
}

type commandRuntime struct {
	Stdin  io.Reader
	Stdout io.Writer
}

func (cmd *serverCommand) Run() error {
	slog.Info("fleetd starting", "version", version)
	return start(&cmd.Config)
}

func (cmd *resetPasswordCommand) Run(ctx context.Context, runtime *commandRuntime) error {
	password, err := cmd.password(runtime.Stdin)
	if err != nil {
		return err
	}

	conn, err := db.ConnectToDatabase(&cmd.DB)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			slog.Warn("failed to close database connection", "error", err)
		}
	}()

	userStore := sqlstores.NewSQLUserStore(conn)
	sessionService := session.NewService(session.Config{}, sqlstores.NewSQLSessionStore(conn))
	activityService := activity.NewService(sqlstores.NewSQLActivityStore(conn))
	resetService := authDomain.NewBreakGlassService(
		userStore,
		sqlstores.NewSQLTransactor(conn),
		sessionService,
		activityService,
	)
	result, err := resetService.ResetSuperAdminPassword(ctx, password)
	if err != nil {
		return err
	}

	return cmd.writeResult(runtime.Stdout, result.Username, password)
}

func (cmd *resetPasswordCommand) writeResult(output io.Writer, username, password string) error {
	message := fmt.Sprintf("Reset the password for SUPER_ADMIN %q.\n", username)
	if !cmd.PasswordStdin {
		// Generated credentials are returned once, only after the transaction commits.
		message += fmt.Sprintf("Temporary password: %s\n", password)
	}
	if _, err := io.WriteString(output, message); err != nil {
		return fmt.Errorf("write reset result: %w", err)
	}
	return nil
}

// fleetdYAMLLoader maps the existing root-level fleetd YAML into each command
// path. This keeps deployed config.yaml files unchanged while allowing Kong to
// validate only the selected command's fields.
func fleetdYAMLLoader(reader io.Reader) (kong.Resolver, error) {
	contents, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read fleetd YAML config: %w", err)
	}
	root := map[string]any{}
	if len(bytes.TrimSpace(contents)) > 0 {
		if err := yaml.Unmarshal(contents, &root); err != nil {
			return nil, fmt.Errorf("decode fleetd YAML config: %w", err)
		}
	}
	wrapped, err := yaml.Marshal(map[string]any{
		"server": root,
		"admin": map[string]any{
			"reset-password": root,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("map fleetd YAML config to commands: %w", err)
	}
	resolver, err := kongyaml.Loader(bytes.NewReader(wrapped))
	if err != nil {
		return nil, fmt.Errorf("load mapped fleetd YAML config: %w", err)
	}
	return resolver, nil
}

func (cmd *resetPasswordCommand) password(stdin io.Reader) (string, error) {
	if !cmd.PasswordStdin {
		password, err := authDomain.GenerateTemporaryPassword()
		if err != nil {
			return "", fmt.Errorf("generate temporary password: %w", err)
		}
		return password, nil
	}

	password, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read password from standard input: %w", err)
	}
	password = strings.TrimSuffix(password, "\n")
	password = strings.TrimSuffix(password, "\r")
	if password == "" {
		return "", fmt.Errorf("password from standard input must not be empty")
	}
	if err := authDomain.ValidatePassword(password); err != nil {
		return "", err
	}
	return password, nil
}
