package rollout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type modelGroupAvailabilityFiles struct {
	FirmwareFiles
	lookups     map[string]int
	strictCalls int
	unavailable bool
}

func (f *modelGroupAvailabilityFiles) FindCachedFirmwareFileIDByChecksum(checksum string) (string, bool) {
	f.lookups[checksum]++
	if f.unavailable {
		return "", false
	}
	return "cached-" + checksum, true
}

func (f *modelGroupAvailabilityFiles) FindFirmwareFileIDByChecksum(string) (string, bool) {
	f.strictCalls++
	return "", false
}

func TestModelGroupPagesUseCachedAvailability(t *testing.T) {
	for _, distinct := range []bool{false, true} {
		name := "shared artifact"
		if distinct {
			name = "100 distinct artifacts"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, 0)
			ctx := t.Context()
			channel, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Model groups"})
			require.NoError(t, err)
			_, err = f.conn.ExecContext(ctx, `INSERT INTO release_channel_firmware
				(channel_id, manufacturer, model, firmware_checksum, firmware_version,
				 firmware_target_manufacturer, firmware_target_model, assigned_by)
				SELECT $1, 'proto', 'rig-' || n, lpad(to_hex(CASE WHEN $2 THEN n ELSE 1 END), 64, '0'),
				 'v1', 'proto', 'rig-' || n, 1 FROM generate_series(1, 100) n`, channel.ID, distinct)
			require.NoError(t, err)
			lookups := &modelGroupAvailabilityFiles{FirmwareFiles: f.files}
			f.svc.files = lookups
			for _, unavailable := range []bool{false, true, false} {
				lookups.unavailable = unavailable
				lookups.lookups = map[string]int{}
				groups, cursor, err := f.svc.ListChannelModelGroups(ctx, f.orgID, channel.ID, 100, "")
				require.NoError(t, err)
				require.Len(t, groups, 100)
				assert.Empty(t, cursor)
				assert.Zero(t, lookups.strictCalls, "polling must not rehash any firmware payload")
				wantLookups := 1
				if distinct {
					wantLookups = 100
				}
				assert.Len(t, lookups.lookups, wantLookups)
				for checksum, calls := range lookups.lookups {
					assert.Equal(t, 1, calls, "checksum %s must be checked once, including unavailable results", checksum)
				}
				for _, group := range groups {
					assert.Equal(t, !unavailable, group.FirmwareAvailable, "each new response refreshes cached file availability")
					if unavailable {
						assert.Empty(t, group.FirmwareFileID)
					} else {
						assert.Equal(t, "cached-"+group.FirmwareChecksum, group.FirmwareFileID)
					}
				}
			}
		})
	}
}
