package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/plugins"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Match direct Fleet's absolute client lifetime. Idle handles have a separate
// allowance in the shared pool, leaving active-operation and cleanup headroom.
const telemetryDeviceTTL = time.Minute
const maxCachedTelemetryDevices = commandPoolSize

type cachedTelemetryDevice struct {
	device    sdk.Device
	handleID  string
	lease     *deviceHandleLease
	plugin    *plugins.LoadedPlugin
	key       [sha256.Size]byte
	expiresAt time.Time
	timer     *time.Timer
}

// Hash decrypted credentials, not their randomized ciphertext. Never retain or
// log an extra plaintext credential copy in the cache key.
func telemetryDeviceKey(info sdk.DeviceInfo, secret sdk.SecretBundle) ([sha256.Size]byte, error) {
	material, err := json.Marshal(struct {
		Info   sdk.DeviceInfo
		Secret sdk.SecretBundle
	}{info, secret})
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(material), nil
}

// Only idle devices live in the cache. The existing per-miner slot exclusively
// owns a checked-out device until Status actually returns, even after timeout.
func (f *pluginTelemetryFetcher) takeCachedDevice(id string, plugin *plugins.LoadedPlugin, key [sha256.Size]byte) *cachedTelemetryDevice {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry := f.cached[id]
	if entry == nil {
		return nil
	}
	delete(f.cached, id)
	entry.timer.Stop()
	if entry.plugin == plugin && entry.key == key && time.Now().Before(entry.expiresAt) {
		return entry
	}
	entry.lease.close(entry.device)
	return nil
}

func (f *pluginTelemetryFetcher) finishCachedDevice(ctx context.Context, slot *telemetrySlot, entry *cachedTelemetryDevice, succeeded bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !succeeded || ctx.Err() != nil || slot.abandoned || f.deviceHandles.ctx.Err() != nil ||
		!time.Now().Before(entry.expiresAt) || len(f.cached) >= maxCachedTelemetryDevices {
		entry.lease.close(entry.device)
		return
	}
	f.cached[slot.deviceIdentifier] = entry
	entry.timer = time.AfterFunc(time.Until(entry.expiresAt), func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.cached[slot.deviceIdentifier] == entry {
			delete(f.cached, slot.deviceIdentifier)
			entry.lease.close(entry.device)
		}
	})
}

func (f *pluginTelemetryFetcher) clearCachedDevices() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, entry := range f.cached {
		delete(f.cached, id)
		entry.timer.Stop()
		entry.lease.close(entry.device)
	}
}
