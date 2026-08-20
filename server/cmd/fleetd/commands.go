package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

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
	PasswordStdin bool      `help:"Read the replacement password from standard input (required)."`
}

// fleetdCommandPaths lists every kong command path exactly once. Both the arg
// normalization in main.go and the YAML config mapping below derive from it,
// so a new command cannot be routed by one and missed by the other.
var fleetdCommandPaths = []string{"server", "admin reset-password"}

func (cmd *serverCommand) Run() error {
	slog.Info("fleetd starting", "version", version)
	return start(&cmd.Config)
}

// resetPasswordTimeout bounds the whole database phase of a reset. Recovery
// runs while the operator is locked out, so a held lock or unreachable
// database must fail the command instead of hanging it.
const resetPasswordTimeout = 60 * time.Second

func (cmd *resetPasswordCommand) Run(ctx context.Context) error {
	password, err := cmd.password(os.Stdin)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, resetPasswordTimeout)
	defer cancel()

	conn, err := db.ConnectToDatabase(&cmd.DB)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			slog.Warn("failed to close database connection", "error", err)
		}
	}()

	// sql.Open never dials; ping so an unreachable database fails fast with a
	// connection error instead of surfacing mid-reset.
	pingCtx, pingCancel := context.WithTimeout(ctx, cmd.DB.InitialConnectionTimeout)
	defer pingCancel()
	if err := conn.PingContext(pingCtx); err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}

	userStore := sqlstores.NewSQLUserStore(conn)
	sessionService := session.NewService(session.Config{}, sqlstores.NewSQLSessionStore(conn))
	activityService := activity.NewService(sqlstores.NewSQLActivityStore(conn))
	resetService := authDomain.NewBreakGlassService(
		userStore,
		sqlstores.NewSQLTransactor(conn),
		sessionService,
		activityService,
	)
	username, err := resetService.ResetSuperAdminPassword(ctx, password)
	if err != nil {
		return err
	}

	reportResetResult(os.Stdout, os.Stderr, username)
	return nil
}

// reportResetResult announces a committed reset. Best-effort by design: the
// reset is already committed, so a broken stdout must not report failure or
// the wrapper would withhold a temporary password that is already active.
func reportResetResult(stdout, stderr io.Writer, username string) {
	if _, err := fmt.Fprintf(stdout, "Reset the password for SUPER_ADMIN %q.\n", username); err != nil {
		// Also best-effort: there is nowhere left to report a broken stderr.
		_, _ = fmt.Fprintf(stderr, "warning: password reset committed but writing the result failed: %v\n", err)
	}
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
	mounts := map[string]any{}
	for _, path := range fleetdCommandPaths {
		node := mounts
		names := strings.Fields(path)
		for _, name := range names[:len(names)-1] {
			child, ok := node[name].(map[string]any)
			if !ok {
				child = map[string]any{}
				node[name] = child
			}
			node = child
		}
		node[names[len(names)-1]] = root
	}
	wrapped, err := yaml.Marshal(mounts)
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
		return "", fmt.Errorf("--password-stdin is required; use the host recovery wrapper to generate a temporary password")
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
