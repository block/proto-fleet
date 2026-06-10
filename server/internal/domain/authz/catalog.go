// Package authz owns the permission catalog and the role/permission resolver
// that the request middleware uses to enforce access. The catalog defined in
// this file is the single source of truth; the seed migration in
// 000053_seed_builtin_roles.up.sql and the startup reconciliation in
// builtin.go both project from it.
package authz

import "sort"

// Permission keys, grouped by resource. Every constant added here MUST also
// be added to AllPermissions(); the catalog-completeness test enforces this.

const (
	// fleet — collection-level reads. Required floor for any list/dashboard
	// view; the role-save validator rejects any role that includes an
	// action permission without its matching read partner.
	PermFleetRead = "fleet:read"

	// miner — per-row reads and miner-scoped actions. Site-scoped resource.
	PermMinerRead             = "miner:read"
	PermMinerBlinkLED         = "miner:blink_led"
	PermMinerReboot           = "miner:reboot"
	PermMinerStartMining      = "miner:start_mining"
	PermMinerStopMining       = "miner:stop_mining"
	PermMinerUpdatePools      = "miner:update_pools"
	PermMinerUpdateWorkerName = "miner:update_worker_names"
	PermMinerRename           = "miner:rename"
	PermMinerDelete           = "miner:delete"
	PermMinerSetCoolingMode   = "miner:set_cooling_mode"
	PermMinerSetPowerTarget   = "miner:set_power_target"
	PermMinerFirmwareUpdate   = "miner:firmware_update"
	PermMinerDownloadLogs     = "miner:download_logs"
	PermMinerUpdatePassword   = "miner:update_password"
	PermMinerUnpair           = "miner:unpair"
	PermMinerPair             = "miner:pair"
	PermMinerExportCSV        = "miner:export_csv"

	// rack — site-scoped physical organization.
	PermRackRead   = "rack:read"
	PermRackManage = "rack:manage"

	// site — site/building CRUD.
	PermSiteRead   = "site:read"
	PermSiteManage = "site:manage"

	// activity — org audit/event log. Read-only catalog; writes happen
	// implicitly as a side effect of every gated mutation.
	PermActivityRead = "activity:read"

	// serverlog — admin server-side log viewer.
	PermServerlogRead = "serverlog:read"

	// curtailment — top-nav operational page; :ingest is the machine-caller
	// gate for IngestCurtailmentSignal.
	PermCurtailmentRead   = "curtailment:read"
	PermCurtailmentManage = "curtailment:manage"
	PermCurtailmentIngest = "curtailment:ingest"

	// pool — org-level mining pool definitions applied to miners. Not
	// site-scoped: pools are a global org resource.
	PermPoolRead   = "pool:read"
	PermPoolManage = "pool:manage"

	// schedule — recurring miner actions (set_power_target, reboot,
	// sleep). schedule:read gates the list surface; schedule:manage
	// gates create/update/delete/pause/resume/reorder. Create, update,
	// and resume additionally require the underlying miner action
	// permission so a manager can't smuggle a privileged action through
	// the scheduler.
	PermScheduleRead   = "schedule:read"
	PermScheduleManage = "schedule:manage"

	// fleetnode — top-nav admin operations.
	PermFleetnodeRead   = "fleetnode:read"
	PermFleetnodeManage = "fleetnode:manage"

	// apikey — org-admin API key management. No separate :read; the surface
	// lives under route-guarded Settings, so a viewer-only role has no
	// reachable UI.
	PermAPIKeyManage = "apikey:manage"

	// user — org-admin user management.
	PermUserRead   = "user:read"
	PermUserManage = "user:manage"

	// role — custom role + ADMIN/FIELD_TECH editing.
	PermRoleManage = "role:manage"
)

// Resource identifiers used to group catalog entries for the admin UI
// and as the lookup key for the read-pairing rule (every action
// permission requires its same-resource read partner).
const (
	ResourceFleet       = "fleet"
	ResourceMiner       = "miner"
	ResourceRack        = "rack"
	ResourceSite        = "site"
	ResourceActivity    = "activity"
	ResourceServerLog   = "serverlog"
	ResourceCurtailment = "curtailment"
	ResourcePool        = "pool"
	ResourceSchedule    = "schedule"
	ResourceFleetNode   = "fleetnode"
	ResourceAPIKey      = "apikey"
	ResourceUser        = "user"
	ResourceRole        = "role"
)

// CatalogEntry is the in-code shape of a single permission. The wire-level
// Permission proto in authz/v1/authz.proto projects from this. The Resource
// field groups entries for the admin UI's catalog view and lets the
// role-save validator find each action's matching read partner without
// re-parsing the key prefix.
type CatalogEntry struct {
	Key         string
	Description string
	Resource    string
}

// catalog is the canonical list. Order is by resource group then by key,
// chosen to match the order the admin UI renders.
var catalog = []CatalogEntry{
	{PermFleetRead, "View dashboard, miner list, and telemetry. Required floor for any role with miner actions.", ResourceFleet},

	{PermMinerRead, "View miner detail, status snapshot, and error history. Required floor for any miner action permission.", ResourceMiner},
	{PermMinerBlinkLED, "Trigger the locator LED on a miner.", ResourceMiner},
	{PermMinerReboot, "Reboot a miner.", ResourceMiner},
	{PermMinerStartMining, "Start mining on a miner.", ResourceMiner},
	{PermMinerStopMining, "Stop mining on a miner.", ResourceMiner},
	{PermMinerUpdatePools, "Update a miner's pool configuration.", ResourceMiner},
	{PermMinerUpdateWorkerName, "Update worker names on a miner.", ResourceMiner},
	{PermMinerRename, "Rename a miner.", ResourceMiner},
	{PermMinerDelete, "Delete a miner.", ResourceMiner},
	{PermMinerSetCoolingMode, "Change a miner's cooling mode.", ResourceMiner},
	{PermMinerSetPowerTarget, "Change a miner's power target.", ResourceMiner},
	{PermMinerFirmwareUpdate, "Push a firmware update to a miner.", ResourceMiner},
	{PermMinerDownloadLogs, "Download diagnostic logs from a miner.", ResourceMiner},
	{PermMinerUpdatePassword, "Change the miner's device-local web UI password.", ResourceMiner},
	{PermMinerUnpair, "Unpair a miner from the fleet.", ResourceMiner},
	{PermMinerPair, "Pair a new miner into the fleet.", ResourceMiner},
	{PermMinerExportCSV, "Export miner data as CSV.", ResourceMiner},

	{PermRackRead, "List racks at a site.", ResourceRack},
	{PermRackManage, "Create, rename, delete racks and move miners between them.", ResourceRack},

	{PermSiteRead, "View sites and buildings.", ResourceSite},
	{PermSiteManage, "Create, edit, and delete sites and buildings.", ResourceSite},

	{PermActivityRead, "View the organization-wide activity log and export it as CSV.", ResourceActivity},

	{PermServerlogRead, "View server-side logs.", ResourceServerLog},

	{PermCurtailmentRead, "View curtailment status, events, and policies.", ResourceCurtailment},
	{PermCurtailmentManage, "Preview, start, stop, and manage curtailment policies.", ResourceCurtailment},
	{PermCurtailmentIngest, "Accept curtailment dispatch signals from external providers.", ResourceCurtailment},

	{PermPoolRead, "View saved mining pool configurations.", ResourcePool},
	{PermPoolManage, "Create, edit, and delete saved mining pool configurations.", ResourcePool},

	{PermScheduleRead, "View scheduled miner actions.", ResourceSchedule},
	{PermScheduleManage, "Create, edit, pause, resume, and delete scheduled miner actions. Requires the underlying miner action permission to schedule that action.", ResourceSchedule},

	{PermFleetnodeRead, "View fleet-node state.", ResourceFleetNode},
	{PermFleetnodeManage, "Perform fleet-node admin operations.", ResourceFleetNode},

	{PermAPIKeyManage, "List, create, and revoke API keys for the organization.", ResourceAPIKey},

	{PermUserRead, "List users in the organization.", ResourceUser},
	{PermUserManage, "Create, reset, and deactivate users in the organization.", ResourceUser},

	{PermRoleManage, "Create, edit, and delete custom roles. Built-in roles cannot be modified.", ResourceRole},
}

// AllPermissions returns the canonical permission keys in catalog order. The
// slice is a fresh copy on each call so callers can sort it without mutating
// shared state.
func AllPermissions() []string {
	out := make([]string, len(catalog))
	for i, entry := range catalog {
		out[i] = entry.Key
	}
	return out
}

// AllPermissionsSorted returns every permission key in lexicographic order.
// Used by deterministic projections (UserInfo.permissions, test fixtures).
func AllPermissionsSorted() []string {
	out := AllPermissions()
	sort.Strings(out)
	return out
}

// Catalog returns the full catalog. The returned slice is a fresh copy.
func Catalog() []CatalogEntry {
	out := make([]CatalogEntry, len(catalog))
	copy(out, catalog)
	return out
}

// CatalogByResource groups the catalog by resource for UI display.
// Within each resource bucket, entries are in declaration order. Map
// iteration order in Go is not stable, so callers that need a fixed
// resource order should drive iteration from a separate ordered list
// (e.g., the slice returned by ResourceOrder).
func CatalogByResource() map[string][]CatalogEntry {
	groups := make(map[string][]CatalogEntry)
	for _, entry := range catalog {
		groups[entry.Resource] = append(groups[entry.Resource], entry)
	}
	return groups
}

// ResourceOrder returns resource identifiers in the order they were
// declared in the catalog. Pair with CatalogByResource to render
// grouped permissions in a deterministic, declaration-driven order.
func ResourceOrder() []string {
	seen := make(map[string]bool, 16)
	out := make([]string, 0, 16)
	for _, entry := range catalog {
		if seen[entry.Resource] {
			continue
		}
		seen[entry.Resource] = true
		out = append(out, entry.Resource)
	}
	return out
}

// Lookup returns the catalog entry for a key, or false if the key is not
// in the catalog. The role-save validator uses this to confirm every
// requested permission is real before persisting role_permission rows.
func Lookup(key string) (CatalogEntry, bool) {
	for _, entry := range catalog {
		if entry.Key == key {
			return entry, true
		}
	}
	return CatalogEntry{}, false
}
