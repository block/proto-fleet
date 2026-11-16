package containers

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func projectEquipment(c *pb.Container, fansJSON, controlsJSON []byte, now time.Time) error {
	var fans []sdk.ContainerFan
	if len(fansJSON) > 0 {
		if err := json.Unmarshal(fansJSON, &fans); err != nil {
			return fmt.Errorf("decode container fans: %w", err)
		}
	}
	for _, f := range fans {
		c.Fans = append(c.Fans, &pb.ContainerFan{ComponentId: f.ComponentID, ObservedAt: timestamppb.New(f.ObservedAt), SpeedPercent: f.SpeedPercent, PowerW: f.PowerW, Direction: f.Direction, AlarmActive: f.AlarmActive, Stale: observationStale(f.ObservedAt, now)})
	}
	var controls *sdk.ContainerControls
	if len(controlsJSON) > 0 {
		if err := json.Unmarshal(controlsJSON, &controls); err != nil {
			return fmt.Errorf("decode container controls: %w", err)
		}
	}
	if v := controls; v != nil {
		c.Controls = &pb.ContainerControls{ObservedAt: timestamppb.New(v.ObservedAt), Supported: v.Supported, MainCircuitBreakerSupported: v.MainCircuitBreakerSupported, MainCircuitBreakerClosed: v.MainCircuitBreakerClosed, AlarmResetSupported: v.AlarmResetSupported, AlarmMuteSupported: v.AlarmMuteSupported, DryCoolerFanReverseSupported: v.DryCoolerFanReverseSupported, Stale: observationStale(v.ObservedAt, now)}
		for _, ctl := range v.Controls {
			c.Controls.Controls = append(c.Controls.Controls, &pb.ContainerControl{Name: ctl.Name, Enabled: ctl.Enabled})
		}
		for _, sp := range v.Setpoints {
			c.Controls.Setpoints = append(c.Controls.Setpoints, &pb.ContainerSetpoint{Name: sp.Name, Value: sp.Value, Unit: sp.Unit})
		}
	}
	return nil
}

func projectPlant(c *pb.Container, plantJSON []byte, now time.Time) error {
	var plant *sdk.ContainerPlant
	if len(plantJSON) == 0 {
		return nil
	}
	if err := json.Unmarshal(plantJSON, &plant); err != nil {
		return fmt.Errorf("decode container plant: %w", err)
	}
	if plant == nil {
		return nil
	}
	temperature := plant.DryCoolerInletTemperatureC
	if temperature != nil && (math.IsNaN(*temperature) || math.IsInf(*temperature, 0)) {
		temperature = nil
	}
	c.Plant = &pb.ContainerPlant{
		ObservedAt:                 timestamppb.New(plant.ObservedAt),
		DryCoolerInletTemperatureC: temperature,
		AlarmActive:                plant.AlarmActive,
		CriticalAlarmActive:        plant.CriticalAlarmActive,
		BuzzerActive:               plant.BuzzerActive,
		Stale:                      observationStale(plant.ObservedAt, now),
	}
	return nil
}
