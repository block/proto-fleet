package hostedaccounts_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

func TestReportingFactPagesLongUnicodeIdentitiesAndBytePrefixes(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Register pages", f.user, hostedaccounts.AllCapabilities)
	actor, uuid := reportingBoundaryActors(t, f, account.AccountID)
	input, _, _ := currentBoardReportingInputs(t, f, account.AccountID)
	c := input.Coverage[0]
	long := strings.Repeat("界", 666)
	input.Orders[0].ID = long
	input.Coverage[0].ID = long
	input.Coverage[0].OrderID = long
	input.Hashrates = nil
	input.PoolObservations = nil
	input.Meters = []reporting.MeterInterval{{ID: long, MeterID: long, SourceID: "meter", SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: long, Start: c.Start, End: c.End, KWh: "1", Quality: "measured", SourceRef: "reading"}}
	saved, e := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "long references", input)
	require.NoError(t, e)
	for offset := 0; offset < 12; offset += 3 {
		changes := []reporting.FactMutation{}
		for i := offset; i < offset+3; i++ {
			changes = append(changes, reportingUpsertForTest(t, reporting.PoolObservation{ID: fmt.Sprintf("pool-%02d", i), AssetID: c.AssetID, Kind: c.Kind, SourceAt: c.Start, ReceivedAt: c.End, State: "unknown", SourceID: "pool", SourceRef: "reading", Reason: strings.Repeat("<&", 25000)}))
		}
		next, e := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, saved.Revision, "large facts", nil, changes)
		require.NoError(t, e)
		saved.ReportingInputsMetadata = next
	}
	cursor := ""
	count := 0
	pages := 0
	for {
		page, e := f.svc.ReportingFactPage(t.Context(), actor, f.org, account.AccountID, reporting.FactPoolObservation, saved.Revision, cursor, 200)
		require.NoError(t, e)
		require.NotEmpty(t, page.Facts)
		wire, e := json.Marshal(page)
		require.NoError(t, e)
		require.LessOrEqual(t, len(wire), hostedaccounts.MaxReportingFactPageBytes)
		for _, fact := range page.Facts {
			_, v, e := reporting.DecodeFact(fact.Kind, fact.Payload)
			require.NoError(t, e)
			observation, ok := v.(reporting.PoolObservation)
			require.True(t, ok)
			require.Equal(t, strings.Repeat("<&", 25000), observation.Reason)
			count++
		}
		pages++
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	require.Equal(t, 12, count)
	require.Greater(t, pages, 1, "encoded-byte budget shortens a page before its row bound")
	page, e := f.svc.ReportingFactPage(t.Context(), actor, f.org, account.AccountID, reporting.FactOrder, saved.Revision, "", 1)
	require.NoError(t, e)
	require.Equal(t, long, page.Facts[0].ID)
	_, e = f.svc.ReportingFactPage(t.Context(), actor, f.org, account.AccountID, reporting.FactOrder, saved.Revision, cursor, 1)
	require.ErrorContains(t, e, "cursor")
	_, e = f.svc.ReportingFactPage(t.Context(), actor, f.org, account.AccountID, reporting.FactOrder, saved.Revision, "not-base64!", 1)
	require.ErrorContains(t, e, "cursor")
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.ReportingAuthority{UserID: uuid, CanCreate: true, SiteIDs: []int64{}}, 1))
	_, e = f.svc.ReportingFactPage(t.Context(), actor, f.org, account.AccountID, reporting.FactOrder, saved.Revision-1, "", 1)
	require.ErrorContains(t, e, "account access denied", "stored-scope denial precedes stale revision")
	_, e = f.svc.MutateReportingInputs(t.Context(), actor, f.org, account.AccountID, saved.Revision-1, "stale denied", &input.Configuration, nil)
	require.ErrorContains(t, e, "account access denied")
}

func TestReportingFactPagesDeletionReinsertionAndCursorBinding(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Revision pages", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	config := reporting.Configuration{Timezone: "UTC", ReportingYearMonth: 1, ReportingYearDay: 1}
	at := time.Date(2026, 1, 1, 0, 0, 0, 1, time.UTC)
	changes := []reporting.FactMutation{}
	for _, id := range []string{"a", "b", "c"} {
		changes = append(changes, reportingUpsertForTest(t, reporting.Order{ID: id, Name: id, Start: at, SourceRef: "signed"}))
	}
	m, e := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, 0, "initial", &config, changes)
	require.NoError(t, e)
	page, e := f.svc.ReportingFactPage(t.Context(), f.host, f.org, account.AccountID, reporting.FactOrder, m.Revision, "", 1)
	require.NoError(t, e)
	require.NotEmpty(t, page.NextCursor)
	deleted, e := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, m.Revision, "delete", nil, []reporting.FactMutation{{Kind: reporting.FactOrder, ID: "b", Delete: true}})
	require.NoError(t, e)
	added, e := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, deleted.Revision, "reinsert", nil, changes[1:2])
	require.NoError(t, e)
	_, e = f.svc.ReportingFactPage(t.Context(), f.host, f.org, account.AccountID, reporting.FactOrder, m.Revision, page.NextCursor, 1)
	require.ErrorContains(t, e, "refresh")
	_, e = f.svc.ReportingFactPage(t.Context(), f.host, f.org, account.AccountID, reporting.FactOrder, added.Revision, page.NextCursor, 1)
	require.ErrorContains(t, e, "cursor")
	other := f.client(t, "Other pages", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, other.AccountID)
	_, e = f.svc.ReportingFactPage(t.Context(), f.host, f.org, other.AccountID, reporting.FactOrder, m.Revision, page.NextCursor, 1)
	require.ErrorContains(t, e, "cursor")
}
