package proto

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// ClearPools uses MDKv2's stable-ID, persist-before-acknowledgement deletion.
// The legacy MDKv1 empty POST acknowledges before saving and logs save failures;
// it cannot establish the durable result needed to retire a Fleet assignment.
func (c *Client) ClearPools(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	pools, err := c.readStoredPools(ctx)
	if err != nil {
		return sdk.WriteRefused(err, sdk.WriteStageConfigurationRead, sdk.WriteReasonReadFailed)
	}
	if err := c.verifyPoolRuntime(ctx, pools); err != nil {
		return sdk.WriteRefused(err, sdk.WriteStageConfigurationRead, sdk.WriteReasonReadFailed)
	}
	for _, pool := range pools {
		resp, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/v2/pools/%d", pool.Id), nil)
		if err != nil {
			return fmt.Errorf("clear pool request failed; outcome unconfirmed")
		}
		err = checkResponse(resp, "clear pool failed", http.StatusOK)
		resp.Body.Close()
		if err != nil {
			return err
		}
	}
	remaining, err := c.readStoredPools(ctx)
	if err != nil {
		return fmt.Errorf("pool clear readback unavailable; outcome unconfirmed")
	}
	if len(remaining) != 0 {
		return fmt.Errorf("device pools remain configured after clearing")
	}
	return c.verifyPoolRuntime(ctx, remaining)
}

func (c *Client) GetStoredMiningPools(ctx context.Context) ([]sdk.StoredMiningPool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	pools, err := c.readStoredPools(ctx)
	if err != nil {
		return nil, err
	}
	// Also reject configuration overrides or unavailable live observations. This
	// is used by Fleet both before clearing and when recovering a lost reply.
	if err := c.verifyPoolRuntime(ctx, pools); err != nil {
		return nil, err
	}
	result := make([]sdk.StoredMiningPool, 0, len(pools))
	for _, pool := range pools {
		result = append(result, sdk.StoredMiningPool{Priority: pool.Priority, URL: pool.Url, Username: pool.Username})
	}
	return result, nil
}

func (c *Client) readStoredPools(ctx context.Context) ([]*mdk.Pool, error) {
	response := &mdk.ListPoolsResponse{}
	if err := c.getPoolMessage(ctx, "/api/v2/pools", response); err != nil {
		return nil, err
	}
	seen := map[int64]bool{}
	for _, pool := range response.Pools {
		if pool == nil || pool.Id <= 0 || seen[pool.Id] || pool.Url == "" {
			return nil, fmt.Errorf("invalid stored pool configuration")
		}
		seen[pool.Id] = true
	}
	sort.Slice(response.Pools, func(i, j int) bool { return response.Pools[i].Priority < response.Pools[j].Priority })
	return response.Pools, nil
}

func (c *Client) verifyPoolRuntime(ctx context.Context, stored []*mdk.Pool) error {
	response := &mdk.GetPoolDiagnosticsResponse{}
	if err := c.getPoolMessage(ctx, "/api/v2/pools/diagnostics", response); err != nil {
		return err
	}
	expected := map[int64]*mdk.Pool{}
	for _, pool := range stored {
		if pool.Enabled {
			expected[pool.Id] = pool
		}
	}
	if response.WeightedMode || len(response.Pools) != len(expected) || response.ActivePoolId != 0 && expected[response.ActivePoolId] == nil {
		return fmt.Errorf("runtime pools differ from stored configuration")
	}
	for _, pool := range response.Pools {
		match := expected[pool.GetPoolId()]
		if match == nil || pool.GetPoolName() != match.Name || pool.GetConfiguredShareWeight() != 0 {
			return fmt.Errorf("runtime pools differ from stored configuration")
		}
		delete(expected, pool.GetPoolId())
	}
	return nil
}

// Use the canonical protobuf JSON contract, including omitted empty repeated
// fields and string-encoded int64 IDs. Never include firmware bodies in errors.
func (c *Client) getPoolMessage(ctx context.Context, path string, response proto.Message) error {
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return fmt.Errorf("pool configuration read unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNotImplemented {
		return sdk.NewErrUnsupportedCapability(sdk.CapabilityPoolClear)
	}
	if resp.StatusCode != http.StatusOK {
		return checkResponse(resp, "pool configuration read failed", http.StatusOK)
	}
	const maxRead = 1 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRead+1))
	if err != nil || len(body) > maxRead || protojson.Unmarshal(body, response) != nil {
		return fmt.Errorf("invalid pool configuration readback")
	}
	return nil
}
