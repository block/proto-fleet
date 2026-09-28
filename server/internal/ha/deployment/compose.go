package deployment

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const localDockerHost = "unix:///var/run/docker.sock"

const installedFleetEnvironment = configRoot + "/" + fleetEnvironmentFile

const (
	haTracingHealthTimeout = 15 * time.Second
	haTracingHealthRetry   = 250 * time.Millisecond
)

type fleetApplicationStartDependencies struct {
	environmentPath string
	runCompose      func(context.Context, []string) error
	collectorReady  func(context.Context) error
	warnings        io.Writer
}

func fleetApplicationComposeArgsAtProfile(root, environmentPath string, profile fleetApplicationProfile, operation string, flags ...string) []string {
	return fleetComposeArgsAtProfile(root, environmentPath, profile, operation, slices.Concat(flags, []string{"fleet-api", "fleet-client"}, profile.sidecars())...)
}

func startFleetApplication(ctx context.Context, root string, flags ...string) error {
	return startFleetApplicationWith(ctx, root, fleetApplicationStartDependencies{
		environmentPath: installedFleetEnvironment,
		runCompose:      RunCompose,
		collectorReady:  waitForTracingCollector,
		warnings:        os.Stderr,
	}, flags...)
}

func startFleetApplicationWith(ctx context.Context, root string, deps fleetApplicationStartDependencies, flags ...string) error {
	profile, err := loadFleetApplicationProfileFile(deps.environmentPath, true)
	if err != nil {
		return fmt.Errorf("load application profile: %w", err)
	}
	services := []string{"fleet-api", "fleet-client"}
	if profile.enabled("ENABLE_BETA_ALERTS") {
		services = append(services, "grafana")
	}
	args := fleetComposeArgsAtProfile(root, deps.environmentPath, profile, "up", slices.Concat(flags, []string{"--remove-orphans"}, services)...)
	if err := deps.runCompose(ctx, args); err != nil {
		return err
	}
	if profile.enabled("ENABLE_TRACING") {
		collectorArgs := fleetComposeArgsAtProfile(root, deps.environmentPath, profile, "up", "-d", "--no-deps", "--no-build", "--pull", "never", "otel-collector")
		collectorErr := deps.runCompose(ctx, collectorArgs)
		if collectorErr == nil {
			collectorErr = deps.collectorReady(ctx)
		}
		if collectorErr != nil {
			_, _ = fmt.Fprintf(deps.warnings, "[warning] Fleet is running, but tracing is degraded: %v\n", collectorErr)
		}
	}
	return nil
}

func waitForTracingCollector(ctx context.Context) error {
	healthCtx, cancel := context.WithTimeout(ctx, haTracingHealthTimeout)
	defer cancel()
	client, cleanup := newProbeHTTPClient(nil, nil)
	defer cleanup()
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/", haTracingHealthHostPort)
	if err := waitForHTTPEndpoint(healthCtx, client, endpoint, haTracingHealthRetry); err != nil {
		return fmt.Errorf("collector health endpoint did not become ready: %w", err)
	}
	return nil
}

func waitForHTTPEndpoint(ctx context.Context, client *http.Client, endpoint string, retryInterval time.Duration) error {
	for {
		if endpointReadyWithClient(ctx, client, endpoint) {
			return nil
		}
		timer := time.NewTimer(retryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("wait for %s: %w", endpoint, ctx.Err())
		case <-timer.C:
		}
	}
}

func fleetApplicationDownArgsAt(root, environmentPath, ownershipMarker string, flags ...string) ([]string, error) {
	return fleetComposeArgsForInstalledProfileAt(root, environmentPath, ownershipMarker, "down", slices.Concat(flags, []string{"--remove-orphans"})...)
}

func fleetSidecarPullArgs(root, environmentPath string, profile fleetApplicationProfile) []string {
	sidecars := profile.sidecars()
	if len(sidecars) == 0 {
		return nil
	}
	return fleetComposeArgsAtProfile(root, environmentPath, profile, "pull", sidecars...)
}

func fleetComposeArgsForInstalledProfileAt(root, environmentPath, ownershipMarker, operation string, flags ...string) ([]string, error) {
	_, markerErr := os.Stat(ownershipMarker)
	if markerErr != nil && !errors.Is(markerErr, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect installed HA Grafana ownership marker: %w", markerErr)
	}
	profile, err := loadFleetApplicationProfileFile(environmentPath, markerErr == nil)
	if err != nil {
		return nil, err
	}
	return fleetComposeArgsAtProfile(root, environmentPath, profile, operation, flags...), nil
}

// ResetSuperAdminPassword runs the offline fleetd recovery command against the
// installed HA application profile. This preserves the generated environment,
// Compose project, and HA overlay selected by fleet-ha.
func ResetSuperAdminPassword(ctx context.Context, passwordInput io.Reader) error {
	config, err := loadNodeConfig(filepath.Join(configRoot, "node.env"))
	if err != nil {
		return err
	}
	if !config.isDatabaseNode() {
		return errors.New("HA password reset must run on ha-a or ha-b")
	}

	args, err := resetSuperAdminPasswordComposeArgsAt(installRoot, installedFleetEnvironment, haGrafanaVolumeOwnershipMarker)
	if err != nil {
		return err
	}
	return runCompose(ctx, args, passwordInput)
}

func resetSuperAdminPasswordComposeArgsAt(root, environmentPath, ownershipMarker string) ([]string, error) {
	command := []string{"--rm", "--no-deps", "-T", "fleet-api", "/app/fleetd", "admin", "reset-password", "--password-stdin"}
	return fleetComposeArgsForInstalledProfileAt(root, environmentPath, ownershipMarker, "run", command...)
}

// RunCompose isolates Compose from parent variables so only installed HA configuration is interpolated.
func RunCompose(ctx context.Context, args []string) error {
	return runCompose(ctx, args, os.Stdin)
}

func runCompose(ctx context.Context, args []string, stdin io.Reader) error {
	environment, external, err := composeEnvironment(args)
	if err != nil {
		return err
	}
	args = endpointComposeArgs(args, external)

	commandArgs := append([]string{"--host", localDockerHost, "compose"}, args...)
	command := exec.CommandContext(ctx, "docker", commandArgs...)
	command.Env = environment
	command.Stdin = stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("run Docker Compose: %w", err)
	}
	return nil
}

func endpointComposeArgs(args []string, external bool) []string {
	if !external {
		return args
	}
	result := make([]string, 0, len(args)+2)
	for i, arg := range args {
		result = append(result, arg)
		var file string
		switch {
		case i > 0 && (args[i-1] == "--file" || args[i-1] == "-f"):
			file = arg
		case strings.HasPrefix(arg, "--file="):
			file = strings.TrimPrefix(arg, "--file=")
		case strings.HasPrefix(arg, "-f"):
			file = strings.TrimPrefix(strings.TrimPrefix(arg, "-f"), "=")
		}
		if filepath.Base(file) == "fleet-compose.yaml" {
			result = append(result, "--file", filepath.Join(filepath.Dir(file), "fleet-compose.external.yaml"))
		}
	}
	return result
}

func composeEnvironment(args []string) ([]string, bool, error) {
	// Docker needs PATH to find its Compose plugin. All Compose interpolation
	// inputs come from the explicit, protected env files below.
	environment := []string{"PATH=" + os.Getenv("PATH")}
	for index := 0; index+1 < len(args); index++ {
		if args[index] != "--env-file" || filepath.Base(args[index+1]) != "node.env" {
			continue
		}
		config, err := loadNodeConfig(args[index+1])
		if err != nil {
			return nil, false, err
		}
		if err := validateNodeConfig(config); err != nil {
			return nil, false, fmt.Errorf("HA node environment rejected: %w", err)
		}
		// The shared tracing overlay requires DD_HOSTNAME during interpolation.
		// External deployments share a monitoring account, so qualify the host
		// identity with the validated public hostname. Keep VIP names unchanged.
		hostname := config.NodeName
		nodeIP := ""
		mode := "vip"
		external := config.externalEndpoint()
		if external {
			publicURL, _ := url.Parse(config.PublicURL) // validated above
			hostname += "." + publicURL.Hostname()
			nodeIP = config.NodeIP
			mode = endpointModeExternal
		}
		environment = append(environment, "DD_HOSTNAME="+hostname, "HA_PUBLIC_URL="+config.publicURL())
		return append(environment, "HA_ENDPOINT_NODE_IP="+nodeIP, "HA_ENDPOINT_MODE="+mode), external, nil
	}
	return environment, false, nil
}
