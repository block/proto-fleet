package hostedaccounts_test

import (
	"database/sql"
	"slices"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

// Account managers can grant themselves reporting authority, so they hold it
// implicitly: no stored grant is needed to prepare reports, own them and
// receive their notifications.
func TestAccountManagersHoldImplicitReportingAuthority(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Implicit authority", f.user, hostedaccounts.AllCapabilities)
	var hostUUID, hostUsername string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id,username FROM "user" WHERE id=$1`, f.host).Scan(&hostUUID, &hostUsername))

	accounts, err := f.svc.HostReportingAccounts(t.Context(), f.host, f.org)
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	require.True(t, accounts[0].CanCreate)

	reportingSetup, err := f.svc.HostReportingSetup(t.Context(), f.host, f.org, client.AccountID)
	require.NoError(t, err)
	require.True(t, reportingSetup.CanCreate)
	require.True(t, reportingSetup.CanReadRequests)
	require.True(t, slices.ContainsFunc(reportingSetup.Owners, func(owner hostedaccounts.ReportingAuthority) bool {
		return owner.UserID == hostUUID
	}), "managers are offered as report owners")

	savedReportInputs(t, f, client.AccountID, f.host, reportingInputs(t, f, client.AccountID, f.host), 0)
	_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.DraftReportOptions{Period: reportPeriod(), InputRevision: 1, OwnerUserID: hostUUID})
	require.NoError(t, err)
	var grants int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_reporting_authority WHERE organization_id=$1`, f.org).Scan(&grants))
	require.Zero(t, grants, "authority stays implicit")

	now := time.Now().UTC().Round(time.Microsecond)
	require.NoError(t, hostedaccounts.NewReportingJobs(f.svc, nil).Tick(t.Context(), now))
	_, err = sqlc.New(f.db).ClaimHostedReportQueueNotification(t.Context(), sqlc.ClaimHostedReportQueueNotificationParams{NowAt: now, ClaimToken: sql.NullString{String: "manager", Valid: true}, LeaseUntil: sql.NullTime{Time: now.Add(time.Minute), Valid: true}})
	require.NoError(t, err, "a manager-owned queue item routes its notification")
	queue, err := f.svc.HostReportQueue(t.Context(), f.host, f.org, 100, "")
	require.NoError(t, err)
	require.NotEmpty(t, queue.Items)
	require.Equal(t, hostUsername, queue.Items[0].OwnerUsername)

	// Users without account management still need an explicit grant.
	other := addUser(t, f.db, f.org)
	_, err = f.svc.HostReportingSetup(t.Context(), other, f.org, client.AccountID)
	require.ErrorContains(t, err, "account access denied")

	// A stored grant overrides implicit authority, so managers can still be revoked.
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportingAuthority{UserID: hostUUID, AllSites: true}, 0))
	reportingSetup, err = f.svc.HostReportingSetup(t.Context(), f.host, f.org, client.AccountID)
	require.NoError(t, err)
	require.False(t, reportingSetup.CanCreate)
	require.False(t, slices.ContainsFunc(reportingSetup.Owners, func(owner hostedaccounts.ReportingAuthority) bool {
		return owner.UserID == hostUUID
	}), "a revoked manager is no longer offered as an owner")
}
