package hostedaccounts_test

import (
	"fmt"
	"strings"
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestAccountAssetTableMetricsSortingAndRedaction(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	slow := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	fast := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	unknown := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	module, controller := f.miningModule(t)
	for _, item := range []struct {
		id          int64
		name, model string
	}{{slow, "Bravo", "B"}, {fast, "Alpha", "A"}, {unknown, "Charlie", "C"}, {controller, "Zulu", "Controller model"}} {
		_, err := f.db.ExecContext(t.Context(), `UPDATE device SET custom_name=$1 WHERE id=$2`, item.name, item.id)
		require.NoError(t, err)
		_, err = f.db.ExecContext(t.Context(), `UPDATE discovered_device SET model=$1 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$2)`, item.model, item.id)
		require.NoError(t, err)
	}
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, slow, 0), selected(5, fast, 0), selected(5, unknown, 0), selected(6, module, 0)))
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w,temp_c,efficiency_jh) SELECT now(),device_identifier,CASE WHEN id=$1 THEN 200e12 ELSE 100e12 END,CASE WHEN id=$1 THEN 4000 ELSE 1000 END,CASE WHEN id=$1 THEN -5 ELSE 60 END,CASE WHEN id=$1 THEN 20e-12 ELSE 10e-12 END FROM device WHERE id IN ($1,$2)`, slow, fast)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE container_module_latest_metrics SET temperature_c=45,efficiency_jh=30e-12,coverage=jsonb_set(coverage,'{temperature}',jsonb_build_object('reporting',2,'expected',2,'topology_complete',true,'oldest_at',now())) WHERE module_id=$1`, module)
	require.NoError(t, err)
	f.publishCurrentModule(t, module)
	ref := func(kind int, id int64) string { return fmt.Sprintf("%d:%d", kind, id) }
	slowRef, fastRef, unknownRef, moduleRef := ref(5, slow), ref(5, fast), ref(5, unknown), ref(6, module)
	list := func(field commonpb.SortField, descending bool) []*pb.AccountAsset {
		t.Helper()
		direction := commonpb.SortDirection_SORT_DIRECTION_ASC
		if descending {
			direction = commonpb.SortDirection_SORT_DIRECTION_DESC
		}
		request := &pb.ListAccountAssetsRequest{AccountId: account.AccountID, PageSize: 1, Sort: &commonpb.SortConfig{Field: field, Direction: direction}}
		var result []*pb.AccountAsset
		for range 5 {
			response, err := f.svc.ListAssets(t.Context(), f.user, f.org, request)
			require.NoError(t, err)
			result = append(result, response.Assets...)
			if response.NextPageToken == "" {
				return result
			}
			request.PageToken = response.NextPageToken
		}
		t.Fatal("pagination did not terminate")
		return nil
	}
	for _, test := range []struct {
		name       string
		field      commonpb.SortField
		descending bool
		expected   []string
	}{
		{"name", commonpb.SortField_SORT_FIELD_NAME, false, []string{fastRef, slowRef, unknownRef, moduleRef}},
		{"hashrate with tied values and null", commonpb.SortField_SORT_FIELD_HASHRATE, true, []string{slowRef, fastRef, moduleRef, unknownRef}},
		{"power ascending", commonpb.SortField_SORT_FIELD_POWER, false, []string{fastRef, moduleRef, slowRef, unknownRef}},
		{"power descending", commonpb.SortField_SORT_FIELD_POWER, true, []string{slowRef, moduleRef, fastRef, unknownRef}},
		{"temperature including negative readings", commonpb.SortField_SORT_FIELD_TEMPERATURE, false, []string{slowRef, moduleRef, fastRef, unknownRef}},
		{"complete-coverage efficiency", commonpb.SortField_SORT_FIELD_EFFICIENCY, false, []string{fastRef, slowRef, unknownRef, moduleRef}},
		{"model ascending", commonpb.SortField_SORT_FIELD_MODEL, false, []string{fastRef, slowRef, unknownRef, moduleRef}},
		{"model descending", commonpb.SortField_SORT_FIELD_MODEL, true, []string{unknownRef, slowRef, fastRef, moduleRef}},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows := list(test.field, test.descending)
			var actual []string
			for _, row := range rows {
				actual = append(actual, ref(int(row.Kind), row.Id))
			}
			require.Equal(t, test.expected, actual)
		})
	}
	rows := list(commonpb.SortField_SORT_FIELD_NAME, false)
	require.Equal(t, "A", rows[0].Model)
	require.NotNil(t, rows[0].TemperatureC)
	require.NotNil(t, rows[0].EfficiencyJTh)
	require.Equal(t, 60.0, *rows[0].TemperatureC)
	require.InDelta(t, 10.0, *rows[0].EfficiencyJTh, 0.00001)
	require.Empty(t, rows[3].Model, "a module must not inherit its controller model")
	require.NotNil(t, rows[3].TemperatureC)
	require.Nil(t, rows[3].EfficiencyJTh, "partial module coverage invalidates per-asset efficiency")
	require.Equal(t, 45.0, *rows[3].TemperatureC)

	require.NotNil(t, rows[3].Coverage)
	require.EqualValues(t, 1, rows[3].Coverage.Hashrate.Reporting)
	require.EqualValues(t, 2, rows[3].Coverage.Hashrate.Expected)
	first, err := f.svc.ListAssets(t.Context(), f.user, f.org, &pb.ListAccountAssetsRequest{AccountId: account.AccountID, PageSize: 1, Sort: &commonpb.SortConfig{Field: commonpb.SortField_SORT_FIELD_POWER}})
	require.NoError(t, err)
	for _, sort := range []*commonpb.SortConfig{{Field: commonpb.SortField_SORT_FIELD_MODEL}, {Field: commonpb.SortField_SORT_FIELD_POWER, Direction: commonpb.SortDirection_SORT_DIRECTION_DESC}} {
		_, err = f.svc.ListAssets(t.Context(), f.user, f.org, &pb.ListAccountAssetsRequest{AccountId: account.AccountID, PageToken: first.NextPageToken, Sort: sort})
		require.Error(t, err, "page tokens must be bound to the sort field and direction")
	}
	_, err = f.svc.ListAssets(t.Context(), f.user, f.org, &pb.ListAccountAssetsRequest{AccountId: account.AccountID, Sort: &commonpb.SortConfig{Field: commonpb.SortField_SORT_FIELD_IP_ADDRESS}})
	require.Error(t, err)
	_, err = f.svc.ListAssets(t.Context(), f.user, f.org, &pb.ListAccountAssetsRequest{AccountId: account.AccountID, SelectedAssets: []*pb.MiningAssetReference{{Kind: 5, Id: fast}}, Sort: &commonpb.SortConfig{Field: commonpb.SortField_SORT_FIELD_NAME}})
	require.Error(t, err, "exact selection must not silently ignore sort settings")
	// A cursor includes the search scope and the sorted name. Both can contain
	// valid characters that expand under JSON escaping.
	longSearch := strings.Repeat("<", 254)
	_, err = f.db.ExecContext(t.Context(), `UPDATE device SET custom_name=$1 || CASE WHEN id=$2 THEN 'A' ELSE 'B' END WHERE id IN ($2,$3)`, longSearch, fast, slow)
	require.NoError(t, err)
	longPage, err := f.svc.ListAssets(t.Context(), f.user, f.org, &pb.ListAccountAssetsRequest{AccountId: account.AccountID, PageSize: 1, Search: longSearch})
	require.NoError(t, err)
	require.Greater(t, len(longPage.NextPageToken), 2048)
	require.LessOrEqual(t, len(longPage.NextPageToken), 8192)
	nextLong, err := f.svc.ListAssets(t.Context(), f.user, f.org, &pb.ListAccountAssetsRequest{AccountId: account.AccountID, PageSize: 1, Search: longSearch, PageToken: longPage.NextPageToken})
	require.NoError(t, err)
	require.Len(t, nextLong.Assets, 1)
	require.NotEqual(t, longPage.Assets[0].Id, nextLong.Assets[0].Id)
	require.Empty(t, nextLong.NextPageToken)
	filteredRequests := []struct {
		name    string
		request *pb.ListAccountAssetsRequest
		kind    pb.MiningAssetKind
		id      int64
	}{
		{"container", &pb.ListAccountAssetsRequest{AccountId: account.AccountID, ContainerId: &controller}, pb.MiningAssetKind_MINING_ASSET_KIND_MODULE, module},
		{"name search", &pb.ListAccountAssetsRequest{AccountId: account.AccountID, Search: longSearch + "A"}, pb.MiningAssetKind_MINING_ASSET_KIND_MINER, fast},
	}
	for _, test := range filteredRequests {
		response, err := f.svc.ListAssets(t.Context(), f.user, f.org, test.request)
		require.NoError(t, err)
		require.Len(t, response.Assets, 1, "%s must match before physical details are redacted", test.name)
		require.Equal(t, test.kind, response.Assets[0].Kind)
		require.Equal(t, test.id, response.Assets[0].Id)
	}
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.user, f.org, account.AccountID, true, nil))
	for _, test := range filteredRequests {
		response, err := f.svc.ListAssets(t.Context(), f.user, f.org, test.request)
		require.NoError(t, err)
		require.Empty(t, response.Assets, "%s must not reveal retained assets through redacted physical details", test.name)
		require.Empty(t, response.NextPageToken)
	}
	ascending := list(commonpb.SortField_SORT_FIELD_MODEL, false)
	require.Len(t, ascending, 4, "unfiltered inventory must retain the redacted allocations")
	descending := list(commonpb.SortField_SORT_FIELD_MODEL, true)
	for i, row := range ascending {
		require.Equal(t, ref(int(row.Kind), row.Id), ref(int(descending[i].Kind), descending[i].Id), "redacted values cannot affect sort order")
		require.Empty(t, row.Name)
		require.Zero(t, row.ContainerId)
		require.Empty(t, row.ContainerName)
		require.Empty(t, row.Model)
		require.Nil(t, row.TemperatureC)
		require.Nil(t, row.EfficiencyJTh)
		require.Nil(t, row.Coverage)
	}
}

func TestAccountAssetFiltersCoverAuthorizedInventoryBeforePagination(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	sibling := f.client(t, "Other client", f.host, hostedaccounts.AllCapabilities)
	q := sqlc.New(f.db)
	north, err := q.CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: f.org, Name: "North", Slug: "north"})
	require.NoError(t, err)
	south, err := q.CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: f.org, Name: "South", Slug: "south"})
	require.NoError(t, err)
	first := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	second := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	hidden := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	module, controller := f.miningModule(t)
	for _, item := range []struct {
		id, site    int64
		name, model string
	}{
		{first, north.ID, "Alpha", "S21"}, {second, south.ID, "Bravo", "S19"},
		{hidden, south.ID, "Private", "Secret model"}, {controller, south.ID, "Zulu container", "Controller"},
	} {
		_, err = f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1,custom_name=$2 WHERE id=$3`, item.site, item.name, item.id)
		require.NoError(t, err)
		_, err = f.db.ExecContext(t.Context(), `UPDATE discovered_device SET model=$1 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$2)`, item.model, item.id)
		require.NoError(t, err)
	}
	f.observeModuleAtCurrentSite(t, module, controller)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, 1, selected(5, first, 0), selected(5, second, 0), selected(6, module, 0)))
	require.NoError(t, f.allocate(t, f.host, f.root, sibling.AccountID, 1, selected(5, hidden, 0)))
	list := func(request *pb.ListAccountAssetsRequest) *pb.ListAccountAssetsResponse {
		t.Helper()
		request.AccountId = account.AccountID
		result, err := f.svc.ListAssets(t.Context(), f.user, f.org, request)
		require.NoError(t, err)
		return result
	}
	page := list(&pb.ListAccountAssetsRequest{PageSize: 1})
	require.Len(t, page.Assets, 1)
	require.Equal(t, first, page.Assets[0].Id)
	require.NotEmpty(t, page.NextPageToken)
	require.Equal(t, []string{"S19", "S21"}, page.FilterOptions.Models)
	require.Len(t, page.FilterOptions.Sites, 2)
	require.Equal(t, controller, page.FilterOptions.Containers[0].Id, "options must include containers beyond the visible page")
	require.Equal(t, "Zulu container", page.FilterOptions.Containers[0].Name)
	for _, test := range []struct {
		request *pb.ListAccountAssetsRequest
		ids     []int64
	}{
		{&pb.ListAccountAssetsRequest{Model: proto.String("S19")}, []int64{second}},
		{&pb.ListAccountAssetsRequest{SiteId: &north.ID}, []int64{first}},
		{&pb.ListAccountAssetsRequest{SiteId: &south.ID, ContainerId: &controller}, []int64{module}},
		{&pb.ListAccountAssetsRequest{SiteId: &north.ID, Model: proto.String("S19")}, nil},
		{&pb.ListAccountAssetsRequest{Model: proto.String("Secret model")}, nil},
	} {
		result := list(test.request)
		var ids []int64
		for _, asset := range result.Assets {
			ids = append(ids, asset.Id)
		}
		require.Equal(t, test.ids, ids)
		require.True(t, proto.Equal(page.FilterOptions, result.FilterOptions), "options must remain complete even when filters match no rows")
	}
	for _, request := range []*pb.ListAccountAssetsRequest{
		{Model: proto.String("S19"), PageToken: page.NextPageToken},
		{SiteId: &north.ID, PageToken: page.NextPageToken},
		{Model: proto.String("S19"), SelectedAssets: []*pb.MiningAssetReference{{Kind: 5, Id: second}}},
		{SiteId: &south.ID, SelectedAssets: []*pb.MiningAssetReference{{Kind: 5, Id: second}}},
		{SiteId: proto.Int64(-1)}, {Model: proto.String("")},
	} {
		request.AccountId = account.AccountID
		_, err = f.svc.ListAssets(t.Context(), f.user, f.org, request)
		require.Error(t, err, "filters must be validated and bound to pagination scope")
	}
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.user, f.org, account.AccountID, true, []int64{north.ID}))
	restricted := list(&pb.ListAccountAssetsRequest{})
	require.Len(t, restricted.Assets, 3, "retain redacted allocations in unfiltered inventory")
	require.Equal(t, []string{"S21"}, restricted.FilterOptions.Models)
	require.Len(t, restricted.FilterOptions.Sites, 1)
	require.Equal(t, north.ID, restricted.FilterOptions.Sites[0].Id)
	require.Empty(t, restricted.FilterOptions.Containers)
	require.Empty(t, list(&pb.ListAccountAssetsRequest{Model: proto.String("S19")}).Assets)
	require.Empty(t, list(&pb.ListAccountAssetsRequest{SiteId: &south.ID}).Assets)
}
