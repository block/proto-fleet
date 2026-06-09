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
	// Prevent unused variable warnings.
	_ = commit
	_ = date

	cmd := &cli.Command{
		Name:    "fleetcli",
		Usage:   "Interact with Fleet gRPC services generated from protobuf definitions",
		Version: version,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "server",
				Usage:   "Fleet server base URL or host; defaults to /api-proxy when no path is provided",
				Sources: cli.EnvVars(envFleetServer),
			},
			&cli.StringFlag{
				Name:    "api-key",
				Usage:   "Fleet API key",
				Sources: cli.EnvVars(envFleetAPIKey),
			},
			&cli.StringFlag{
				Name:    "username",
				Usage:   "Fleet username when no API key is provided",
				Sources: cli.EnvVars(envFleetUsername),
			},
			&cli.StringFlag{
				Name:    "password",
				Usage:   "Fleet password when no API key is provided",
				Sources: cli.EnvVars(envFleetPassword),
			},
			&cli.BoolFlag{
				Name:    "insecure",
				Usage:   "Allow insecure TLS certificates and use http:// when --server has no scheme",
				Sources: cli.EnvVars(envFleetInsecure),
			},
			&cli.BoolFlag{
				Name:    "debug",
				Usage:   "Show debug output",
				Sources: cli.EnvVars("FLEET_DEBUG"),
			},
		},
		Commands:              allCommands(),
		EnableShellCompletion: true,
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			fmt.Fprintf(os.Stderr, "%s returned %s:\n", apiErr.Method, apiErr.Status)
			colorizeJSON(apiErr.Body)
		} else {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		os.Exit(1)
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
					client, _, err := openClient(ctx, cmd)
					if err != nil {
						return err
					}
					defer func() { _ = client.Close() }()

					username, password, err := usernamePassword(cmd)
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
		Usage: "Create, list, and revoke Fleet API keys",
		Commands: []*cli.Command{
			{
				Name:  "create",
				Usage: "Generate a new API key using apikey.v1.ApiKeyService",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "name", Usage: "Human-readable API key name", Required: true},
					&cli.StringFlag{Name: "expires-at", Usage: "Optional expiration timestamp in RFC3339 format"},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					client, opts, err := openClient(ctx, cmd)
					if err != nil {
						return err
					}
					defer func() { _ = client.Close() }()

					if opts.APIKey == "" {
						username, password, err := usernamePassword(cmd)
						if err != nil {
							return fmt.Errorf("create requires either an API key or username/password: %w", err)
						}
						if _, err := client.Authenticate(ctx, username, password); err != nil {
							return err
						}
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
					client, opts, err := openClient(ctx, cmd)
					if err != nil {
						return err
					}
					defer func() { _ = client.Close() }()

					if opts.APIKey == "" {
						username, password, err := usernamePassword(cmd)
						if err != nil {
							return fmt.Errorf("list requires either an API key or username/password: %w", err)
						}
						if _, err := client.Authenticate(ctx, username, password); err != nil {
							return err
						}
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
					client, opts, err := openClient(ctx, cmd)
					if err != nil {
						return err
					}
					defer func() { _ = client.Close() }()

					if opts.APIKey == "" {
						username, password, err := usernamePassword(cmd)
						if err != nil {
							return fmt.Errorf("revoke requires either an API key or username/password: %w", err)
						}
						if _, err := client.Authenticate(ctx, username, password); err != nil {
							return err
						}
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
					&cli.IntFlag{Name: "page-size", Usage: "Maximum number of metric rows to request", Value: 500},
					&cli.StringSliceFlag{Name: "metric", Usage: "Metric types to request", Value: []string{"hashrate", "efficiency", "power", "temperature", "uptime"}},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					client, _, err := openClient(ctx, cmd)
					if err != nil {
						return err
					}
					defer func() { _ = client.Close() }()

					resp, err := client.GetCombinedMetrics(ctx, buildCombinedMetricsRequest(cmd))
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
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
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

func buildCombinedMetricsRequest(cmd *cli.Command) *telemetryv1.GetCombinedMetricsRequest {
	end := time.Now().UTC()
	start := end.Add(-cmd.Duration("window"))

	return &telemetryv1.GetCombinedMetricsRequest{
		DeviceSelector: &telemetryv1.DeviceSelector{
			SelectorValue: &telemetryv1.DeviceSelector_AllDevices{AllDevices: true},
		},
		MeasurementTypes: parseMeasurementTypes(cmd.StringSlice("metric")),
		Aggregations: []telemetryv1.AggregationType{
			telemetryv1.AggregationType_AGGREGATION_TYPE_AVERAGE,
			telemetryv1.AggregationType_AGGREGATION_TYPE_MIN,
			telemetryv1.AggregationType_AGGREGATION_TYPE_MAX,
			telemetryv1.AggregationType_AGGREGATION_TYPE_MEDIAN,
		},
		Granularity: durationpb.New(cmd.Duration("granularity")),
		StartTime:   timestamppb.New(start),
		EndTime:     timestamppb.New(end),
		PageSize:    int32(cmd.Int("page-size")),
	}
}

func parseMeasurementTypes(values []string) []telemetryv1.MeasurementType {
	if len(values) == 0 {
		return nil
	}

	result := make([]telemetryv1.MeasurementType, 0, len(values))
	for _, value := range values {
		switch value {
		case "temperature":
			result = append(result, telemetryv1.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE)
		case "hashrate":
			result = append(result, telemetryv1.MeasurementType_MEASUREMENT_TYPE_HASHRATE)
		case "power":
			result = append(result, telemetryv1.MeasurementType_MEASUREMENT_TYPE_POWER)
		case "efficiency":
			result = append(result, telemetryv1.MeasurementType_MEASUREMENT_TYPE_EFFICIENCY)
		case "fan_speed":
			result = append(result, telemetryv1.MeasurementType_MEASUREMENT_TYPE_FAN_SPEED)
		case "voltage":
			result = append(result, telemetryv1.MeasurementType_MEASUREMENT_TYPE_VOLTAGE)
		case "current":
			result = append(result, telemetryv1.MeasurementType_MEASUREMENT_TYPE_CURRENT)
		case "uptime":
			result = append(result, telemetryv1.MeasurementType_MEASUREMENT_TYPE_UPTIME)
		case "error_rate":
			result = append(result, telemetryv1.MeasurementType_MEASUREMENT_TYPE_ERROR_RATE)
		}
	}
	return result
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
	keys := make([]string, 0, len(latest))
	for measurementType, metric := range latest {
		name := compactMeasurementName(measurementType)
		keys = append(keys, name)
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
	sort.Strings(keys)
	orderedLatest := make(map[string]metricSummary, len(keys))
	for _, key := range keys {
		orderedLatest[key] = latestMetrics[key]
	}

	var latestTemperature *temperatureSummary
	if counts := newestTemperatureStatus(resp.GetTemperatureStatusCounts()); counts != nil {
		latestTemperature = &temperatureSummary{
			Timestamp: counts.GetTimestamp().AsTime().UTC().Format(time.RFC3339),
			Cold:      counts.GetColdCount(),
			OK:        counts.GetOkCount(),
			Hot:       counts.GetHotCount(),
			Critical:  counts.GetCriticalCount(),
		}
	}

	var latestUptime *uptimeSummary
	if counts := newestUptimeStatus(resp.GetUptimeStatusCounts()); counts != nil {
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
		RequestedMetrics:  cmd.StringSlice("metric"),
		ReturnedRows:      len(resp.GetMetrics()),
		NextPageToken:     resp.GetNextPageToken(),
		Latest:            orderedLatest,
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

func newestTemperatureStatus(values []*telemetryv1.TemperatureStatusCount) *telemetryv1.TemperatureStatusCount {
	var latest *telemetryv1.TemperatureStatusCount
	for _, value := range values {
		if latest == nil || value.GetTimestamp().AsTime().After(latest.GetTimestamp().AsTime()) {
			latest = value
		}
	}
	return latest
}

func newestUptimeStatus(values []*telemetryv1.UptimeStatusCount) *telemetryv1.UptimeStatusCount {
	var latest *telemetryv1.UptimeStatusCount
	for _, value := range values {
		if latest == nil || value.GetTimestamp().AsTime().After(latest.GetTimestamp().AsTime()) {
			latest = value
		}
	}
	return latest
}

func compactMeasurementName(value telemetryv1.MeasurementType) string {
	switch value {
	case telemetryv1.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE:
		return "temperature"
	case telemetryv1.MeasurementType_MEASUREMENT_TYPE_HASHRATE:
		return "hashrate"
	case telemetryv1.MeasurementType_MEASUREMENT_TYPE_POWER:
		return "power"
	case telemetryv1.MeasurementType_MEASUREMENT_TYPE_EFFICIENCY:
		return "efficiency"
	case telemetryv1.MeasurementType_MEASUREMENT_TYPE_FAN_SPEED:
		return "fan_speed"
	case telemetryv1.MeasurementType_MEASUREMENT_TYPE_VOLTAGE:
		return "voltage"
	case telemetryv1.MeasurementType_MEASUREMENT_TYPE_CURRENT:
		return "current"
	case telemetryv1.MeasurementType_MEASUREMENT_TYPE_UPTIME:
		return "uptime"
	case telemetryv1.MeasurementType_MEASUREMENT_TYPE_ERROR_RATE:
		return "error_rate"
	default:
		return value.String()
	}
}

func openClient(ctx context.Context, cmd *cli.Command) (*Client, Options, error) {
	opts, err := resolvedClientOptions(cmd)
	if err != nil {
		return nil, Options{}, err
	}
	client, err := New(ctx, opts)
	if err != nil {
		return nil, Options{}, err
	}
	return client, opts, nil
}

func usernamePassword(cmd *cli.Command) (string, string, error) {
	username := cmd.String("username")
	password := cmd.String("password")
	if username == "" || password == "" {
		return "", "", fmt.Errorf("username and password must both be provided")
	}
	return username, password, nil
}

func colorizeJSON(data []byte) {
	formatter := prettyjson.NewFormatter()
	formatter.Indent = 2
	formatter.KeyColor = color.New(color.FgBlue)
	formatter.StringColor = color.New(color.FgGreen)
	formatter.BoolColor = color.New(color.FgYellow)
	formatter.NumberColor = color.New(color.FgCyan)
	formatter.NullColor = color.New(color.FgHiBlack)

	colorized, err := formatter.Format(data)
	if err != nil {
		fmt.Println(string(data))
		return
	}
	fmt.Println(string(colorized))
}

func printProto(message proto.Message) error {
	output, err := protojson.MarshalOptions{
		Multiline:       true,
		Indent:          "  ",
		UseProtoNames:   true,
		EmitUnpopulated: true,
	}.Marshal(message)
	if err != nil {
		return err
	}
	colorizeJSON(output)
	return nil
}

func printJSON(value any) error {
	output, err := json.Marshal(value)
	if err != nil {
		return err
	}
	colorizeJSON(output)
	return nil
}

func resolvedClientOptions(cmd *cli.Command) (Options, error) {
	apiKey, username, password, err := resolvedAuthInputs(cmd)
	if err != nil {
		return Options{}, err
	}
	server := cmd.String("server")
	if server == "" {
		server = defaultFleetServer
	}
	return Options{
		Server:   server,
		APIKey:   apiKey,
		Username: username,
		Password: password,
		Insecure: cmd.Bool("insecure"),
		Debug:    cmd.Bool("debug"),
	}, nil
}

func resolvedAuthInputs(cmd *cli.Command) (string, string, string, error) {
	if value, ok := explicitStringFlag(cmd, "api-key"); ok {
		return value, "", "", nil
	}

	if flagProvidedOnCLI("username") || flagProvidedOnCLI("password") {
		return "", cmd.String("username"), cmd.String("password"), nil
	}

	if value := cmd.String("api-key"); value != "" {
		return value, "", "", nil
	}

	username := cmd.String("username")
	password := cmd.String("password")
	if username != "" || password != "" {
		return "", username, password, nil
	}

	return "", "", "", nil
}

func explicitStringFlag(cmd *cli.Command, name string) (string, bool) {
	if !flagProvidedOnCLI(name) {
		return "", false
	}
	return cmd.String(name), true
}

func flagProvidedOnCLI(name string) bool {
	prefix := "--" + name
	for _, arg := range os.Args[1:] {
		if arg == prefix || strings.HasPrefix(arg, prefix+"=") {
			return true
		}
	}
	return false
}
