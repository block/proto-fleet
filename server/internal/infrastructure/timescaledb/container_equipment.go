package timescaledb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Supplementary observations are independent of topology ordering. Invalid
// supplementary records are ignored so they cannot discard valid inventory.
func storeContainerEquipment(ctx context.Context, q *sqlc.Queries, identifier string, snapshot *sdk.ContainerSnapshot) error {
	now := time.Now()
	validTime := func(t time.Time) bool { return !t.IsZero() && !t.After(now.Add(5*time.Second)) }
	finite := func(v *float64) bool { return v == nil || (!math.IsNaN(*v) && !math.IsInf(*v, 0)) }
	fans := make([]sdk.ContainerFan, 0, len(snapshot.Fans))
	seen := map[string]bool{}
	for _, f := range snapshot.Fans {
		if strings.TrimSpace(f.ComponentID) == "" || seen[f.ComponentID] || !validTime(f.ObservedAt) {
			continue
		}
		seen[f.ComponentID] = true
		if !finite(f.SpeedPercent) || (f.SpeedPercent != nil && (*f.SpeedPercent < 0 || *f.SpeedPercent > 100)) {
			f.SpeedPercent = nil
		}
		if !finite(f.PowerW) || (f.PowerW != nil && *f.PowerW < 0) {
			f.PowerW = nil
		}
		fans = append(fans, f)
	}
	data, err := json.Marshal(fans)
	if err != nil {
		return fmt.Errorf("encode fan observations: %w", err)
	}
	if err := q.UpsertContainerFans(ctx, sqlc.UpsertContainerFansParams{DeviceIdentifier: identifier, Fans: data}); err != nil {
		return err
	}
	c := snapshot.Controls
	if c == nil || !validTime(c.ObservedAt) {
		return nil
	}
	// A malformed duplicate list must not replace a previously valid observation.
	seen = map[string]bool{}
	for _, v := range c.Controls {
		if strings.TrimSpace(v.Name) == "" || seen[v.Name] {
			return nil
		}
		seen[v.Name] = true
	}
	seen = map[string]bool{}
	for _, v := range c.Setpoints {
		if strings.TrimSpace(v.Name) == "" || seen[v.Name] || !finite(v.Value) {
			return nil
		}
		seen[v.Name] = true
	}
	data, err = json.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode control observation: %w", err)
	}
	id, err := q.AdvanceContainerControls(ctx, sqlc.AdvanceContainerControlsParams{DeviceIdentifier: identifier, Observation: data})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := q.DeleteContainerControls(ctx, id); err != nil {
		return err
	}
	if err := q.DeleteContainerSetpoints(ctx, id); err != nil {
		return err
	}
	controls, setpoints := c.Controls, c.Setpoints
	if controls == nil {
		controls = []sdk.ContainerControl{}
	}
	if setpoints == nil {
		setpoints = []sdk.ContainerSetpoint{}
	}
	data, err = json.Marshal(controls)
	if err != nil {
		return fmt.Errorf("encode control states: %w", err)
	}
	if err := q.InsertContainerControls(ctx, sqlc.InsertContainerControlsParams{DeviceID: id, Controls: data}); err != nil {
		return err
	}
	data, err = json.Marshal(setpoints)
	if err != nil {
		return fmt.Errorf("encode setpoints: %w", err)
	}
	return q.InsertContainerSetpoints(ctx, sqlc.InsertContainerSetpointsParams{DeviceID: id, Setpoints: data})
}
