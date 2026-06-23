package authz

import (
	"reflect"
	"regexp"
	"sort"
	"testing"
)

var permKeyRegex = regexp.MustCompile(`^[a-z]+:[a-z_]+$`)

func TestAllPermissions_NoDuplicates(t *testing.T) {
	seen := make(map[string]bool)
	for _, key := range AllPermissions() {
		if seen[key] {
			t.Errorf("duplicate permission key in catalog: %q", key)
		}
		seen[key] = true
	}
}

func TestAllPermissions_KeyShape(t *testing.T) {
	for _, key := range AllPermissions() {
		if !permKeyRegex.MatchString(key) {
			t.Errorf("permission key %q does not match %s", key, permKeyRegex)
		}
	}
}

// TestCatalogCompleteness fails when a new permission constant is added but
// not registered in the catalog slice — the canonical "did you remember to
// add it to AllPermissions" check. It works by enumerating every exported
// string constant on this package starting with Perm and asserting each
// value is reachable via Lookup.
func TestCatalogCompleteness(t *testing.T) {
	expectedKeys := []string{
		PermFleetRead,
		PermMinerRead,
		PermMinerBlinkLED,
		PermMinerReboot,
		PermMinerStartMining,
		PermMinerStopMining,
		PermMinerUpdatePools,
		PermMinerUpdateWorkerName,
		PermMinerRename,
		PermMinerDelete,
		PermMinerSetCoolingMode,
		PermMinerSetPowerTarget,
		PermMinerFirmwareUpdate,
		PermMinerDownloadLogs,
		PermMinerUpdatePassword,
		PermMinerUnpair,
		PermMinerPair,
		PermMinerExportCSV,
		PermRackRead,
		PermRackManage,
		PermSiteRead,
		PermSiteManage,
		PermActivityRead,
		PermServerlogRead,
		PermCurtailmentRead,
		PermCurtailmentManage,
		PermCurtailmentIngest,
		PermCohortRead,
		PermCohortManage,
		PermPoolRead,
		PermPoolManage,
		PermScheduleRead,
		PermScheduleManage,
		PermFleetnodeRead,
		PermFleetnodeManage,
		PermAlertRead,
		PermAlertManage,
		PermAPIKeyManage,
		PermUserRead,
		PermUserManage,
		PermRoleManage,
	}

	for _, key := range expectedKeys {
		if _, ok := Lookup(key); !ok {
			t.Errorf("permission %q declared as a constant but missing from catalog", key)
		}
	}

	got := AllPermissionsSorted()
	want := append([]string(nil), expectedKeys...)
	sort.Strings(want)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("catalog drift:\n  got:  %v\n  want: %v", got, want)
	}
}

func TestAllPermissions_FreshCopy(t *testing.T) {
	a := AllPermissions()
	a[0] = "tampered"

	b := AllPermissions()
	if b[0] == "tampered" {
		t.Fatal("AllPermissions returned a shared backing array; caller mutation leaked into subsequent calls")
	}
}

func TestCatalog_FreshCopy(t *testing.T) {
	a := Catalog()
	a[0].Key = "tampered"

	b := Catalog()
	if b[0].Key == "tampered" {
		t.Fatal("Catalog returned a shared backing array; caller mutation leaked into subsequent calls")
	}
}

func TestCatalogByResource_GroupsAndAssociates(t *testing.T) {
	groups := CatalogByResource()

	for _, resource := range []string{
		ResourceFleet, ResourceMiner, ResourceRack, ResourceSite, ResourceActivity,
		ResourceServerLog, ResourceCurtailment, ResourceCohort, ResourcePool, ResourceSchedule, ResourceFleetNode,
		ResourceAlert, ResourceAPIKey, ResourceUser, ResourceRole,
	} {
		if len(groups[resource]) == 0 {
			t.Errorf("resource %q has no permissions in catalog", resource)
		}
		for _, entry := range groups[resource] {
			if entry.Resource != resource {
				t.Errorf("entry %q grouped under %q but its Resource field is %q", entry.Key, resource, entry.Resource)
			}
		}
	}
}

func TestLookup_UnknownKeyReturnsFalse(t *testing.T) {
	if _, ok := Lookup("does:not_exist"); ok {
		t.Fatal("Lookup returned ok for an unknown key")
	}
}

func TestResourceOrder_MatchesCatalogDeclarationOrder(t *testing.T) {
	got := ResourceOrder()
	want := []string{
		ResourceFleet,
		ResourceMiner,
		ResourceRack,
		ResourceSite,
		ResourceActivity,
		ResourceServerLog,
		ResourceCurtailment,
		ResourceCohort,
		ResourcePool,
		ResourceSchedule,
		ResourceFleetNode,
		ResourceAlert,
		ResourceAPIKey,
		ResourceUser,
		ResourceRole,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ResourceOrder mismatch:\n  got:  %v\n  want: %v", got, want)
	}
}

func TestResourceOrder_NoDuplicates(t *testing.T) {
	seen := make(map[string]bool)
	for _, r := range ResourceOrder() {
		if seen[r] {
			t.Errorf("duplicate resource in ResourceOrder: %q", r)
		}
		seen[r] = true
	}
}
