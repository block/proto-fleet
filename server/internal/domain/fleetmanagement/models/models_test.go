package models

import (
	"testing"

	commonpb "github.com/btc-mining/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestMinerTelemetry_SetMeasurements(t *testing.T) {
	timestamp := timestamppb.Now()
	measurements := []*commonpb.Measurement{
		{Value: 100.0, Timestamp: timestamp},
		{Value: 200.0, Timestamp: timestamp},
	}

	tests := []struct {
		name            string
		measurementType pb.MeasurementConfig_MeasurementType
		verifyField     func(t *testing.T, telemetry *MinerTelemetry, expected []*commonpb.Measurement)
	}{
		{
			name:            "sets power usage measurements",
			measurementType: pb.MeasurementConfig_MEASUREMENT_TYPE_POWER_USAGE,
			verifyField: func(t *testing.T, telemetry *MinerTelemetry, expected []*commonpb.Measurement) {
				assert.Equal(t, expected, telemetry.PowerUsage)
				assert.Nil(t, telemetry.Temperature)
				assert.Nil(t, telemetry.Hashrate)
				assert.Nil(t, telemetry.Efficiency)
			},
		},
		{
			name:            "sets temperature measurements",
			measurementType: pb.MeasurementConfig_MEASUREMENT_TYPE_TEMPERATURE,
			verifyField: func(t *testing.T, telemetry *MinerTelemetry, expected []*commonpb.Measurement) {
				assert.Equal(t, expected, telemetry.Temperature)
				assert.Nil(t, telemetry.PowerUsage)
				assert.Nil(t, telemetry.Hashrate)
				assert.Nil(t, telemetry.Efficiency)
			},
		},
		{
			name:            "sets hashrate measurements",
			measurementType: pb.MeasurementConfig_MEASUREMENT_TYPE_HASHRATE,
			verifyField: func(t *testing.T, telemetry *MinerTelemetry, expected []*commonpb.Measurement) {
				assert.Equal(t, expected, telemetry.Hashrate)
				assert.Nil(t, telemetry.PowerUsage)
				assert.Nil(t, telemetry.Temperature)
				assert.Nil(t, telemetry.Efficiency)
			},
		},
		{
			name:            "sets efficiency measurements",
			measurementType: pb.MeasurementConfig_MEASUREMENT_TYPE_EFFICIENCY,
			verifyField: func(t *testing.T, telemetry *MinerTelemetry, expected []*commonpb.Measurement) {
				assert.Equal(t, expected, telemetry.Efficiency)
				assert.Nil(t, telemetry.PowerUsage)
				assert.Nil(t, telemetry.Temperature)
				assert.Nil(t, telemetry.Hashrate)
			},
		},
		{
			name:            "ignores unspecified measurement type",
			measurementType: pb.MeasurementConfig_MEASUREMENT_TYPE_UNSPECIFIED,
			verifyField: func(t *testing.T, telemetry *MinerTelemetry, _ []*commonpb.Measurement) {
				assert.Nil(t, telemetry.PowerUsage)
				assert.Nil(t, telemetry.Temperature)
				assert.Nil(t, telemetry.Hashrate)
				assert.Nil(t, telemetry.Efficiency)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			telemetry := &MinerTelemetry{}

			// Act
			telemetry.SetMeasurements(tt.measurementType, measurements)

			// Assert
			tt.verifyField(t, telemetry, measurements)
		})
	}
}

func TestMinerTelemetry_SetMeasurements_OverwritesExisting(t *testing.T) {
	// Arrange
	timestamp := timestamppb.Now()
	initialMeasurements := []*commonpb.Measurement{
		{Value: 50.0, Timestamp: timestamp},
	}
	newMeasurements := []*commonpb.Measurement{
		{Value: 100.0, Timestamp: timestamp},
		{Value: 200.0, Timestamp: timestamp},
	}
	telemetry := &MinerTelemetry{
		PowerUsage: initialMeasurements,
	}

	// Act
	telemetry.SetMeasurements(pb.MeasurementConfig_MEASUREMENT_TYPE_POWER_USAGE, newMeasurements)

	// Assert
	assert.Equal(t, newMeasurements, telemetry.PowerUsage)
	assert.Len(t, telemetry.PowerUsage, 2)
}

func TestMinerTelemetry_SetMeasurements_HandlesNilMeasurements(t *testing.T) {
	// Arrange
	timestamp := timestamppb.Now()
	telemetry := &MinerTelemetry{
		PowerUsage: []*commonpb.Measurement{
			{Value: 50.0, Timestamp: timestamp},
		},
	}

	// Act
	telemetry.SetMeasurements(pb.MeasurementConfig_MEASUREMENT_TYPE_POWER_USAGE, nil)

	// Assert
	assert.Nil(t, telemetry.PowerUsage)
}
