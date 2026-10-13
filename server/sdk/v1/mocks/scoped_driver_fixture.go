package mocks

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// ScopedDriverFixture gives mock-backed domain tests the SDK registry contract.
// Real backend lifecycle is exercised separately through the actual SDK server.
type ScopedDriverFixture struct {
	sdk.Driver
	mu      sync.Mutex
	entries map[string]scopedFixtureEntry
}
type scopedFixtureEntry struct {
	digest [32]byte
	result sdk.NewDeviceResult
}

func (d *ScopedDriverFixture) NewDevice(ctx context.Context, id string, info sdk.DeviceInfo, secret sdk.SecretBundle) (sdk.NewDeviceResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.entries, id)
	return d.Driver.NewDevice(ctx, id, info, secret)
}
func (d *ScopedDriverFixture) GetOrCreateDevice(ctx context.Context, id string, info sdk.DeviceInfo, secret sdk.SecretBundle, scope string) (sdk.NewDeviceResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return sdk.NewDeviceResult{}, fmt.Errorf("resolve scoped fixture: %w", err)
	}
	data, err := json.Marshal(struct {
		Info   sdk.DeviceInfo
		Secret sdk.SecretBundle
		Scope  string
	}{info, secret, scope})
	if err != nil {
		return sdk.NewDeviceResult{}, fmt.Errorf("marshal scoped fixture configuration: %w", err)
	}
	digest := sha256.Sum256(data)
	if e, ok := d.entries[id]; ok && e.digest == digest {
		return e.result, nil
	}
	result, err := d.Driver.NewDevice(ctx, id, info, secret)
	if err == nil {
		if d.entries == nil {
			d.entries = map[string]scopedFixtureEntry{}
		}
		d.entries[id] = scopedFixtureEntry{digest, result}
	}
	return result, err
}
