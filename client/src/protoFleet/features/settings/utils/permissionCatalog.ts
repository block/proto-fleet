// Client-side mirror of the server permission catalog
// (server/internal/domain/authz/catalog.go). It powers the role builder's
// grouped checkbox UI and the read-pairing enforcement the server applies on
// save.
//
// TODO(rbac): replace this hand-maintained copy with the catalog fetched from
// AuthzService.GetPermissionCatalog once that RPC ships. The proto messages
// (authz.v1.Permission / PermissionGroup) already exist; only the service and
// its generated client are outstanding. Until then this constant MUST be kept
// in sync with catalog.go — the keys, descriptions, and resource grouping are
// all sourced from there.

export interface CatalogEntry {
  /** Stable key, e.g. "miner:reboot". Matches /^[a-z]+:[a-z_]+$/. */
  key: string;
  description: string;
  /** Resource group ("fleet", "miner", ...) used for UI grouping. */
  resource: string;
}

export interface PermissionGroup {
  resource: string;
  /** Human-readable group heading shown in the role builder. */
  label: string;
  entries: CatalogEntry[];
}

// UI display groups for the role builder. These consolidate the server's
// per-resource grouping into fewer, more scannable buckets. The underlying
// permission keys and the read-pairing logic are unchanged — only the
// visual grouping differs.

// `fleet` is intentionally excluded from RESOURCE_TO_GROUP so `fleet:read`
// never renders as a manually toggleable checkbox in the role builder —
// it is the dependency floor for every miner action and is auto-included
// by `withRequiredReads` whenever a miner action is selected.
/** Maps a catalog entry's `resource` field to a UI group key. */
const RESOURCE_TO_GROUP: Record<string, string> = {
  miner: "miner",
  rack: "infrastructure",
  site: "infrastructure",
  curtailment: "curtailment",
  pool: "pool",
  schedule: "schedule",
  fleetnode: "admin",
  serverlog: "admin",
  activity: "admin",
  apikey: "admin",
  user: "admin",
  role: "admin",
};

const GROUP_LABELS: Record<string, string> = {
  miner: "Miners",
  infrastructure: "Sites, buildings & racks",
  curtailment: "Curtailment",
  pool: "Mining pools",
  schedule: "Schedules",
  admin: "Administration",
};

const GROUP_ORDER = ["miner", "infrastructure", "curtailment", "pool", "schedule", "admin"];

// The canonical catalog, in declaration order. Keep in lockstep with the
// `catalog` slice in catalog.go.
export const PERMISSION_CATALOG: CatalogEntry[] = [
  {
    key: "fleet:read",
    description: "View dashboard, miner list, and telemetry. Required floor for any role with miner actions.",
    resource: "fleet",
  },

  {
    key: "miner:read",
    description:
      "View miner detail, status snapshot, and error history. Required floor for any miner action permission.",
    resource: "miner",
  },
  { key: "miner:blink_led", description: "Trigger the locator LED on a miner.", resource: "miner" },
  { key: "miner:reboot", description: "Reboot a miner.", resource: "miner" },
  { key: "miner:start_mining", description: "Start mining on a miner.", resource: "miner" },
  { key: "miner:stop_mining", description: "Stop mining on a miner.", resource: "miner" },
  { key: "miner:update_pools", description: "Update a miner's pool configuration.", resource: "miner" },
  { key: "miner:update_worker_names", description: "Update worker names on a miner.", resource: "miner" },
  { key: "miner:rename", description: "Rename a miner.", resource: "miner" },
  { key: "miner:delete", description: "Delete a miner.", resource: "miner" },
  { key: "miner:set_cooling_mode", description: "Change a miner's cooling mode.", resource: "miner" },
  { key: "miner:set_power_target", description: "Change a miner's power target.", resource: "miner" },
  { key: "miner:firmware_update", description: "Push a firmware update to a miner.", resource: "miner" },
  { key: "miner:download_logs", description: "Download diagnostic logs from a miner.", resource: "miner" },
  { key: "miner:update_password", description: "Change the miner's device-local web UI password.", resource: "miner" },
  { key: "miner:unpair", description: "Unpair a miner from the fleet.", resource: "miner" },
  { key: "miner:pair", description: "Pair a new miner into the fleet.", resource: "miner" },
  { key: "miner:export_csv", description: "Export miner data as CSV.", resource: "miner" },

  { key: "rack:read", description: "List racks at a site.", resource: "rack" },
  { key: "rack:manage", description: "Create, rename, delete racks and move miners between them.", resource: "rack" },

  { key: "site:read", description: "View sites and buildings.", resource: "site" },
  { key: "site:manage", description: "Create, edit, and delete sites and buildings.", resource: "site" },

  {
    key: "activity:read",
    description: "View the organization-wide activity log and export it as CSV.",
    resource: "activity",
  },

  { key: "serverlog:read", description: "View server-side logs.", resource: "serverlog" },

  { key: "curtailment:read", description: "View curtailment policies and preview impact.", resource: "curtailment" },
  { key: "curtailment:manage", description: "Create, edit, and delete curtailment policies.", resource: "curtailment" },
  {
    key: "curtailment:ingest",
    description: "Accept curtailment dispatch signals from external providers (QSE bridge, aggregator, OpenADR VTN).",
    resource: "curtailment",
  },

  { key: "pool:read", description: "View saved mining pool configurations.", resource: "pool" },
  { key: "pool:manage", description: "Create, edit, and delete saved mining pool configurations.", resource: "pool" },

  { key: "schedule:read", description: "View scheduled miner actions.", resource: "schedule" },
  {
    key: "schedule:manage",
    description:
      "Create, edit, pause, resume, and delete scheduled miner actions. Requires the underlying miner action permission to schedule that action.",
    resource: "schedule",
  },

  { key: "fleetnode:read", description: "View fleet-node state.", resource: "fleetnode" },
  { key: "fleetnode:manage", description: "Perform fleet-node admin operations.", resource: "fleetnode" },

  { key: "apikey:manage", description: "List, create, and revoke API keys for the organization.", resource: "apikey" },

  { key: "user:read", description: "List users in the organization.", resource: "user" },
  { key: "user:manage", description: "Create, reset, and deactivate users in the organization.", resource: "user" },

  {
    key: "role:manage",
    description: "Create, edit, and delete custom roles. Built-in roles cannot be modified.",
    resource: "role",
  },
];

/** True for catalog keys whose action segment is "read". */
export const isReadKey = (key: string): boolean => key.endsWith(":read");

/** The catalog grouped for the role builder UI. */
export const permissionGroups: PermissionGroup[] = GROUP_ORDER.flatMap((group) => {
  const entries = PERMISSION_CATALOG.filter((entry) => RESOURCE_TO_GROUP[entry.resource] === group);
  if (entries.length === 0) return [];
  return [{ resource: group, label: GROUP_LABELS[group] ?? group, entries }];
});

const READ_KEY_BY_RESOURCE = new Map<string, string>(
  PERMISSION_CATALOG.filter((entry) => isReadKey(entry.key)).map((entry) => [entry.resource, entry.key]),
);

/**
 * The read permissions a given action key depends on, mirroring the
 * server's role-save validator: every action permission requires its
 * same-resource read partner, and any miner action additionally requires
 * the fleet:read floor.
 */
export const requiredReadsFor = (key: string): string[] => {
  if (isReadKey(key)) return [];

  const entry = PERMISSION_CATALOG.find((e) => e.key === key);
  if (!entry) return [];

  const reads = new Set<string>();
  const sameResourceRead = READ_KEY_BY_RESOURCE.get(entry.resource);
  if (sameResourceRead) reads.add(sameResourceRead);
  // fleet:read is the floor for any role that grants a miner action.
  if (entry.resource === "miner") reads.add("fleet:read");

  return [...reads];
};

/**
 * Expands a selection to include every read permission its action keys
 * depend on. Apply this whenever the user toggles an action on so the
 * UI never holds a selection the server would reject on save.
 */
export const withRequiredReads = (selected: Iterable<string>): string[] => {
  const result = new Set(selected);
  for (const key of [...result]) {
    for (const read of requiredReadsFor(key)) result.add(read);
  }
  return [...result];
};

/**
 * Read keys that are still required by some other selected action, so the
 * UI can keep them locked rather than letting the user clear a dependency.
 */
export const lockedReadKeys = (selected: Iterable<string>): Set<string> => {
  const selectedSet = new Set(selected);
  const locked = new Set<string>();
  for (const key of selectedSet) {
    for (const read of requiredReadsFor(key)) {
      if (selectedSet.has(read)) locked.add(read);
    }
  }
  return locked;
};
