package fleet_telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

type Config struct {
	Enabled     bool    `help:"Enable OpenTelemetry metrics" default:"false" env:"ENABLED"`
	Endpoint    string  `help:"OTLP HTTP endpoint for traces (e.g. http://localhost:4318)" default:"http://localhost:4318" env:"ENDPOINT"`
	ServiceName string  `help:"Service name reported in traces" default:"fleetd" env:"SERVICE_NAME"`
	SampleRate  float64 `help:"Fraction of traces to sample (0.0–1.0)" default:"1.0" env:"SAMPLE_RATE"`
}

// Setup initialises a global TracerProvider and returns a shutdown function.
// If telemetry is disabled the returned shutdown function is a no-op.
func Setup(ctx context.Context, version string, cfg Config) (func(context.Context) error, error) {
	if !cfg.Enabled {
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(cfg.Endpoint))
	if err != nil {
		return nil, fmt.Errorf("create OTLP HTTP exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(version),
		),
		resource.WithProcessPID(),
		resource.WithProcessExecutableName(),
		resource.WithProcessRuntimeName(),
		resource.WithProcessRuntimeVersion(),
		resource.WithProcessRuntimeDescription(),
		resource.WithOS(),
	)
	if errors.Is(err, resource.ErrPartialResource) {
		slog.Warn("Could not gather resources in OTel creation.", "error", err)
	} else if err != nil {
		return nil, fmt.Errorf("create OTel resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRate))),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}
