package poolapplications

import (
	"context"
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/stretchr/testify/require"
)

type runtimePoolReader struct {
	interfaces.Miner
	pools []interfaces.MinerConfiguredPool
	err   error
}

func (r runtimePoolReader) GetMiningPools(context.Context) ([]interfaces.MinerConfiguredPool, error) {
	return r.pools, r.err
}

type storedPoolReader struct {
	runtimePoolReader
	stored  []interfaces.MinerStoredPool
	readErr error
}

func (r storedPoolReader) GetStoredMiningPools(context.Context) ([]interfaces.MinerStoredPool, error) {
	return r.stored, r.readErr
}

func TestVerifyPoolsComparesAvailableSettings(t *testing.T) {
	desired := []dto.MiningPool{{URL: "stratum+tcp://primary.example", Username: "account.worker", Password: "secret"}}
	runtime := runtimePoolReader{pools: []interfaces.MinerConfiguredPool{{URL: desired[0].URL, Username: desired[0].Username}}}
	matching := interfaces.MinerStoredPool{URL: desired[0].URL, Username: desired[0].Username}
	password := desired[0].Password
	matchingPassword := matching
	matchingPassword.Password = &password
	wrongPassword := "incorrect"
	differingPassword := matching
	differingPassword.Password = &wrongPassword
	differingStoredURL := matching
	differingStoredURL.URL = "stratum+tcp://different.example"
	for _, tc := range []struct {
		name        string
		miner       interfaces.Miner
		state, code string
	}{
		{"no private reader", runtime, "verified", ""},
		{"private reader unsupported", storedPoolReader{runtimePoolReader: runtime, readErr: fleeterror.NewUnimplementedError("unsupported")}, "verified", ""},
		{"password omitted", storedPoolReader{runtimePoolReader: runtime, stored: []interfaces.MinerStoredPool{matching}}, "verified", ""},
		{"password matches", storedPoolReader{runtimePoolReader: runtime, stored: []interfaces.MinerStoredPool{matchingPassword}}, "verified", ""},
		{"password differs", storedPoolReader{runtimePoolReader: runtime, stored: []interfaces.MinerStoredPool{differingPassword}}, "different", "settings_differ"},
		{"stored URL differs", storedPoolReader{runtimePoolReader: runtime, stored: []interfaces.MinerStoredPool{differingStoredURL}}, "different", "settings_differ"},
		{"private read failed", storedPoolReader{runtimePoolReader: runtime, readErr: errors.New("read failed")}, "applied_unverified", "readback_unavailable"},
		{"runtime read failed", runtimePoolReader{err: errors.New("unreachable")}, "applied_unverified", "readback_unavailable"},
		{"runtime count differs", runtimePoolReader{}, "different", "settings_differ"},
		{"runtime username differs", runtimePoolReader{pools: []interfaces.MinerConfiguredPool{{URL: desired[0].URL, Username: "other"}}}, "different", "settings_differ"},
		{"runtime priority differs", runtimePoolReader{pools: []interfaces.MinerConfiguredPool{{URL: desired[0].URL, Username: desired[0].Username, Priority: 1}}}, "different", "settings_differ"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, code := verifyPools(t.Context(), tc.miner, desired)
			require.Equal(t, tc.state, state)
			require.Equal(t, tc.code, code)
		})
	}
}
