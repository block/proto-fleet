package ct1

import (
	"context"
	"fmt"
	"net/http"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const mainCircuitBreakerControl = "main_circuit_breaker"

func (d *Device) SetContainerControl(ctx context.Context, req sdk.ContainerControlRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	c := d.client
	c.mu.Lock()
	defer c.mu.Unlock()
	var wire controlsResponse
	if err := c.get(ctx, "/api/v2/power/container/controls", &wire); err != nil {
		return err
	}
	observed := wire.observation(time.Now())
	if observed == nil {
		return sdk.NewErrUnsupportedCapability(req.Name)
	}
	path := "/api/v2/power/container/controls/" + req.Name
	body := map[string]bool{"enabled": *req.Enabled}
	if req.Name == mainCircuitBreakerControl {
		if observed.MainCircuitBreakerSupported == nil || !*observed.MainCircuitBreakerSupported || observed.MainCircuitBreakerClosed == nil {
			return sdk.NewErrUnsupportedCapability(req.Name)
		}
		path = "/api/v2/power/container/main-circuit-breaker"
		body = map[string]bool{"closed": *req.Enabled}
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
	var response struct {
		Control *struct {
			Name    string `json:"name"`
			Enabled *bool  `json:"enabled"`
		} `json:"control"`
		Closed *bool `json:"closed"`
	}
	// Never retry a write on authentication failure or ambiguous transport failure.
	// The preceding GET refreshes authentication; the PLC owns acceptance/interlocks.
	if _, err := c.request(ctx, http.MethodPost, path, body, &response); err != nil {
		return fmt.Errorf("equipment write failed (state unconfirmed): %w", err)
	}
	if req.Name == mainCircuitBreakerControl {
		if response.Closed == nil || *response.Closed != *req.Enabled {
			return fmt.Errorf("controller did not confirm requested breaker state")
		}
	} else if response.Control == nil || response.Control.Name != req.Name || (response.Control.Enabled == nil || *response.Control.Enabled != *req.Enabled) {
		return fmt.Errorf("controller did not acknowledge requested equipment state")
	}
	return nil
}
