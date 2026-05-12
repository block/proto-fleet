package metrics

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// instruments holds the cached OTel instruments for every metric in the contract.
type instruments struct {
	// Per-device gauges.
	deviceOnline                 metric.Int64Gauge
	deviceHashrateTerahash       metric.Float64Gauge
	deviceHashrateExpectedTerahz metric.Float64Gauge
	deviceTemperatureMax         metric.Float64Gauge
	deviceTemperatureAvg         metric.Float64Gauge
	devicePoolConnected          metric.Int64Gauge

	// Counters.
	commandTotal       metric.Int64Counter
	telemetryPollTotal metric.Int64Counter
}

// initInstruments creates every contract instrument on the meter and stores them on p.
func (p *Provider) initInstruments() error {
	p.instMu.Lock()
	defer p.instMu.Unlock()

	if p.insts != nil {
		return nil
	}

	insts := &instruments{}
	var err error

	insts.deviceOnline, err = p.meter.Int64Gauge(
		MetricDeviceOnline,
		metric.WithDescription("1 when the device is reachable and reporting telemetry, 0 when unreachable"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("create %s gauge: %w", MetricDeviceOnline, err)
	}

	insts.deviceHashrateTerahash, err = p.meter.Float64Gauge(
		MetricDeviceHashrateTerahash,
		metric.WithDescription("Observed hashrate of the device in TH/s"),
		metric.WithUnit("Th/s"),
	)
	if err != nil {
		return fmt.Errorf("create %s gauge: %w", MetricDeviceHashrateTerahash, err)
	}

	insts.deviceHashrateExpectedTerahz, err = p.meter.Float64Gauge(
		MetricDeviceHashrateExpectedTerahash,
		metric.WithDescription("Expected (nameplate) hashrate of the device in TH/s"),
		metric.WithUnit("Th/s"),
	)
	if err != nil {
		return fmt.Errorf("create %s gauge: %w", MetricDeviceHashrateExpectedTerahash, err)
	}

	insts.deviceTemperatureMax, err = p.meter.Float64Gauge(
		MetricDeviceTemperatureMaxCelsius,
		metric.WithDescription("Maximum temperature observed across the device's sensors of a given kind"),
		metric.WithUnit("Cel"),
	)
	if err != nil {
		return fmt.Errorf("create %s gauge: %w", MetricDeviceTemperatureMaxCelsius, err)
	}

	insts.deviceTemperatureAvg, err = p.meter.Float64Gauge(
		MetricDeviceTemperatureAvgCelsius,
		metric.WithDescription("Average temperature across the device's sensors of a given kind"),
		metric.WithUnit("Cel"),
	)
	if err != nil {
		return fmt.Errorf("create %s gauge: %w", MetricDeviceTemperatureAvgCelsius, err)
	}

	insts.devicePoolConnected, err = p.meter.Int64Gauge(
		MetricDevicePoolConnected,
		metric.WithDescription("1 when the device is connected to its primary mining pool, 0 otherwise"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("create %s gauge: %w", MetricDevicePoolConnected, err)
	}

	insts.commandTotal, err = p.meter.Int64Counter(
		MetricCommandTotal,
		metric.WithDescription("Total number of dispatched commands by kind and terminal result"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("create %s counter: %w", MetricCommandTotal, err)
	}

	insts.telemetryPollTotal, err = p.meter.Int64Counter(
		MetricTelemetryPollTotal,
		metric.WithDescription("Total number of telemetry poll attempts by terminal result"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("create %s counter: %w", MetricTelemetryPollTotal, err)
	}

	p.insts = insts
	return nil
}
