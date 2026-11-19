package hostedaccounts_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/migrations"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

func reportingBoundaryActors(t *testing.T, f fixture, account string) (int64, string) {
	t.Helper()
	var hostUUID string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.host).Scan(&hostUUID))
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account, hostedaccounts.ReportingAuthority{UserID: hostUUID, CanCreate: true, AllSites: true}, 0))
	reviewer := addUser(t, f.db, f.org)
	var role int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM role WHERE organization_id=$1 AND builtin_key='SUPER_ADMIN'`, f.org).Scan(&role))
	_, err := sqlc.New(f.db).AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: reviewer, OrganizationID: f.org, RoleID: role, ScopeType: "org"})
	require.NoError(t, err)
	var reviewerUUID string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, reviewer).Scan(&reviewerUUID))
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account, hostedaccounts.ReportingAuthority{UserID: reviewerUUID, CanCreate: true, AllSites: true}, 0))
	return reviewer, reviewerUUID
}

func reportingCoverageInputs(t *testing.T, f fixture, account string, count int) reporting.Inputs {
	t.Helper()
	input := reportingInputs(t, f, account, f.host)
	for i := range count {
		module, controller := f.miningModule(t)
		var site int64
		require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,$2,$2) RETURNING id`, f.org, fmt.Sprintf("report-site-%d", i)).Scan(&site))
		_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site, controller)
		require.NoError(t, err)
		f.observeModuleAtCurrentSite(t, module, controller)
		require.NoError(t, f.allocate(t, f.host, f.root, account, 1, selected(6, module, 0)))
		f.legacyModuleHistory(t)
		_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=now()-interval '48 hours',ended_at=now()-interval '24 hours' WHERE kind=6 AND asset_id=$1`, module)
		require.NoError(t, err)
		var allocation int64
		var start, end time.Time
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id,started_at,ended_at FROM hosted_account_asset_interval WHERE kind=6 AND asset_id=$1`, module).Scan(&allocation, &start, &end))
		order := fmt.Sprintf("order-%d", i)
		input.Orders = append(input.Orders, reporting.Order{ID: order, Name: order, Start: start.Add(-time.Hour), SourceRef: "signed order"})
		input.Coverage = append(input.Coverage, reporting.CoverageSegment{ID: fmt.Sprintf("coverage-%d", i), AllocationIntervalID: allocation, Kind: 6, AssetID: module, SiteID: site, ContainerID: controller, OrderID: order, Start: start, End: end, CapacityTHS: "100", SourceRef: "allocation evidence"})
	}
	return input
}

func TestReportingInputReplacementRequiresAuthorityOverRemovedSites(t *testing.T) {
	for _, retain := range []bool{false, true} {
		t.Run(fmt.Sprint(retain), func(t *testing.T) {
			f := setup(t)
			account := f.client(t, "Reporting client", f.user, hostedaccounts.AllCapabilities)
			actor, id := reportingBoundaryActors(t, f, account.AccountID)
			input := reportingCoverageInputs(t, f, account.AccountID, 2)
			_, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "both sites", input)
			require.NoError(t, err)
			grant := hostedaccounts.ReportingAuthority{UserID: id, CanCreate: true, SiteIDs: []int64{input.Coverage[0].SiteID}}
			require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, grant, 1))
			_, err = reportingInputsForTest(f.svc, t.Context(), actor, f.org, account.AccountID)
			require.ErrorContains(t, err, "account access denied", "a narrowed grant cannot read the saved A+B scope")
			setup, err := f.svc.HostReportingSetup(t.Context(), actor, f.org, account.AccountID)
			require.NoError(t, err)
			require.Nil(t, setup.Input)
			if retain {
				input.Coverage = input.Coverage[:1]
			} else {
				input.Coverage = nil
			}
			_, err = saveReportingInputsForTest(f.svc, t.Context(), actor, f.org, account.AccountID, 0, "remove site", input)
			require.ErrorContains(t, err, "account access denied")
			var latest int64
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT max(revision) FROM hosted_reporting_inputs`).Scan(&latest))
			require.EqualValues(t, 1, latest)
			grant.AllSites, grant.SiteIDs = true, nil
			require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, grant, 2))
			saved, err := saveReportingInputsForTest(f.svc, t.Context(), actor, f.org, account.AccountID, 1, "authorized removal", input)
			require.NoError(t, err)
			require.EqualValues(t, 2, saved.Revision)
		})
	}
}

func TestReportingAssetFactsRequireMatchingCoverageWindowsAndAttribution(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Reporting client", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, client.AccountID)
	base := reportingCoverageInputs(t, f, client.AccountID, 2)
	c := base.Coverage[0]
	start, end := c.Start.Add(time.Hour), c.Start.Add(4*time.Hour)
	base.Coverage[0].Start, base.Coverage[0].End = start, end
	// A different container at the same current site must still not support this asset.
	_, otherContainer := f.miningModule(t)
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, c.SiteID, otherContainer)
	require.NoError(t, err)
	var revision int64
	hash := reporting.HashrateInterval{ID: "hash", Kind: c.Kind, AssetID: c.AssetID, SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: start, End: end, SourceID: "hash-source", HashrateHS: "100", Complete: true, SourceRef: "hash evidence"}
	obligation := reporting.ObligationInterval{ID: "episode", EpisodeID: "episode", Kind: c.Kind, AssetID: c.AssetID, SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: start, AsOf: end, ReceivedAt: end, SourceRef: "ticket evidence"}
	pool := reporting.PoolObservation{ID: "pool", Kind: c.Kind, AssetID: c.AssetID, SourceID: "pool-source", SourceAt: start, ReceivedAt: end.Add(time.Hour), State: "reporting", SourceRef: "pool evidence"}
	for _, tc := range []struct {
		name string
		edit func(*reporting.Inputs)
	}{
		{"hash_before", func(in *reporting.Inputs) {
			h := hash
			h.Start = start.Add(-time.Minute)
			in.Hashrates = []reporting.HashrateInterval{h}
		}},
		{"hash_after", func(in *reporting.Inputs) {
			h := hash
			h.End = end.Add(time.Minute)
			in.Hashrates = []reporting.HashrateInterval{h}
		}},
		{"hash_site", func(in *reporting.Inputs) {
			h := hash
			h.SiteID = base.Coverage[1].SiteID
			h.ContainerID = base.Coverage[1].ContainerID
			in.Hashrates = []reporting.HashrateInterval{h}
		}},
		{"hash_container", func(in *reporting.Inputs) {
			h := hash
			h.ContainerID = otherContainer
			in.Hashrates = []reporting.HashrateInterval{h}
		}},
		{"hash_order", func(in *reporting.Inputs) {
			h := hash
			h.OrderID = base.Coverage[1].OrderID
			in.Orders[1].Start = in.Orders[0].Start
			in.Hashrates = []reporting.HashrateInterval{h}
		}},
		{"obligation_before", func(in *reporting.Inputs) {
			o := obligation
			o.Start = start.Add(-time.Minute)
			in.Obligations = []reporting.ObligationInterval{o}
		}},
		{"obligation_after", func(in *reporting.Inputs) {
			o := obligation
			o.AsOf = end.Add(time.Minute)
			in.Obligations = []reporting.ObligationInterval{o}
		}},
		{"obligation_site", func(in *reporting.Inputs) {
			o := obligation
			o.SiteID = base.Coverage[1].SiteID
			o.ContainerID = base.Coverage[1].ContainerID
			in.Obligations = []reporting.ObligationInterval{o}
		}},
		{"obligation_order", func(in *reporting.Inputs) {
			o := obligation
			o.OrderID = base.Coverage[1].OrderID
			in.Orders[1].Start = in.Orders[0].Start
			in.Obligations = []reporting.ObligationInterval{o}
		}},
		{"pool_before", func(in *reporting.Inputs) {
			o := pool
			o.SourceAt = start.Add(-time.Minute)
			in.PoolObservations = []reporting.PoolObservation{o}
		}},
		{"pool_at_end", func(in *reporting.Inputs) {
			o := pool
			o.SourceAt = end
			in.PoolObservations = []reporting.PoolObservation{o}
		}},
		{"pool_after", func(in *reporting.Inputs) {
			o := pool
			o.SourceAt = end.Add(time.Minute)
			in.PoolObservations = []reporting.PoolObservation{o}
		}},
		{"hash_gap", func(in *reporting.Inputs) {
			left, right := in.Coverage[0], in.Coverage[0]
			left.End = start.Add(time.Hour)
			right.ID = "second-window"
			right.Start = left.End.Add(time.Minute)
			in.Coverage = append([]reporting.CoverageSegment{left, right}, in.Coverage[1:]...)
			in.Hashrates = []reporting.HashrateInterval{hash}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := base
			input.Coverage = append([]reporting.CoverageSegment(nil), base.Coverage...)
			input.Orders = append([]reporting.Order(nil), base.Orders...)
			tc.edit(&input)
			require.NoError(t, input.Validate(), "fixture passes pure validation")
			proposed, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, revision, "invalid fact attribution", input)
			if err == nil {
				revision = proposed.Revision
			}
			require.Error(t, err, "facts cannot borrow coverage from a different time or attribution")
		})
	}
	// Adjacent coverage segments support a whole interval; an open episode is bounded by AsOf.
	left, right := base.Coverage[0], base.Coverage[0]
	left.End, right.Start, right.ID = start.Add(time.Hour), start.Add(time.Hour), "second-window"
	base.Coverage = append([]reporting.CoverageSegment{right, left}, base.Coverage[1:]...)
	base.Hashrates = []reporting.HashrateInterval{hash}
	base.Obligations = []reporting.ObligationInterval{obligation}
	base.PoolObservations = []reporting.PoolObservation{pool}
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, revision, "matching fact evidence", base)
	require.NoError(t, err, "out-of-order adjacent coverage and delayed receipt remain valid")
}

func TestReportingSavedRemovalDoesNotGateLaterRevisionsOnOldSites(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Reporting client", f.user, hostedaccounts.AllCapabilities)
	actor, id := reportingBoundaryActors(t, f, account.AccountID)
	both := reportingCoverageInputs(t, f, account.AccountID, 2)
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), actor, f.org, account.AccountID, 0, "both sites", both)
	require.NoError(t, err)
	onlyA := both
	onlyA.Coverage = both.Coverage[:1]
	saved, err = saveReportingInputsForTest(f.svc, t.Context(), actor, f.org, account.AccountID, saved.Revision, "authorized removal", onlyA)
	require.NoError(t, err)
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.ReportingAuthority{UserID: id, CanCreate: true, SiteIDs: []int64{onlyA.Coverage[0].SiteID}}, 1))
	next, err := saveReportingInputsForTest(f.svc, t.Context(), actor, f.org, account.AccountID, saved.Revision, "update remaining site", onlyA)
	require.NoError(t, err, "each saved revision replaces the prior live scope immediately")
	require.Equal(t, saved.Revision+1, next.Revision)
}

func TestReportingInputAdmissionPrecedesFactValidation(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Reporting client", f.user, hostedaccounts.AllCapabilities)
	input := reportingInputs(t, f, client.AccountID, f.host)
	input.Configuration.Timezone = "invalid/timezone"
	other := addUser(t, f.db, f.org)
	_, err := saveReportingInputsForTest(f.svc, t.Context(), other, f.org, client.AccountID, 0, "source", input)
	require.ErrorContains(t, err, "account access denied", "missing reporting authority is checked before validating facts")
	reportingBoundaryActors(t, f, client.AccountID)
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "source", input)
	require.ErrorContains(t, err, "timezone", "admitted callers still receive fact validation errors")
}

func TestHostReportingDiscoveryExcludesSuspendedAncestors(t *testing.T) {
	f := setup(t)
	parent := f.client(t, "Parent", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, parent.AccountID, "Subclient", f.user, hostedaccounts.AllCapabilities)
	require.NoError(t, err)
	active := f.client(t, "Unrelated active", f.user, hostedaccounts.AllCapabilities)
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, parent.AccountID, hostedaccounts.AllCapabilities, true))
	accounts, err := f.svc.HostReportingAccounts(t.Context(), f.host, f.org)
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	require.Equal(t, active.AccountID, accounts[0].AccountID)
	for _, a := range accounts {
		require.NotEqual(t, child.AccountID, a.AccountID)
	}
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account SET suspended=true WHERE organization_id=$1 AND depth=0`, f.org)
	require.NoError(t, err)
	accounts, err = f.svc.HostReportingAccounts(t.Context(), f.host, f.org)
	require.NoError(t, err)
	require.Empty(t, accounts, "a suspended root excludes every descendant")
}

func TestReportingReplacementCanRemoveSitesExcludedByCurrentAccountPolicy(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Policy change", f.user, hostedaccounts.AllCapabilities)
	actor, id := reportingBoundaryActors(t, f, account.AccountID)
	var a, b int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'A','a') RETURNING id`, f.org).Scan(&a))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'B','b') RETURNING id`, f.org).Scan(&b))
	baseline := siteOnlyReportingInputs(t, f, account.AccountID, a, b)
	_, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "A and B", baseline)
	require.NoError(t, err)
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.user, f.org, account.AccountID, true, []int64{a}))
	setup, err := f.svc.HostReportingSetup(t.Context(), actor, f.org, account.AccountID)
	require.NoError(t, err)
	require.NotNil(t, setup.Input)
	grant := hostedaccounts.ReportingAuthority{UserID: id, CanCreate: true, SiteIDs: []int64{a}}
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, grant, 1))
	replacement := siteOnlyReportingInputs(t, f, account.AccountID, a)
	_, err = saveReportingInputsForTest(f.svc, t.Context(), actor, f.org, account.AccountID, 1, "remove B", replacement)
	require.Error(t, err, "removing B requires host authority over B")
	grant.AllSites, grant.SiteIDs = true, nil
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, grant, 2))
	_, err = saveReportingInputsForTest(f.svc, t.Context(), actor, f.org, account.AccountID, 1, "remove B", replacement)
	require.NoError(t, err)
	_, err = saveReportingInputsForTest(f.svc, t.Context(), actor, f.org, account.AccountID, 2, "restore B", baseline)
	require.Error(t, err, "new facts still require the client's current site policy")
	var policyError fleeterror.FleetError
	require.ErrorAs(t, err, &policyError)
	require.Equal(t, connect.CodeInvalidArgument, policyError.GRPCCode)
	require.ErrorContains(t, err, fmt.Sprintf("reporting site %d is excluded by the current account site policy", b))
	current, err := reportingInputsForTest(f.svc, t.Context(), actor, f.org, account.AccountID)
	require.NoError(t, err)
	require.EqualValues(t, 2, current.Revision)
	require.Equal(t, replacement.Sites(), current.Inputs.Sites(), "policy rejection preserves the current inputs")
}

func TestReportingAuthorityRetainsDeletedSitesButCannotAddThem(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Retained grants", f.user, hostedaccounts.AllCapabilities)
	_, id := reportingBoundaryActors(t, f, account.AccountID)
	var kept, unrelated int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Kept','kept') RETURNING id`, f.org).Scan(&kept))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Unrelated','unrelated') RETURNING id`, f.org).Scan(&unrelated))
	grant := hostedaccounts.ReportingAuthority{UserID: id, CanCreate: true, SiteIDs: []int64{kept}}
	_, err := f.db.ExecContext(t.Context(), `UPDATE site SET deleted_at=clock_timestamp() WHERE id IN ($1,$2)`, kept, unrelated)
	require.NoError(t, err)
	// Narrowing existing all-site authority introduces no new deleted-site grant.
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, grant, 1))
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, grant, 2))
	grant.CanCreate = false
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, grant, 3))
	grant.SiteIDs = append(grant.SiteIDs, unrelated)
	require.ErrorContains(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, grant, 4), "account access denied")
	currentSetup, err := f.svc.HostReportingSetup(t.Context(), f.host, f.org, account.AccountID)
	require.NoError(t, err)
	found := false
	for _, g := range currentSetup.Authorities {
		if g.UserID == id {
			found = true
			require.EqualValues(t, 4, g.Revision)
			require.Equal(t, []int64{kept}, g.SiteIDs)
			require.False(t, g.CanCreate)
		}
	}
	require.True(t, found, "the disabled grant must retain its historical site after the rejected update")
}

func TestReportingAuditNormalizesEmptyModuleCoverage(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Audit", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	input := reportingInputs(t, f, account.AccountID, f.host)
	_, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "initial", input)
	require.NoError(t, err)
	input.ModuleCoverage = []reporting.ModuleCoverageSegment{}
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 1, "same", input)
	require.NoError(t, err)
	require.EqualValues(t, 2, saved.Revision)
	var changed bool
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT metadata->'changed_sections' ? 'moduleCoverage' FROM activity_log WHERE event_type='save_reporting_inputs' ORDER BY id DESC LIMIT 1`).Scan(&changed))
	require.False(t, changed)
}

func TestReportingAllocationLookupHandlesBoundedBatches(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Batched allocation evidence", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	input := reportingCoverageInputs(t, f, account.AccountID, 1)
	base := input.Coverage[0]
	// Derive retained intervals from a real allocation. Each segment is
	// non-overlapping but has its own historical interval, crossing the lookup
	// batch boundary without constructing 501 independent inventories.
	rows, err := f.db.QueryContext(t.Context(), `
INSERT INTO hosted_account_asset_interval
 (organization_id,kind,asset_id,device_id,site_id,device_identifier,client_id,subclient_id,started_at,ended_at)
SELECT organization_id,kind,asset_id,device_id,site_id,device_identifier,client_id,subclient_id,
       started_at + n * interval '1 second', started_at + (n+1) * interval '1 second'
FROM hosted_account_asset_interval CROSS JOIN generate_series(0,500) n
WHERE id=$1 RETURNING id,started_at,ended_at`, base.AllocationIntervalID)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	input.Coverage = nil
	for rows.Next() {
		c := base
		require.NoError(t, rows.Scan(&c.AllocationIntervalID, &c.Start, &c.End))
		c.ID = fmt.Sprintf("batch-%d", c.AllocationIntervalID)
		input.Coverage = append(input.Coverage, c)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.Len(t, input.Coverage, 501)
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "batched history", input)
	require.NoError(t, err)
	input.Coverage[500].AllocationIntervalID = 9223372036854775807
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, saved.Revision, "missing historical interval", input)
	require.ErrorContains(t, err, "account access denied")
	current, err := reportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID)
	require.NoError(t, err)
	require.EqualValues(t, saved.Revision, current.Revision, "failed validation must not replace the saved revision")
}

func TestReportingDistinctSiteAndContainerBatchesValidateFinalClaim(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Many locations", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	_, controller := f.miningModule(t)
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO site(org_id,name,slug) SELECT $1,'batch-'||n,'batch-'||n FROM generate_series(1,2001) n`, f.org)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO device(device_identifier,mac_address,org_id,discovered_device_id,site_id) SELECT 'batch-device-'||s.id,d.mac_address,d.org_id,d.discovered_device_id,s.id FROM device d CROSS JOIN site s WHERE d.id=$1 AND s.org_id=$2 ORDER BY s.id`, controller, f.org)
	require.NoError(t, err)
	rows, err := f.db.QueryContext(t.Context(), `SELECT site_id,id FROM device WHERE device_identifier LIKE 'batch-device-%' ORDER BY site_id,id`)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	input := reportingInputs(t, f, account.AccountID, f.host)
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for rows.Next() {
		var site, container int64
		require.NoError(t, rows.Scan(&site, &container))
		input.ExternalResults = append(input.ExternalResults, reporting.ContractResult{ID: fmt.Sprint(container), Metric: "availability", SiteID: site, ContainerID: container, Start: start, End: start.Add(time.Hour), Value: "99", Unit: "%", SourceRef: "source", CalculationRef: "calculation"})
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.Len(t, input.ExternalResults, 2001)
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "many locations", input)
	require.NoError(t, err)
	last := input.ExternalResults[2000]
	for _, kind := range []string{"missing-site", "wrong-container-site", "missing-device"} {
		t.Run(kind, func(t *testing.T) {
			input.ExternalResults[2000] = last
			if kind == "missing-site" {
				input.ExternalResults[2000].SiteID = 9223372036854775807
			}
			if kind == "wrong-container-site" {
				input.ExternalResults[2000].ContainerID = input.ExternalResults[0].ContainerID
			}
			if kind == "missing-device" {
				input.ExternalResults[2000].ContainerID = 9223372036854775807
			}
			_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, saved.Revision, "invalid final claim", input)
			require.Error(t, err)
			var rev, audits int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT revision FROM hosted_reporting_inputs WHERE account_id=$1`, account.ID).Scan(&rev))
			require.EqualValues(t, saved.Revision, rev)
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='save_reporting_inputs'`).Scan(&audits))
			require.EqualValues(t, saved.Revision, audits)
		})
	}
}

func TestReportingRepresentativeLookupPreservesDatabaseFailures(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Representatives", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	require.NoError(t, f.svc.SetAncestorGrant(t.Context(), f.host, f.org, account.AccountID, f.root, hostedaccounts.View|hostedaccounts.ManageMembers))
	var member, nonmember string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.user).Scan(&member))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.host).Scan(&nonmember))
	input := reportingInputs(t, f, account.AccountID, f.host)
	input.Configuration.Representatives = []reporting.Representative{{UserID: nonmember, CanSubmit: true, SourceRef: "authority"}}
	_, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "missing representative", input)
	require.ErrorContains(t, err, "representative must be a current direct account teammate")
	input.Configuration.Representatives[0].UserID = member
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "valid representative", input)
	require.NoError(t, err)
	// Preserve the saved recipients so the timed-out operation reaches their
	// validation lookup rather than the separate team-management admission.
	lock, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer lock.Rollback()
	_, err = lock.ExecContext(t.Context(), `LOCK TABLE hosted_account_membership IN ACCESS EXCLUSIVE MODE`)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, e := saveReportingInputsForTest(f.svc, ctx, f.host, f.org, account.AccountID, 1, "timed out representative", input)
		done <- e
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		e := f.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND query LIKE '%FindHostedAccountMemberByUUID%' AND wait_event_type='Lock')`).Scan(&waiting)
		return e == nil && waiting
	}, time.Second, 10*time.Millisecond)
	err = <-done
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	require.Equal(t, connect.CodeInternal, fleetErr.GRPCCode)
	require.NotContains(t, err.Error(), "representative must")
	require.NoError(t, lock.Rollback())
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_reporting_inputs`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='save_reporting_inputs'`).Scan(&count))
	require.Equal(t, 1, count)
	var revision int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT revision FROM hosted_reporting_inputs WHERE account_id=$1`, account.ID).Scan(&revision))
	require.EqualValues(t, 1, revision)
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 1, "valid representative", input)
	require.NoError(t, err)
}

func TestReportingInputRollbackRefusesGrantOnlyState(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Granted", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	down, err := migrations.Current.ReadFile("current/001071_internal_hosted_reporting_inputs.down.sql")
	require.NoError(t, err)
	tx, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(t.Context(), string(down))
	require.ErrorContains(t, err, "preserve reporting inputs")
	require.NoError(t, tx.Rollback())
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_reporting_authority`).Scan(&count))
	require.Equal(t, 2, count)
}

func TestReportingCreationRevocationAndAuthorityRevisionGuards(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Revoked reporting", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	input := reportingInputs(t, f, account.AccountID, f.host)
	_, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "initial", input)
	require.NoError(t, err)
	var hostID string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.host).Scan(&hostID))
	grant := hostedaccounts.ReportingAuthority{UserID: hostID, CanCreate: false, AllSites: true}
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, grant, 1))
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 1, "revoked edit", input)
	require.ErrorContains(t, err, "account access denied")
	_, err = reportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID)
	require.ErrorContains(t, err, "account access denied")
	setup, err := f.svc.HostReportingSetup(t.Context(), f.host, f.org, account.AccountID)
	require.NoError(t, err)
	require.False(t, setup.CanCreate)
	require.Nil(t, setup.Input)
	grant.CanCreate = true
	err = f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, grant, 1)
	var revisionError fleeterror.FleetError
	require.ErrorAs(t, err, &revisionError)
	require.Equal(t, connect.CodeFailedPrecondition, revisionError.GRPCCode, "stale revisions cannot restore creation")
	err = f.svc.SetReportingAuthority(t.Context(), f.user, f.org, account.AccountID, grant, 2)
	require.ErrorContains(t, err, "account access denied", "account membership does not grant account:manage")
	currentSetup, err := f.svc.HostReportingSetup(t.Context(), f.host, f.org, account.AccountID)
	require.NoError(t, err)
	found := false
	for _, authority := range currentSetup.Authorities {
		if authority.UserID == hostID {
			found = true
			require.False(t, authority.CanCreate)
			require.EqualValues(t, 2, authority.Revision)
		}
	}
	require.True(t, found, "revoked reporting authority remains listed")
	var revision, saves int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT revision FROM hosted_reporting_inputs WHERE account_id=$1`, account.ID).Scan(&revision))
	require.EqualValues(t, 1, revision)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='save_reporting_inputs'`).Scan(&saves))
	require.EqualValues(t, 1, saves)
}

func TestReportingAllSitesGrantDoesNotWidenHostSiteRole(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Site-scoped creator", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	q := sqlc.New(f.db)
	allowed, err := q.CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: f.org, Name: "Allowed", Slug: "allowed"})
	require.NoError(t, err)
	hidden, err := q.CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: f.org, Name: "Hidden", Slug: "hidden"})
	require.NoError(t, err)
	role, err := authz.NewService(f.db, nil).CreateCustomRole(t.Context(), f.host, f.org, "Reporting reader", "", []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead})
	require.NoError(t, err)
	actor := addUser(t, f.db, f.org)
	_, err = q.AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: actor, OrganizationID: f.org, RoleID: role.ID, ScopeType: "site", ScopeID: sql.NullInt64{Int64: allowed.ID, Valid: true}})
	require.NoError(t, err)
	var actorID string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, actor).Scan(&actorID))
	grant := hostedaccounts.ReportingAuthority{UserID: actorID, CanCreate: true, AllSites: true}
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, grant, 0))
	input := siteOnlyReportingInputs(t, f, account.AccountID, hidden.ID)
	_, err = saveReportingInputsForTest(f.svc, t.Context(), actor, f.org, account.AccountID, 0, "hidden site", input)
	require.ErrorContains(t, err, "account access denied", "AllSites cannot widen the host's site role")
	require.NotContains(t, err.Error(), fmt.Sprintf("reporting site %d", hidden.ID), "host authorization precedes policy details")
	input = siteOnlyReportingInputs(t, f, account.AccountID, allowed.ID)
	_, err = saveReportingInputsForTest(f.svc, t.Context(), actor, f.org, account.AccountID, 0, "allowed site", input)
	require.NoError(t, err)
	err = f.svc.SetReportingAuthority(t.Context(), actor, f.org, account.AccountID, grant, 1)
	require.ErrorContains(t, err, "account access denied", "site reading does not grant account:manage")
	input = siteOnlyReportingInputs(t, f, account.AccountID, allowed.ID, hidden.ID)
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 1, "both sites", input)
	require.NoError(t, err)
	_, err = reportingInputsForTest(f.svc, t.Context(), actor, f.org, account.AccountID)
	require.ErrorContains(t, err, "account access denied")
	setup, err := f.svc.HostReportingSetup(t.Context(), actor, f.org, account.AccountID)
	require.NoError(t, err)
	require.True(t, setup.CanCreate)
	require.Nil(t, setup.Input)
}
