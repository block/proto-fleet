package ct1

import (
	"context"
	"fmt"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const mainCircuitBreakerControl = "main_circuit_breaker"

func (d *Device) SetContainerControl(ctx context.Context, req sdk.ContainerControlRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	c, err := d.selectClient(ctx, true)
	if err != nil {
		return err
	}
	wire, err := c.power.GetContainerControls(ctx, &mdk.GetContainerControlsRequest{})
	if err != nil {
		return err
	}
	observed := controlObservation(wire).observation(time.Now())
	if observed == nil {
		return sdk.NewErrUnsupportedCapability(req.Name)
	}
	if req.Name == mainCircuitBreakerControl {
		if observed.MainCircuitBreakerSupported == nil || !*observed.MainCircuitBreakerSupported || observed.MainCircuitBreakerClosed == nil {
			return sdk.NewErrUnsupportedCapability(req.Name)
		}
	} else {
		known := false
		if observed.Supported != nil && *observed.Supported {
			for _, control := range observed.Controls {
				if control.Name == req.Name && control.Enabled != nil {
					known = true
				}
			}
		}
		if !known {
			return sdk.NewErrUnsupportedCapability(req.Name)
		}
	}
	// Write once. A transport error after dispatch cannot prove whether the PLC applied it.
	if req.Name == mainCircuitBreakerControl {
		response, err := c.power.SetMainCircuitBreakerState(ctx, &mdk.SetMainCircuitBreakerStateRequest{Closed: *req.Enabled})
		if err != nil {
			return fmt.Errorf("equipment write failed (state unconfirmed): %w", err)
		}
		if response.GetClosed() != *req.Enabled {
			return fmt.Errorf("controller did not confirm requested breaker state")
		}
	} else {
		response, err := c.power.SetContainerControl(ctx, &mdk.SetContainerControlRequest{Name: req.Name, Enabled: *req.Enabled})
		if err != nil {
			return fmt.Errorf("equipment write failed (state unconfirmed): %w", err)
		}
		confirmation := response.GetConfirmation()
		if response.GetControl().GetName() != req.Name || confirmation == nil || confirmation.GetState() != mdk.PlantWriteConfirmationState_PLANT_WRITE_CONFIRMATION_STATE_CONFIRMED || confirmation.GetSource() != mdk.PlantWriteConfirmationSource_PLANT_WRITE_CONFIRMATION_SOURCE_CONTROLLER_REGISTER || confirmation.ObservedEnabled == nil || *confirmation.ObservedEnabled != *req.Enabled || confirmation.RequestedEnabled != *req.Enabled {
			return fmt.Errorf("controller did not confirm requested equipment state")
		}
	}
	return nil
}
