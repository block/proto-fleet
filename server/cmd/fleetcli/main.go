package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	apikeyv1 "github.com/block/proto-fleet/server/generated/grpc/apikey/v1"
	telemetryv1 "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/fatih/color"
	"github.com/hokaccha/go-prettyjson"
	"github.com/urfave/cli/v3"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// #nosec G101 -- environment variable names, not credentials.
const (
	defaultFleetServer = "https://localhost/api-proxy"
	envFleetServer     = "FLEET_SERVER"
	envFleetAPIKey     = "FLEET_API_KEY"
	envFleetUsername   = "FLEET_USERNAME"
	envFleetPassword   = "FLEET_PASSWORD"
	envFleetInsecure   = "FLEET_INSECURE"
)

// Version information that will be set by GoReleaser.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := newRootCommand().Run(context.Background(), os.Args); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			writeAPIError(os.Stderr, apiErr)
		} else {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		os.Exit(1)
	}
}

func newRootCommand() *cli.Command {
	return &cli.Command{
		Name:    "fleetcli",
		Usage:   "Interact with Fleet gRPC services generated from protobuf definitions",
		Version: version,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "server",
				Usage:   "Fleet server base URL or host; defaults to /api-proxy when no path is provided, use a trailing slash to target the RPC root directly",
				Sources: cli.EnvVars(envFleetServer),
			},
			&cli.StringFlag{
				Name:    "api-key",
				Usage:   "Fleet API key; preferred over username/password for authenticated commands when both are set",
				Sources: cli.EnvVars(envFleetAPIKey),
			},
			&cli.StringFlag{
				Name:    "username",
				Usage:   "Fleet username for commands that need a session",
				Sources: cli.EnvVars(envFleetUsername),
			},
			&cli.BoolFlag{
				Name:  "password-stdin",
				Usage: "Read Fleet password for commands that need a session from stdin",
			},
			&cli.BoolFlag{
				Name:    "insecure",
				Usage:   "Allow insecure TLS certificates and non-loopback http:// server URLs for testing",
				Sources: cli.EnvVars(envFleetInsecure),
			},
		},
		Commands:              allCommands(),
		EnableShellCompletion: true,
	}
}

func authCommand() *cli.Command {
	return &cli.Command{
		Name:  "auth",
		Usage: "Authenticate against Fleet using a username and password",
		Commands: []*cli.Command{
			{
				Name:  "login",
				Usage: "Validate session credentials against the auth service",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					client, err := openClient(ctx, cmd)
					if err != nil {
						return err
					}
					defer func() { _ = client.Close() }()

					username, password, err := client.sessionCredentials()
					if err != nil {
						return err
					}
					resp, err := client.Authenticate(ctx, username, password)
					if err != nil {
						return err
					}
					return printProto(resp)
				},
			},
		},
	}
}

func apiKeyCommand() *cli.Command {
	return &cli.Command{
		Name:  "apikey",
		Usage: "Create, list, and revoke Fleet API keys using session credentials",
		Commands: []*cli.Command{
			{
				Name:  "create",
				Usage: "Generate a new API key using apikey.v1.ApiKeyService",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "name", Usage: "Human-readable API key name", Required: true},
					&cli.StringFlag{Name: "expires-at", Usage: "Optional expiration timestamp in RFC3339 format"},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					client, err := openClient(ctx, cmd)
					if err != nil {
						return err
					}
					defer func() { _ = client.Close() }()

					if err := authenticateAPIKeySession(ctx, client, "create"); err != nil {
						return err
					}

					req := &apikeyv1.CreateApiKeyRequest{Name: cmd.String("name")}
					if expiresAt := cmd.String("expires-at"); expiresAt != "" {
						parsed, err := time.Parse(time.RFC3339, expiresAt)
						if err != nil {
							return fmt.Errorf("invalid expires-at value: %w", err)
						}
						req.ExpiresAt = timestamppb.New(parsed)
					}

					resp, err := client.CreateAPIKey(ctx, req)
					if err != nil {
						return err
					}
					return printProto(resp)
				},
			},
			{
				Name:  "list",
				Usage: "List active API keys",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					client, err := openClient(ctx, cmd)
					if err != nil {
						return err
					}
					defer func() { _ = client.Close() }()

					if err := authenticateAPIKeySession(ctx, client, "list"); err != nil {
						return err
					}

					resp, err := client.ListAPIKeys(ctx)
					if err != nil {
						return err
					}
					return printProto(resp)
				},
			},
			{
				Name:  "revoke",
				Usage: "Revoke an API key by key id",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "key-id", Usage: "API key id to revoke", Required: true},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					client, err := openClient(ctx, cmd)
					if err != nil {
						return err
					}
					defer func() { _ = client.Close() }()

					if err := authenticateAPIKeySession(ctx, client, "revoke"); err != nil {
						return err
					}

					resp, err := client.RevokeAPIKey(ctx, cmd.String("key-id"))
					if err != nil {
						return err
					}
					return printProto(resp)
				},
			},
		},
	}
}

func authenticateAPIKeySession(ctx context.Context, client *Client, action string) error {
	username, password, err := client.sessionCredentials()
	if err != nil {
		return fmt.Errorf("%s requires username and password because API key lifecycle commands are session-only: %w", action, err)
	}
	if _, err := client.Authenticate(ctx, username, password); err != nil {
		return err
	}
	return nil
}

// defaultPerformanceMetrics are the metric types `performance get` requests
// when no --metric flags are provided.
var defaultPerformanceMetrics = []string{"hashrate", "efficiency", "power", "temperature", "uptime"}

// measurementTypeByMetric maps CLI metric names to telemetry measurement
// types; compactMeasurementName reverses it for response summaries.
var measurementTypeByMetric = map[string]telemetryv1.MeasurementType{
	"temperature": telemetryv1.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE,
	"hashrate":    telemetryv1.MeasurementType_MEASUREMENT_TYPE_HASHRATE,
	"power":       telemetryv1.MeasurementType_MEASUREMENT_TYPE_POWER,
	"efficiency":  telemetryv1.MeasurementType_MEASUREMENT_TYPE_EFFICIENCY,
	"fan_speed":   telemetryv1.MeasurementType_MEASUREMENT_TYPE_FAN_SPEED,
	"voltage":     telemetryv1.MeasurementType_MEASUREMENT_TYPE_VOLTAGE,
	"current":     telemetryv1.MeasurementType_MEASUREMENT_TYPE_CURRENT,
	"uptime":      telemetryv1.MeasurementType_MEASUREMENT_TYPE_UPTIME,
	"error_rate":  telemetryv1.MeasurementType_MEASUREMENT_TYPE_ERROR_RATE,
}

func performanceCommand() *cli.Command {
	return &cli.Command{
		Name:  "performance",
		Usage: "Read fleet performance metrics",
		Commands: []*cli.Command{
			{
				Name:  "get",
				Usage: "Fetch performance metrics using the active telemetry service",
				Flags: []cli.Flag{
					&cli.DurationFlag{Name: "window", Usage: "Lookback window for historical metrics", Value: time.Hour},
					&cli.DurationFlag{Name: "granularity", Usage: "Bucket size for aggregated metrics", Value: 30 * time.Second},
					&cli.Int32Flag{Name: "page-size", Usage: "Maximum number of metric rows to request", Value: 500},
					&cli.StringFlag{Name: "page-token", Usage: "Pagination token from a previous response"},
					&cli.StringSliceFlag{Name: "metric", Usage: "Metric types to request"},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					req, err := buildCombinedMetricsRequest(cmd)
					if err != nil {
						return err
					}

					client, err := openClient(ctx, cmd)
					if err != nil {
						return err
					}
					defer func() { _ = client.Close() }()

					resp, err := client.GetCombinedMetrics(ctx, req)
					if err != nil {
						return err
					}
					return printJSON(summarizePerformance(cmd, resp))
				},
			},
		},
	}
}

func readProtoJSON(path string, msg proto.Message) error {
	data, err := readInput(path)
	if err != nil {
		return err
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(data, msg); err != nil {
		return fmt.Errorf("failed to parse %s: %w", path, err)
	}
	return nil
}

func readInput(path string) ([]byte, error) {
	if path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}

func parseInt64Slice(values []string) ([]int64, error) {
	result := make([]int64, 0, len(values))
	for _, value := range values {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid integer %q: %w", value, err)
		}
		result = append(result, parsed)
	}
	return result, nil
}

func normalizeEnum(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", "_"))
}

func buildCombinedMetricsRequest(cmd *cli.Command) (*telemetryv1.GetCombinedMetricsRequest, error) {
	end := time.Now().UTC()
	start := end.Add(-cmd.Duration("window"))
	metrics := requestedPerformanceMetrics(cmd)
	measurementTypes, err := parseMeasurementTypes(metrics)
	if err != nil {
		return nil, err
	}

	return &telemetryv1.GetCombinedMetricsRequest{
		DeviceSelector: &telemetryv1.DeviceSelector{
			SelectorValue: &telemetryv1.DeviceSelector_AllDevices{AllDevices: true},
		},
		MeasurementTypes: measurementTypes,
		Aggregations: []telemetryv1.AggregationType{
			telemetryv1.AggregationType_AGGREGATION_TYPE_AVERAGE,
			telemetryv1.AggregationType_AGGREGATION_TYPE_MIN,
			telemetryv1.AggregationType_AGGREGATION_TYPE_MAX,
			telemetryv1.AggregationType_AGGREGATION_TYPE_MEDIAN,
		},
		Granularity: durationpb.New(cmd.Duration("granularity")),
		StartTime:   timestamppb.New(start),
		EndTime:     timestamppb.New(end),
		PageSize:    cmd.Int32("page-size"),
		PageToken:   cmd.String("page-token"),
	}, nil
}

func parseMeasurementTypes(values []string) ([]telemetryv1.MeasurementType, error) {
	if len(values) == 0 {
		return nil, nil
	}

	result := make([]telemetryv1.MeasurementType, 0, len(values))
	unknown := make([]string, 0)
	for _, value := range values {
		if measurementType, ok := measurementTypeByMetric[normalizeEnum(value)]; ok {
			result = append(result, measurementType)
			continue
		}
		unknown = append(unknown, value)
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("invalid value for metric: %s. Valid options: %s", strings.Join(unknown, ", "), strings.Join(supportedMetricNames(), ", "))
	}
	return result, nil
}

func requestedPerformanceMetrics(cmd *cli.Command) []string {
	if cmd.IsSet("metric") {
		return cmd.StringSlice("metric")
	}
	return defaultPerformanceMetrics
}

func supportedMetricNames() []string {
	names := make([]string, 0, len(measurementTypeByMetric))
	for name := range measurementTypeByMetric {
		names = append(names, strings.ReplaceAll(name, "_", "-"))
	}
	sort.Strings(names)
	return names
}

func summarizePerformance(cmd *cli.Command, resp *telemetryv1.GetCombinedMetricsResponse) any {
	type metricSummary struct {
		Timestamp   string   `json:"timestamp"`
		Average     *float64 `json:"average,omitempty"`
		Minimum     *float64 `json:"minimum,omitempty"`
		Maximum     *float64 `json:"maximum,omitempty"`
		Median      *float64 `json:"median,omitempty"`
		DeviceCount int32    `json:"device_count"`
	}
	type temperatureSummary struct {
		Timestamp string `json:"timestamp"`
		Cold      int32  `json:"cold"`
		OK        int32  `json:"ok"`
		Hot       int32  `json:"hot"`
		Critical  int32  `json:"critical"`
	}
	type uptimeSummary struct {
		Timestamp  string `json:"timestamp"`
		Hashing    int32  `json:"hashing"`
		NotHashing int32  `json:"not_hashing"`
	}
	type performanceSummary struct {
		Source            string                   `json:"source"`
		Window            string                   `json:"window"`
		Granularity       string                   `json:"granularity"`
		RequestedMetrics  []string                 `json:"requested_metrics"`
		ReturnedRows      int                      `json:"returned_rows"`
		NextPageToken     string                   `json:"next_page_token,omitempty"`
		Latest            map[string]metricSummary `json:"latest"`
		LatestTemperature *temperatureSummary      `json:"latest_temperature_status,omitempty"`
		LatestUptime      *uptimeSummary           `json:"latest_uptime_status,omitempty"`
	}

	latest := make(map[telemetryv1.MeasurementType]*telemetryv1.Metric)
	for _, metric := range resp.GetMetrics() {
		current := latest[metric.GetMeasurementType()]
		if current == nil || metric.GetOpenTime().AsTime().After(current.GetOpenTime().AsTime()) {
			latest[metric.GetMeasurementType()] = metric
		}
	}

	latestMetrics := make(map[string]metricSummary, len(latest))
	for measurementType, metric := range latest {
		name := compactMeasurementName(measurementType)
		summary := metricSummary{
			Timestamp:   metric.GetOpenTime().AsTime().UTC().Format(time.RFC3339),
			DeviceCount: metric.GetDeviceCount(),
		}
		if value, ok := aggregatedValue(metric.GetAggregatedValues(), telemetryv1.AggregationType_AGGREGATION_TYPE_AVERAGE); ok {
			summary.Average = float64Ptr(value)
		}
		if value, ok := aggregatedValue(metric.GetAggregatedValues(), telemetryv1.AggregationType_AGGREGATION_TYPE_MIN); ok {
			summary.Minimum = float64Ptr(value)
		}
		if value, ok := aggregatedValue(metric.GetAggregatedValues(), telemetryv1.AggregationType_AGGREGATION_TYPE_MAX); ok {
			summary.Maximum = float64Ptr(value)
		}
		if value, ok := aggregatedValue(metric.GetAggregatedValues(), telemetryv1.AggregationType_AGGREGATION_TYPE_MEDIAN); ok {
			summary.Median = float64Ptr(value)
		}
		latestMetrics[name] = summary
	}

	var latestTemperature *temperatureSummary
	if counts := newestByTimestamp(resp.GetTemperatureStatusCounts(), func(c *telemetryv1.TemperatureStatusCount) time.Time {
		return c.GetTimestamp().AsTime()
	}); counts != nil {
		latestTemperature = &temperatureSummary{
			Timestamp: counts.GetTimestamp().AsTime().UTC().Format(time.RFC3339),
			Cold:      counts.GetColdCount(),
			OK:        counts.GetOkCount(),
			Hot:       counts.GetHotCount(),
			Critical:  counts.GetCriticalCount(),
		}
	}

	var latestUptime *uptimeSummary
	if counts := newestByTimestamp(resp.GetUptimeStatusCounts(), func(c *telemetryv1.UptimeStatusCount) time.Time {
		return c.GetTimestamp().AsTime()
	}); counts != nil {
		latestUptime = &uptimeSummary{
			Timestamp:  counts.GetTimestamp().AsTime().UTC().Format(time.RFC3339),
			Hashing:    counts.GetHashingCount(),
			NotHashing: counts.GetNotHashingCount(),
		}
	}

	return performanceSummary{
		Source:            "telemetry.v1.TelemetryService/GetCombinedMetrics",
		Window:            cmd.Duration("window").String(),
		Granularity:       cmd.Duration("granularity").String(),
		RequestedMetrics:  requestedPerformanceMetrics(cmd),
		ReturnedRows:      len(resp.GetMetrics()),
		NextPageToken:     resp.GetNextPageToken(),
		Latest:            latestMetrics,
		LatestTemperature: latestTemperature,
		LatestUptime:      latestUptime,
	}
}

func aggregatedValue(values []*telemetryv1.AggregatedValue, aggregation telemetryv1.AggregationType) (float64, bool) {
	for _, value := range values {
		if value.GetAggregationType() == aggregation {
			return value.GetValue(), true
		}
	}
	return 0, false
}

func float64Ptr(value float64) *float64 {
	return &value
}

// newestByTimestamp returns the element with the latest timestamp, or nil when
// values is empty.
func newestByTimestamp[T any](values []*T, timestamp func(*T) time.Time) *T {
	var latest *T
	for _, value := range values {
		if latest == nil || timestamp(value).After(timestamp(latest)) {
			latest = value
		}
	}
	return latest
}

func compactMeasurementName(value telemetryv1.MeasurementType) string {
	for name, measurementType := range measurementTypeByMetric {
		if measurementType == value {
			return name
		}
	}
	return value.String()
}

func openClient(ctx context.Context, cmd *cli.Command) (*Client, error) {
	return New(ctx, resolvedClientOptions(cmd))
}

func writeAPIError(w io.Writer, apiErr *APIError) {
	_, _ = fmt.Fprintf(w, "%s returned %s:\n", apiErr.Method, apiErr.Status)
	colorizeJSONTo(w, apiErr.Body)
}

func colorizeJSONTo(w io.Writer, data []byte) {
	formatter := prettyjson.NewFormatter()
	formatter.Indent = 2
	formatter.KeyColor = color.New(color.FgBlue)
	formatter.StringColor = color.New(color.FgGreen)
	formatter.BoolColor = color.New(color.FgYellow)
	formatter.NumberColor = color.New(color.FgCyan)
	formatter.NullColor = color.New(color.FgHiBlack)

	colorized, err := formatter.Format(data)
	if err != nil {
		_, _ = fmt.Fprintln(w, string(data))
		return
	}
	_, _ = fmt.Fprintln(w, string(colorized))
}

func printProto(message proto.Message) error {
	output, err := protojson.MarshalOptions{
		Multiline:       true,
		Indent:          "  ",
		UseProtoNames:   true,
		EmitUnpopulated: true,
	}.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	colorizeJSONTo(os.Stdout, output)
	return nil
}

func printJSON(value any) error {
	output, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal output: %w", err)
	}
	colorizeJSONTo(os.Stdout, output)
	return nil
}

// resolvedClientOptions reads the global connection and auth flags from the
// root command, so subcommand-local flags never shadow them.
func resolvedClientOptions(cmd *cli.Command) Options {
	apiKey, username, password := resolvedAuthInputs(cmd)
	root := cmd.Root()
	server := root.String("server")
	if server == "" {
		server = defaultFleetServer
	}
	return Options{
		Server:        server,
		APIKey:        apiKey,
		Username:      username,
		Password:      password,
		PasswordStdin: root.Bool("password-stdin"),
		Insecure:      root.Bool("insecure"),
	}
}

// resolvedAuthInputs reads the global auth flags from the root command, so
// subcommand-local flags that happen to be named "username" or "password"
// (e.g. pool credentials) never leak into Fleet auth. All three values pass
// through together: authenticated commands prefer the API key, session-only
// commands use username/password, and the Fleet password comes only from
// FLEET_PASSWORD, --password-stdin, or a prompt.
func resolvedAuthInputs(cmd *cli.Command) (string, string, string) {
	root := cmd.Root()
	return root.String("api-key"), root.String("username"), os.Getenv(envFleetPassword)
}
