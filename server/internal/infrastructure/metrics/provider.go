package metrics

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const ServiceName = "protofleet-fleet-api"

type Config struct {
	Enabled    bool          `help:"Enable OpenTelemetry metrics export" default:"false" env:"ENABLED"`
	Endpoint   string        `help:"OTLP gRPC endpoint for metrics" default:"http://otel-collector:4317" env:"EXPORTER_OTLP_ENDPOINT"`
	Interval   time.Duration `help:"Periodic export interval for metrics" default:"15s" env:"EXPORTER_INTERVAL"`
	InstanceID string        `help:"Override for the service.instance.id resource attribute" default:"" env:"INSTANCE_ID"`
}

type Provider struct {
	cfg      Config
	provider *sdkmetric.MeterProvider
	meter    metric.Meter
	resource *resource.Resource
	exporter sdkmetric.Exporter
	shutdown func(context.Context) error
	enabled  bool

	instMu sync.Mutex
	insts  *instruments
}

func Setup(ctx context.Context, version string, cfg Config) (*Provider, error) {
	if !cfg.Enabled {
		return newDisabledProvider(cfg), nil
	}

	res, err := buildResource(ctx, version, cfg.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("build OTel resource: %w", err)
	}

	exporter, err := newOTLPExporter(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create OTLP metric exporter: %w", err)
	}

	interval := cfg.Interval
	if interval <= 0 {
		interval = 15 * time.Second
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(
			exporter,
			sdkmetric.WithInterval(interval),
		)),
	)

	otel.SetMeterProvider(mp)

	p := &Provider{
		cfg:      cfg,
		provider: mp,
		meter:    mp.Meter(ServiceName),
		resource: res,
		exporter: exporter,
		shutdown: mp.Shutdown,
		enabled:  true,
	}

	if err := p.initInstruments(); err != nil {
		_ = mp.Shutdown(ctx)
		return nil, fmt.Errorf("initialise contract instruments: %w", err)
	}

	return p, nil
}

func newDisabledProvider(cfg Config) *Provider {
	mp := noop.NewMeterProvider()
	p := &Provider{
		cfg:      cfg,
		meter:    mp.Meter(ServiceName),
		shutdown: func(context.Context) error { return nil },
		enabled:  false,
	}
	// Errors here are impossible because the noop provider never fails.
	if err := p.initInstruments(); err != nil {
		slog.Error("metrics noop provider failed to init instruments", "error", err)
	}
	return p
}

func (p *Provider) Shutdown(ctx context.Context) error {
	if p == nil || p.shutdown == nil {
		return nil
	}
	return p.shutdown(ctx)
}

func (p *Provider) Enabled() bool {
	return p != nil && p.enabled
}

func (p *Provider) Meter() metric.Meter {
	if p == nil {
		return noop.NewMeterProvider().Meter(ServiceName)
	}
	return p.meter
}

func newOTLPExporter(ctx context.Context, cfg Config) (sdkmetric.Exporter, error) {
	opts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpointURL(cfg.Endpoint),
	}
	res, err := otlpmetricgrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to init OTLP exporter: %w", err)
	}
	return res, nil
}

func buildResource(ctx context.Context, version, instanceID string) (*resource.Resource, error) {
	attrs := []resource.Option{
		resource.WithAttributes(
			semconv.ServiceName(ServiceName),
			semconv.ServiceVersion(version),
		),
		resource.WithProcessPID(),
		resource.WithProcessExecutableName(),
		resource.WithProcessRuntimeName(),
		resource.WithProcessRuntimeVersion(),
		resource.WithOS(),
		resource.WithHost(),
	}
	if instanceID != "" {
		attrs = append(attrs, resource.WithAttributes(
			attribute.String("service.instance.id", instanceID),
		))
	}

	res, err := resource.New(ctx, attrs...)
	if errors.Is(err, resource.ErrPartialResource) {
		slog.Warn("partial OTel resource for metrics", "error", err)
		return res, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to build OTLP resource: %w", err)
	}
	return res, nil
}
