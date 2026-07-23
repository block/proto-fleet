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

	// miner channel — desired-state cells and rig reservations.
	PermMinerChannelRead   = "miner_channel:read"
	PermMinerChannelManage = "miner_channel:manage"

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

	PermAlertRead   = "alert:read"
	PermAlertManage = "alert:manage"

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
	ResourceFleet        = "fleet"
	ResourceMiner        = "miner"
	ResourceRack         = "rack"
	ResourceSite         = "site"
	ResourceActivity     = "activity"
	ResourceServerLog    = "serverlog"
	ResourceCurtailment  = "curtailment"
	ResourceMinerChannel = "miner_channel"
	ResourcePool         = "pool"
	ResourceSchedule     = "schedule"
	ResourceFleetNode    = "fleetnode"
	ResourceAlert        = "alert"
	ResourceAPIKey       = "apikey"
	ResourceUser         = "user"
	ResourceRole         = "role"
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
	{PermFleetRead, "View the dashboard, live telemetry, and fleet diagnostics.", ResourceFleet},

	{PermMinerRead, "View a miner's details, status, and the fleet-wide miner list. Required for every other miner action.", ResourceMiner},
	{PermMinerBlinkLED, "Flash a miner's locator LED.", ResourceMiner},
	{PermMinerReboot, "Reboot a miner.", ResourceMiner},
	{PermMinerStartMining, "Start mining on a miner.", ResourceMiner},
	{PermMinerStopMining, "Stop mining on a miner.", ResourceMiner},
	{PermMinerUpdatePools, "Change a miner's mining pool configuration.", ResourceMiner},
	{PermMinerUpdateWorkerName, "Change a miner's worker names.", ResourceMiner},
	{PermMinerRename, "Rename a miner.", ResourceMiner},
	{PermMinerDelete, "Delete a miner from the fleet, unpairing the physical device.", ResourceMiner},
	{PermMinerSetCoolingMode, "Change a miner's cooling mode.", ResourceMiner},
	{PermMinerSetPowerTarget, "Change a miner's power target.", ResourceMiner},
	{PermMinerFirmwareUpdate, "Install firmware on a miner. Installs can reboot the device, so this also needs reboot access.", ResourceMiner},
	{PermMinerDownloadLogs, "Download diagnostic logs from a miner.", ResourceMiner},
	{PermMinerUpdatePassword, "Change a miner's local web interface password.", ResourceMiner},
	{PermMinerUnpair, "Unpair a miner from the fleet.", ResourceMiner},
	{PermMinerPair, "Discover, pair, and bulk-import miners into the fleet.", ResourceMiner},
	{PermMinerExportCSV, "Export the miner list as CSV.", ResourceMiner},

	{PermRackRead, "View racks and device groups, including their members and layout.", ResourceRack},
	{PermRackManage, "Create, edit, and delete racks and device groups, and move miners between them.", ResourceRack},

	{PermSiteRead, "View sites and buildings.", ResourceSite},
	{PermSiteManage, "Create, edit, and delete sites and buildings, and assign racks and miners to them.", ResourceSite},

	{PermActivityRead, "View the organization-wide activity log and export it as CSV.", ResourceActivity},

	{PermServerlogRead, "View recent server logs across all organizations.", ResourceServerLog},

	{PermCurtailmentRead, "View curtailment events and active status.", ResourceCurtailment},
	{PermCurtailmentManage, "Preview, start, stop, and override curtailment events, and manage profiles, automation rules, and dispatch sources.", ResourceCurtailment},
	{PermCurtailmentIngest, "Accept external curtailment dispatch signals. Not yet implemented.", ResourceCurtailment},

	{PermMinerChannelRead, "View miner channels, reservations, and effective desired state.", ResourceMinerChannel},
	{PermMinerChannelManage, "Create, release, and manage miner channels and miner channel memberships.", ResourceMinerChannel},

	{PermPoolRead, "View saved mining pool configurations.", ResourcePool},
	{PermPoolManage, "Create, edit, delete, and validate saved mining pool configurations. Validation makes the server connect to a supplied address.", ResourcePool},

	{PermScheduleRead, "View scheduled miner actions.", ResourceSchedule},
	{PermScheduleManage, "Create, edit, pause, resume, reorder, and delete scheduled miner actions. Scheduling an action also needs permission to perform it.", ResourceSchedule},

	{PermFleetnodeRead, "View fleet nodes, their paired miners, and discovered unpaired devices.", ResourceFleetNode},
	{PermFleetnodeManage, "Enroll, confirm, and revoke fleet nodes, pair miners on them, and run network scans.", ResourceFleetNode},

	{PermAlertRead, "View alert channels, rules, maintenance windows, and alert history.", ResourceAlert},
	{PermAlertManage, "Create, edit, test, and delete alert channels; create, edit, delete, pause, and resume alert rules; create, edit, and delete maintenance windows.", ResourceAlert},

	{PermAPIKeyManage, "List, create, and revoke API keys for the organization.", ResourceAPIKey},

	{PermUserRead, "List users in the organization.", ResourceUser},
	{PermUserManage, "Create, deactivate, reset passwords, and reassign roles for users in the organization.", ResourceUser},

	{PermRoleManage, "Create, edit, and delete custom roles, and view all roles and permissions. Built-in roles cannot be modified.", ResourceRole},
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
