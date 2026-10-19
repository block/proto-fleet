package hostedaccounts_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

// Inspect the real generated query's executed projection, rather than a timing
// threshold that could pass when unrelated telemetry is cached.
type miningProjectionDB struct {
	*sql.DB
	plan []byte
}

func (d *miningProjectionDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if err := d.DB.QueryRowContext(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) "+query, args...).Scan(&d.plan); err != nil {
		return nil, fmt.Errorf("explain mining projection: %w", err)
	}
	rows, err := d.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read mining projection: %w", err)
	}
	return rows, nil
}

type miningProjectionPlan struct {
	NodeType    string                 `json:"Node Type"`
	SubplanName string                 `json:"Subplan Name"`
	ActualRows  float64                `json:"Actual Rows"`
	ActualLoops float64                `json:"Actual Loops"`
	Plans       []miningProjectionPlan `json:"Plans"`
}

func (p miningProjectionPlan) liveProjection() (miningProjectionPlan, bool) {
	if p.SubplanName == "CTE live_assets" {
		return p, true
	}
	for _, child := range p.Plans {
		if projection, ok := child.liveProjection(); ok {
			return projection, true
		}
	}
	return miningProjectionPlan{}, false
}

// The Fleet view reads latest miner telemetry with a per-device LIMIT 1.
// Count its executions as well as materialized rows to catch late filtering.
func (p miningProjectionPlan) minerTelemetryLookups() float64 {
	if p.NodeType == "Limit" {
		return p.ActualLoops
	}
	var total float64
	for _, child := range p.Plans {
		total += child.minerTelemetryLookups()
	}
	return total
}

func TestAccountMiningSummaryScopesLargeFleetBeforeProjection(t *testing.T) {
	f := setup(t)
	first := f.client(t, "First", f.user, hostedaccounts.AllCapabilities)
	second := f.client(t, "Second", f.host, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, first.AccountID, "Subclient", f.host, hostedaccounts.AllCapabilities)
	require.NoError(t, err)
	viewers := []int64{addUser(t, f.db, f.org), addUser(t, f.db, f.org)}
	require.NoError(t, f.svc.SetMembership(t.Context(), f.user, f.org, first.AccountID, viewers[0], hostedaccounts.RoleViewer))
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, second.AccountID, viewers[1], hostedaccounts.RoleViewer))
	miners := make([]int64, 1000)
	for i := range miners {
		miners[i] = f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	}
	module, _ := f.miningModule(t)
	assign := pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN
	require.NoError(t, f.allocate(t, f.host, f.root, first.AccountID, assign, selected(5, miners[0], 0), selected(6, module, 0)))
	require.NoError(t, f.allocate(t, f.user, first.AccountID, child.AccountID, assign, selected(6, module, 1)))
	require.NoError(t, f.allocate(t, f.host, f.root, second.AccountID, assign, selected(5, miners[1], 0)))

	t.Run("projection", func(t *testing.T) {
		for _, account := range []struct {
			account  hostedaccounts.Account
			expected int
			miners   int
		}{{first, 2, 1}, {second, 1, 1}, {child, 1, 0}} {
			capture := &miningProjectionDB{DB: f.db}
			rows, err := sqlc.New(capture).ListHostedAccountMiningAssets(t.Context(), sqlc.ListHostedAccountMiningAssetsParams{OrganizationID: f.org, AccountInternalID: account.account.ID, SelectedAssets: json.RawMessage("[]")})
			require.NoError(t, err)
			require.Len(t, rows, account.expected)
			var plans []struct{ Plan miningProjectionPlan }
			require.NoError(t, json.Unmarshal(capture.plan, &plans))
			require.Len(t, plans, 1)
			projection, found := plans[0].Plan.liveProjection()
			require.True(t, found, "expected the executed live-assets projection")
			require.EqualValues(t, account.expected, projection.ActualRows*projection.ActualLoops, "account %s must not materialize unrelated host telemetry", account.account.Name)
			require.EqualValues(t, account.miners, projection.minerTelemetryLookups(), "account %s must not read unrelated miner telemetry", account.account.Name)

			// The paginated list shares the estate scope, so a customer page must not
			// materialize the host fleet either.
			capture = &miningProjectionDB{DB: f.db}
			page, err := sqlc.New(capture).ListHostedAccountMiningAssetPage(t.Context(), sqlc.ListHostedAccountMiningAssetPageParams{OrganizationID: f.org, AccountInternalID: account.account.ID, HostAuthority: json.RawMessage("[]"), PolicySites: []int64{}, PageLimit: 101})
			require.NoError(t, err)
			require.Len(t, page, account.expected)
			plans = nil
			require.NoError(t, json.Unmarshal(capture.plan, &plans))
			require.Len(t, plans, 1)
			projection, found = plans[0].Plan.liveProjection()
			require.True(t, found, "expected the executed page live-assets projection")
			require.EqualValues(t, account.expected, projection.ActualRows*projection.ActualLoops, "account %s page must not materialize unrelated host telemetry", account.account.Name)
			require.EqualValues(t, account.miners, projection.minerTelemetryLookups(), "account %s page must not read unrelated miner telemetry", account.account.Name)
		}
	})

	t.Run("concurrent summaries and revocation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		start := make(chan struct{})
		results := make(chan error, 3)
		for i, account := range []hostedaccounts.Account{first, second} {
			go func(i int, account hostedaccounts.Account) {
				<-start
				for range 6 {
					summary, err := f.svc.MiningSummary(ctx, viewers[i], f.org, account.AccountID)
					if i == 0 && fleeterror.IsForbiddenError(err) {
						continue
					}
					if err != nil {
						results <- err
						return
					}
					expected := int64(1)
					if i == 0 {
						expected = 2
					}
					if summary.Totals.AssetCount != expected {
						results <- fmt.Errorf("account %s returned %d assets, want %d", account.Name, summary.Totals.AssetCount, expected)
						return
					}
				}
				results <- nil
			}(i, account)
		}
		go func() { <-start; results <- f.svc.RevokeMembership(ctx, f.user, f.org, first.AccountID, viewers[0]) }()
		close(start)
		for range 3 {
			require.NoError(t, <-results)
		}
		_, err := f.svc.MiningSummary(ctx, viewers[0], f.org, first.AccountID)
		require.True(t, fleeterror.IsForbiddenError(err), "revocation must be live after the concurrent reports: %v", err)
	})
}

func TestAccountMiningReadsBoundOrganizationLockWait(t *testing.T) {
	for name, read := range map[string]func(context.Context, fixture, string) error{
		"summary": func(ctx context.Context, f fixture, account string) error {
			_, err := f.svc.MiningSummary(ctx, f.user, f.org, account)
			return err
		},
		"asset list": func(ctx context.Context, f fixture, account string) error {
			_, err := f.svc.ListAssets(ctx, f.user, f.org, &pb.ListAccountAssetsRequest{AccountId: account})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Blocked report", f.user, hostedaccounts.AllCapabilities)
			tx, err := f.db.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			defer tx.Rollback()
			_, err = tx.ExecContext(t.Context(), `SELECT id FROM organization WHERE id=$1 FOR NO KEY UPDATE`, f.org)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- read(ctx, f, client.AccountID) }()
			select {
			case err := <-result:
				require.Error(t, err)
			case <-time.After(8 * time.Second):
				t.Fatal("report did not bound organization lock wait")
			}
			require.NoError(t, tx.Rollback())
			require.NoError(t, read(t.Context(), f, client.AccountID), "timed-out report must release its transaction")
		})
	}
}
