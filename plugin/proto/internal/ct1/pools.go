package ct1

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

var _ sdk.DeviceStoredPoolReader = (*Device)(nil)

// UpdateMiningPools reconciles the complete priority stack using the existing
// per-pool firmware API. Credential updates to an unchanged enabled stack keep
// pools enabled unless a password must be cleared. Other updates disable pools
// first because firmware rejects intermediate duplicate URLs/priorities during
// swaps. This can interrupt mining. Backups are updated before the primary; obsolete
// slots are removed last. A failed RPC stops the sequence, without rollback (old
// passwords cannot be read) or replay onto another HA controller. A subsequent
// explicitly admitted attempt starts from current stored pools and converges.
func (d *Device) UpdateMiningPools(ctx context.Context, desired []sdk.MiningPoolConfig) error {
	desired = slices.Clone(desired)
	slices.SortFunc(desired, func(a, b sdk.MiningPoolConfig) int { return cmp.Compare(a.Priority, b.Priority) })
	if err := validateWholePoolStack(desired); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	c, err := d.selectClient(ctx, true)
	if err != nil {
		return err
	}
	groups := mdk.NewMiningGroupServiceClient(c.conn)
	if err := requireLegacyPoolMode(ctx, groups); err != nil {
		return err
	}
	client := mdk.NewPoolServiceClient(c.conn)
	current, err := readWholePools(ctx, client)
	if err != nil {
		return err
	}
	for _, pool := range current {
		if pool.ShareWeight != nil {
			return fmt.Errorf("pool mode check: %w", status.Error(codes.FailedPrecondition, "weighted pool allocation cannot be replaced by a priority configuration"))
		}
	}
	if err := verifyRuntimePools(ctx, client, current); err != nil {
		return err
	}
	if !canUpdatePoolsInPlace(current, desired) {
		for _, pool := range current {
			if !pool.Enabled {
				continue
			}
			if err := requireLegacyPoolMode(ctx, groups); err != nil {
				return err
			}
			disabled := proto.CloneOf(pool)
			disabled.Enabled = false
			if _, err := client.UpdatePool(ctx, &mdk.UpdatePoolRequest{Pool: disabled, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"enabled"}}}); err != nil {
				return poolWriteError("disable pool", err)
			}
		}
	}
	for i := len(desired) - 1; i >= 0; i-- {
		want := desired[i]
		pool := &mdk.Pool{Name: unusedPoolName(current, i), Url: want.URL, Username: want.WorkerName, Priority: want.Priority, Enabled: true}
		if i < len(current) {
			pool.Id = current[i].Id
			if current[i].Name != "" {
				pool.Name = current[i].Name
			}
		}
		if err := requireLegacyPoolMode(ctx, groups); err != nil {
			return err
		}
		if pool.Id != 0 && want.Password == "" {
			// Empty UpdatePool passwords mean preserve. Deletion/recreation is
			// the only current firmware operation that writes no credential.
			// Frigate 6acbeba0 pkg/api/pool_service.go CreatePool stores NULL
			// for an empty password; the pool manager renders NULL as empty.
			if _, err := client.DeletePool(ctx, &mdk.DeletePoolRequest{Id: pool.Id}); err != nil {
				return poolWriteError("remove credential", err)
			}
			pool.Id = 0
			if err := requireLegacyPoolMode(ctx, groups); err != nil {
				return err
			}
		}
		if pool.Id == 0 {
			if _, err := client.CreatePool(ctx, &mdk.CreatePoolRequest{Pool: pool, Password: want.Password}); err != nil {
				return poolWriteError("create pool", err)
			}
		} else {
			// Include every mutable field explicitly; the firmware has different
			// legacy semantics for an empty mask and a share_weight-only mask.
			mask := &fieldmaskpb.FieldMask{Paths: []string{"name", "url", "username", "priority", "enabled", "share_weight"}}
			if _, err := client.UpdatePool(ctx, &mdk.UpdatePoolRequest{Pool: pool, Password: want.Password, UpdateMask: mask}); err != nil {
				return poolWriteError("update pool", err)
			}
		}
	}
	for i := len(desired); i < len(current); i++ {
		if err := requireLegacyPoolMode(ctx, groups); err != nil {
			return err
		}
		if _, err := client.DeletePool(ctx, &mdk.DeletePoolRequest{Id: current[i].Id}); err != nil {
			return poolWriteError("remove obsolete pool", err)
		}
	}
	if err := requireLegacyPoolMode(ctx, groups); err != nil {
		return err
	}
	actual, err := readWholePools(ctx, client)
	if err != nil {
		return fmt.Errorf("pool update readback unavailable; state unconfirmed")
	}
	if len(actual) != len(desired) {
		return fmt.Errorf("pool update readback does not match the requested stack")
	}
	for i, pool := range actual {
		want := desired[i]
		if pool.Priority != want.Priority || pool.Url != want.URL || pool.Username != want.WorkerName || !pool.Enabled || pool.ShareWeight != nil {
			return fmt.Errorf("pool update readback does not match the requested stack")
		}
	}
	if err := verifyRuntimePools(ctx, client, actual); err != nil {
		return err
	}
	// Credentials remain unverified: firmware never returns passwords. The
	// optional stored-reader below deliberately reports nil password evidence.
	return nil
}

// Both stacks are sorted by priority. Matching positions cannot introduce URL
// or priority collisions. Empty passwords still require deletion/recreation,
// since firmware treats an empty UpdatePool password as preserving credentials.
func canUpdatePoolsInPlace(current []*mdk.Pool, desired []sdk.MiningPoolConfig) bool {
	if len(current) != len(desired) {
		return false
	}
	for i, pool := range current {
		want := desired[i]
		if !pool.Enabled || pool.Url != want.URL || pool.Priority != want.Priority || want.Password == "" {
			return false
		}
	}
	return true
}

func (d *Device) GetMiningPools(ctx context.Context) ([]sdk.ConfiguredPool, error) {
	pools, err := d.readPoolStack(ctx, true)
	if err != nil {
		return nil, err
	}
	result := make([]sdk.ConfiguredPool, 0, len(pools))
	for _, pool := range pools {
		if pool.Enabled {
			result = append(result, sdk.ConfiguredPool{Priority: pool.Priority, URL: pool.Url, Username: pool.Username})
		}
	}
	return result, nil
}

func (d *Device) GetStoredMiningPools(ctx context.Context) ([]sdk.StoredMiningPool, error) {
	pools, err := d.readPoolStack(ctx, false)
	if err != nil {
		return nil, err
	}
	result := make([]sdk.StoredMiningPool, 0, len(pools))
	for _, pool := range pools {
		result = append(result, sdk.StoredMiningPool{Priority: pool.Priority, URL: pool.Url, Username: pool.Username})
	}
	return result, nil
}

func (d *Device) readPoolStack(ctx context.Context, runtime bool) ([]*mdk.Pool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	c, err := d.selectClient(ctx, false)
	if err != nil {
		return nil, err
	}
	if err := requireLegacyPoolMode(ctx, mdk.NewMiningGroupServiceClient(c.conn)); err != nil {
		return nil, err
	}
	client := mdk.NewPoolServiceClient(c.conn)
	pools, err := readWholePools(ctx, client)
	if err == nil && runtime {
		err = verifyRuntimePools(ctx, client, pools)
	}
	return pools, err
}

// ListPools reflects stored rows even when native DATUM overrides the runtime
// manager. Diagnostics identify the rows actually loaded by this controller.
// Connection health is deliberately not required: an unreachable destination
// does not mean its configuration failed to apply.
func verifyRuntimePools(ctx context.Context, client mdk.PoolServiceClient, stored []*mdk.Pool) error {
	response, err := client.GetPoolDiagnostics(ctx, &mdk.GetPoolDiagnosticsRequest{})
	if err != nil {
		return fmt.Errorf("container runtime pool verification unavailable")
	}
	expected := make(map[int64]*mdk.Pool)
	for _, pool := range stored {
		if pool.Enabled {
			expected[pool.Id] = pool
		}
	}
	if response.GetWeightedMode() || len(response.GetPools()) != len(expected) {
		return fmt.Errorf("container runtime pools differ from stored configuration")
	}
	for _, pool := range response.GetPools() {
		match := expected[pool.GetPoolId()]
		if match == nil || pool.GetPoolName() != match.Name || pool.GetConfiguredShareWeight() != 0 {
			return fmt.Errorf("container runtime pools differ from stored configuration")
		}
		delete(expected, pool.GetPoolId())
	}
	return nil
}

func requireLegacyPoolMode(ctx context.Context, client mdk.MiningGroupServiceClient) error {
	response, err := client.GetMiningGroupCapability(ctx, &mdk.GetMiningGroupCapabilityRequest{})
	if err != nil {
		return fmt.Errorf("pool mode check: %w", status.Error(status.Code(err), "cannot verify container mining group mode"))
	}
	if response.GetCapability() == nil {
		return fmt.Errorf("container mining group mode is unavailable")
	}
	if response.Capability.Enabled {
		return fmt.Errorf("pool mode check: %w", status.Error(codes.FailedPrecondition, "whole-container pools cannot change while mining groups are enabled"))
	}
	return nil
}

func unusedPoolName(current []*mdk.Pool, slot int) string {
	base := fmt.Sprintf("Fleet pool %d", slot+1)
	name := base
	for suffix := 2; slices.ContainsFunc(current, func(p *mdk.Pool) bool { return p.Name == name }); suffix++ {
		name = fmt.Sprintf("%s (%d)", base, suffix)
	}
	return name
}

func readWholePools(ctx context.Context, client mdk.PoolServiceClient) ([]*mdk.Pool, error) {
	response, err := client.ListPools(ctx, &mdk.ListPoolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("pool readback: %w", status.Error(status.Code(err), "read container pool configuration failed"))
	}
	pools := response.GetPools()
	seen := map[int64]bool{}
	for _, pool := range pools {
		if pool == nil || pool.Id <= 0 || seen[pool.Id] {
			return nil, fmt.Errorf("invalid container pool readback")
		}
		seen[pool.Id] = true
	}
	slices.SortFunc(pools, func(a, b *mdk.Pool) int { return cmp.Or(cmp.Compare(a.Priority, b.Priority), cmp.Compare(a.Id, b.Id)) })
	return pools, nil
}

func poolWriteError(action string, err error) error {
	// Firmware errors may echo the supplied password. Retain only the transport
	// code; a lost acknowledgement never authorizes an automatic replay.
	return status.Errorf(status.Code(err), "container pool %s failed; state unconfirmed", action)
}

func validateWholePoolStack(pools []sdk.MiningPoolConfig) error {
	if len(pools) < 1 || len(pools) > 3 {
		return fmt.Errorf("pool configuration requires one primary and up to two backups")
	}
	urls := make(map[string]bool, len(pools))
	for i, pool := range pools {
		parsed, err := url.Parse(pool.URL)
		if err != nil || parsed.Host == "" || parsed.User != nil || len(pool.URL) > 256 || strings.ContainsAny(pool.URL, "\r\n\t ") {
			return fmt.Errorf("invalid pool URL")
		}
		switch parsed.Scheme {
		case "stratum+tcp", "stratum+ssl":
		default:
			return fmt.Errorf("unsupported pool URL scheme")
		}
		key := strings.ToLower(pool.URL)
		if urls[key] {
			return fmt.Errorf("container pools require distinct URLs")
		}
		urls[key] = true
		if int64(pool.Priority) != int64(i) || pool.WorkerName == "" || len(pool.WorkerName) > 255 || len(pool.Password) > 255 {
			return fmt.Errorf("invalid pool priority or credential")
		}
		for _, value := range []string{pool.WorkerName, pool.Password} {
			if !utf8.ValidString(value) || strings.ContainsFunc(value, unicode.IsControl) {
				return fmt.Errorf("invalid pool credential")
			}
		}
		if strings.ContainsFunc(pool.WorkerName, unicode.IsSpace) {
			return fmt.Errorf("invalid pool worker")
		}
	}
	return nil
}
