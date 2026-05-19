package metrics

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// sampleKind tags a row with the destination table.
type sampleKind uint8

const (
	sampleKindDevice sampleKind = iota + 1
	sampleKindTemperature
	sampleKindCommand
	sampleKindTelemetryPoll
)

// sample is one row pending insert. Each field is only meaningful for a
// subset of kinds; the writer reads the kind and dispatches accordingly.
type sample struct {
	kind sampleKind
	time time.Time

	// device + temperature
	orgID       int64
	deviceID    string
	deviceGroup string
	driver      string

	// device gauges (sampleKindDevice)
	online              *bool
	hashrateTHs         *float64
	hashrateExpectedTHs *float64
	poolConnected       *bool

	// temperature (sampleKindTemperature)
	sensorKind      string
	temperatureMaxC float64
	temperatureAvgC float64
	temperatureHasV bool // true when temperatureMaxC/AvgC are populated

	// command / telemetry counters
	commandKind string
	result      string
}

// writeBatch fans batch out into one INSERT per destination table. We group
// by kind first so each statement sees a homogenous shape; this trades a
// little memory for the simplicity of a single placeholder template per
// table.
func (p *Provider) writeBatch(ctx context.Context, batch []sample) error {
	if len(batch) == 0 {
		return nil
	}

	var device, temperature, command, telemetry []sample
	for _, s := range batch {
		switch s.kind {
		case sampleKindDevice:
			device = append(device, s)
		case sampleKindTemperature:
			temperature = append(temperature, s)
		case sampleKindCommand:
			command = append(command, s)
		case sampleKindTelemetryPoll:
			telemetry = append(telemetry, s)
		}
	}

	if err := p.insertDevice(ctx, device); err != nil {
		return err
	}
	if err := p.insertTemperature(ctx, temperature); err != nil {
		return err
	}
	if err := p.insertCommand(ctx, command); err != nil {
		return err
	}
	if err := p.insertTelemetry(ctx, telemetry); err != nil {
		return err
	}
	return nil
}

// columns per table — kept here so each INSERT statement reflects exactly the
// contract's surface and migrations 000050_create_notification_metrics.up.sql.

const deviceCols = "(time, organization_id, device_id, device_group, driver, online, hashrate_ths, hashrate_expected_ths, pool_connected)"
const deviceColCount = 9

const temperatureCols = "(time, organization_id, device_id, device_group, driver, sensor_kind, temperature_max_c, temperature_avg_c)"
const temperatureColCount = 8

const commandCols = "(time, organization_id, kind, result)"
const commandColCount = 4

const telemetryCols = "(time, organization_id, device_id, result)"
const telemetryColCount = 4

func (p *Provider) insertDevice(ctx context.Context, rows []sample) error {
	if len(rows) == 0 {
		return nil
	}
	args := make([]any, 0, len(rows)*deviceColCount)
	for _, s := range rows {
		args = append(args,
			s.time,
			s.orgID,
			requiredString(s.deviceID),
			nullableString(s.deviceGroup),
			nullableString(s.driver),
			nullableBool(s.online),
			nullableFloat(s.hashrateTHs),
			nullableFloat(s.hashrateExpectedTHs),
			nullableBool(s.poolConnected),
		)
	}
	q := buildInsert("notification_device_metrics", deviceCols, deviceColCount, len(rows))
	_, err := p.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("insert notification_device_metrics: %w", err)
	}
	return nil
}

func (p *Provider) insertTemperature(ctx context.Context, rows []sample) error {
	if len(rows) == 0 {
		return nil
	}
	args := make([]any, 0, len(rows)*temperatureColCount)
	for _, s := range rows {
		args = append(args,
			s.time,
			s.orgID,
			requiredString(s.deviceID),
			nullableString(s.deviceGroup),
			nullableString(s.driver),
			s.sensorKind,
			s.temperatureMaxC,
			s.temperatureAvgC,
		)
	}
	q := buildInsert("notification_device_temperature", temperatureCols, temperatureColCount, len(rows))
	_, err := p.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("insert notification_device_temperature: %w", err)
	}
	return nil
}

func (p *Provider) insertCommand(ctx context.Context, rows []sample) error {
	if len(rows) == 0 {
		return nil
	}
	args := make([]any, 0, len(rows)*commandColCount)
	for _, s := range rows {
		args = append(args, s.time, s.orgID, s.commandKind, s.result)
	}
	q := buildInsert("notification_command_events", commandCols, commandColCount, len(rows))
	_, err := p.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("insert notification_command_events: %w", err)
	}
	return nil
}

func (p *Provider) insertTelemetry(ctx context.Context, rows []sample) error {
	if len(rows) == 0 {
		return nil
	}
	args := make([]any, 0, len(rows)*telemetryColCount)
	for _, s := range rows {
		args = append(args, s.time, s.orgID, nullableString(s.deviceID), s.result)
	}
	q := buildInsert("notification_telemetry_poll_events", telemetryCols, telemetryColCount, len(rows))
	_, err := p.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("insert notification_telemetry_poll_events: %w", err)
	}
	return nil
}

// buildInsert renders INSERT INTO <table> <cols> VALUES (...),(...) with
// 1-based parameter placeholders.
func buildInsert(table, cols string, colCount, rowCount int) string {
	var b strings.Builder
	b.Grow(64 + colCount*rowCount*6)
	b.WriteString("INSERT INTO ")
	b.WriteString(table)
	b.WriteString(" ")
	b.WriteString(cols)
	b.WriteString(" VALUES ")
	param := 1
	for i := range rowCount {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("(")
		for j := range colCount {
			if j > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, "$%d", param)
			param++
		}
		b.WriteString(")")
	}
	return b.String()
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// requiredString is used for columns declared NOT NULL — the writer treats
// the empty string as a real value rather than NULL so a schema mismatch is
// loud rather than silent.
func requiredString(s string) any { return s }

func nullableBool(b *bool) any {
	if b == nil {
		return nil
	}
	return *b
}

func nullableFloat(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}
