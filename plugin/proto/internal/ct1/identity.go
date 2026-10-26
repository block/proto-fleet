package ct1

import (
	"context"
	"fmt"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/google/uuid"
)

// A new group needs confirmed durable identity. During an outage a retained ID
// can verify an existing group, but it cannot establish a new one.
func haIdentity(report *mdk.HAStatus, id, savedKey string) (string, error) {
	if report == nil {
		return "", nil
	}
	if report.ReportingServerId != id || report.IdentityState == mdk.HAIdentityState_HA_IDENTITY_STATE_CONFLICTED || len(report.Conflicts) > 0 {
		return "", fmt.Errorf("invalid or conflicting CT1 HA identity")
	}
	if !report.Enabled || !report.InventoryReady {
		return "", nil
	}
	if report.ClusterId == "" && report.IdentityState == mdk.HAIdentityState_HA_IDENTITY_STATE_UNAVAILABLE {
		return "", nil
	}
	cluster, err := uuid.Parse(report.ClusterId)
	if err != nil || cluster == uuid.Nil || cluster.String() != report.ClusterId {
		return "", fmt.Errorf("invalid CT1 HA cluster ID")
	}
	key := "ct1:" + cluster.String()
	if report.IdentityState == mdk.HAIdentityState_HA_IDENTITY_STATE_CONFIRMED {
		return key, nil
	}
	if report.IdentityState == mdk.HAIdentityState_HA_IDENTITY_STATE_UNAVAILABLE && key == savedKey {
		return key, nil
	}
	return "", nil
}

// The same identity check serves discovery and live probes. Optional diagnostics
// may be absent only before an endpoint group has been saved.
func (c *Client) readHA(ctx context.Context, id, savedKey string) (*mdk.HAStatus, string, error) {
	response, err := c.system.GetHAStatus(ctx, &mdk.GetHAStatusRequest{})
	if err != nil {
		if savedKey != "" || ctx.Err() != nil {
			return nil, "", fmt.Errorf("read CT1 HA identity: %w", err)
		}
		return nil, "", nil
	}
	report := response.GetStatus()
	key, err := haIdentity(report, id, savedKey)
	if err != nil {
		return nil, "", err
	}
	if savedKey != "" && key != savedKey {
		return nil, "", fmt.Errorf("CT1 cluster identity changed")
	}
	return report, key, nil
}
