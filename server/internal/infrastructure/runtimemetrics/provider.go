// Package runtimemetrics exposes vendor-neutral operational metrics independently
// of the database-backed customer alert metrics and trace sampling.
package runtimemetrics

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

type Config struct {
	Enabled bool   `help:"Expose operational metrics on a private Prometheus endpoint" default:"false" env:"ENABLED"`
	Address string `help:"Loopback metrics listen address" default:"127.0.0.1:9090" env:"ADDRESS"`
}

type Provider struct {
	meter    metric.Meter
	provider *sdkmetric.MeterProvider
	server   *http.Server
	address  string
}

// Start binds synchronously so a bad address cannot silently disable collection.
// The endpoint is loopback-only; the host's collector owns remote delivery.
func Start(cfg Config) (*Provider, error) {
	p := &Provider{meter: noop.NewMeterProvider().Meter("fleet.runtime")}
	if !cfg.Enabled {
		return p, nil
	}
	addr, err := netip.ParseAddrPort(cfg.Address)
	if err != nil || !addr.Addr().IsLoopback() {
		return nil, fmt.Errorf("runtime metrics address must be a loopback IP and port: %q", cfg.Address)
	}
	registry := prometheus.NewRegistry()
	exporter, err := otelprom.New(otelprom.WithRegisterer(registry), otelprom.WithoutTargetInfo(), otelprom.WithoutScopeInfo())
	if err != nil {
		return nil, fmt.Errorf("create metrics exporter: %w", err)
	}
	p.provider = sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	p.meter = p.provider.Meter("fleet.runtime")
	started, err := p.meter.Float64Gauge("fleet_runtime_start_time_seconds")
	if err != nil {
		_ = p.provider.Shutdown(context.Background())
		return nil, fmt.Errorf("create runtime start time gauge: %w", err)
	}
	started.Record(context.Background(), float64(time.Now().Unix()))
	listener, err := net.Listen("tcp", cfg.Address)
	if err != nil {
		_ = p.provider.Shutdown(context.Background())
		return nil, fmt.Errorf("listen for runtime metrics: %w", err)
	}
	p.address = listener.Addr().String()
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{Timeout: 5 * time.Second, MaxRequestsInFlight: 2}))
	p.server = &http.Server{Handler: mux, ReadHeaderTimeout: 3 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	go func() {
		if err := p.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("runtime metrics listener failed", "error", err)
		}
	}()
	return p, nil
}

func (p *Provider) Meter() metric.Meter { return p.meter }

// Addr returns the bound metrics address, or an empty string when disabled.
func (p *Provider) Addr() string { return p.address }

func (p *Provider) Shutdown(ctx context.Context) error {
	if p.server == nil {
		return nil
	}
	return errors.Join(p.server.Shutdown(ctx), p.provider.Shutdown(ctx))
}
