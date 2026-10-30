package hostedaccounts_test

import (
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestHashboardReplacementCandidatesApplyFullOwnerPolicyBeforePagination(t *testing.T) {
	for _, restriction := range []string{"root", "client", "subclient", "client ceiling", "subclient ceiling"} {
		t.Run(restriction, func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
			child, err := f.svc.CreateChild(t.Context(), f.user, f.org, client.AccountID, "Subclient", f.host, hostedaccounts.AllCapabilities)
			require.NoError(t, err)
			module, _ := f.miningModule(t)
			old := f.moduleSelection(t, module).Boards[0]
			require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(7, old.Id, 0)))
			require.NoError(t, f.allocate(t, f.user, client.AccountID, child.AccountID, 1, selected(7, old.Id, 1)))
			old.ExpectedRevision = 2
			var allowedSite, excludedSite int64
			require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(name,org_id,slug) VALUES('Allowed',$1,'allowed') RETURNING id`, f.org).Scan(&allowedSite))
			require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(name,org_id,slug) VALUES('Excluded',$1,'excluded') RETURNING id`, f.org).Scan(&excludedSite))
			var allowed []int64
			for _, site := range []int64{excludedSite, allowedSite} {
				spareModule, controller := f.miningModule(t)
				_, err = f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site, controller)
				require.NoError(t, err)
				f.observeModuleAtCurrentSite(t, spareModule, controller)
				boards := f.moduleSelection(t, spareModule).Boards
				if site == allowedSite {
					for _, board := range boards {
						allowed = append(allowed, board.Id)
					}
				}
			}
			policyContext := t.Context()
			actor, target := f.host, f.root
			switch restriction {
			case "client":
				actor, target = f.user, client.AccountID
			case "subclient":
				target = child.AccountID
			case "client ceiling":
				policyContext, err = hostedaccounts.WithRequestAccountID(t.Context(), f.root)
				target = client.AccountID
			case "subclient ceiling":
				policyContext, err = hostedaccounts.WithRequestAccountID(t.Context(), client.AccountID)
				actor, target = f.user, child.AccountID
			}
			require.NoError(t, err)
			require.NoError(t, f.svc.ConfigureSites(policyContext, actor, f.org, target, true, []int64{allowedSite}))
			request := &pb.ListAccountAssetsRequest{AccountId: f.root, OwnerAccountId: &f.root, HashboardsOnly: true, ReplacementFor: old, PageSize: 1}
			var found []int64
			for range 4 {
				page, err := f.svc.ListAssets(t.Context(), f.host, f.org, request)
				require.NoError(t, err)
				require.Len(t, page.Assets, 1, "excluded candidates must not consume a page")
				board := page.Assets[0]
				require.True(t, board.Available)
				require.False(t, board.OutsidePolicy)
				require.Equal(t, allowedSite, board.SiteId)
				require.Equal(t, f.root, board.OwnerAccountId)
				found = append(found, board.Id)
				if page.NextPageToken == "" {
					break
				}
				request.PageToken = page.NextPageToken
			}
			require.ElementsMatch(t, allowed, found)
			// Policy changes after selection must still be checked at transfer time.
			require.NoError(t, f.svc.ConfigureSites(policyContext, actor, f.org, target, true, nil))
			err = f.svc.ReplaceHashboard(t.Context(), f.host, f.org, &pb.ReplaceAccountHashboardRequest{
				AccountId: f.root, OldBoard: old, ReplacementBoard: &pb.HashboardAllocationSelection{Id: found[0]},
			})
			require.ErrorContains(t, err, "outside the allocated account's site policy")
			request.PageToken = ""
			page, err := f.svc.ListAssets(t.Context(), f.host, f.org, request)
			require.NoError(t, err)
			require.Empty(t, page.Assets)
			require.Empty(t, page.NextPageToken)
		})
	}
}

func TestHashboardReplacementCandidatesRequireReviewedHostAllocation(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	module, _ := f.miningModule(t)
	boards := f.moduleSelection(t, module).Boards
	for _, board := range boards[:2] {
		require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(7, board.Id, 0)))
	}
	f.miningModule(t)
	request := &pb.ListAccountAssetsRequest{AccountId: f.root, OwnerAccountId: &f.root, HashboardsOnly: true,
		ReplacementFor: &pb.HashboardAllocationSelection{Id: boards[0].Id, ExpectedRevision: 1}, PageSize: 1}
	page, err := f.svc.ListAssets(t.Context(), f.host, f.org, request)
	require.NoError(t, err)
	require.NotEmpty(t, page.NextPageToken)
	for _, source := range []*pb.HashboardAllocationSelection{
		{Id: boards[1].Id, ExpectedRevision: 1},
		nil,
	} {
		changed := proto.CloneOf(request)
		changed.PageToken = page.NextPageToken
		changed.ReplacementFor = source
		_, err = f.svc.ListAssets(t.Context(), f.host, f.org, changed)
		require.ErrorContains(t, err, "page token scope changed")
	}
	_, err = f.svc.ListAssets(t.Context(), f.user, f.org, request)
	require.True(t, fleeterror.IsForbiddenError(err))
	customer := proto.CloneOf(request)
	customer.AccountId, customer.OwnerAccountId = client.AccountID, &client.AccountID
	_, err = f.svc.ListAssets(t.Context(), f.user, f.org, customer)
	require.True(t, fleeterror.IsForbiddenError(err))
	request.ReplacementFor.ExpectedRevision = 0
	_, err = f.svc.ListAssets(t.Context(), f.host, f.org, request)
	require.ErrorContains(t, err, "refresh")
	request.ReplacementFor = &pb.HashboardAllocationSelection{Id: boards[2].Id}
	_, err = f.svc.ListAssets(t.Context(), f.host, f.org, request)
	require.ErrorContains(t, err, "refresh", "an unallocated source has no replacement owner")
}
