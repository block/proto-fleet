import type { ActivityEntry } from "@/protoFleet/api/generated/activity/v1/activity_pb";
import { baseEventType, isCompletedEvent } from "@/protoFleet/features/activity/utils/eventType";
import { formatLabel } from "@/protoFleet/features/activity/utils/formatLabel";

type ActivityMetadata = Record<string, unknown>;

const completedCommandDescriptions: Record<string, string> = {
  start_mining: "Started mining",
  stop_mining: "Stopped mining",
  reboot: "Rebooted miners",
  blink_led: "Blinked LEDs",
  download_logs: "Downloaded logs",
  set_power_target: "Updated power target",
  set_cooling_mode: "Updated cooling mode",
  update_mining_pools: "Updated mining pools",
  update_miner_password: "Updated miner password",
  firmware_update: "Updated firmware",
  unpair: "Unpaired miners",
  curtail: "Started curtailment",
  uncurtail: "Ended curtailment",
};

const startedCommandDescriptions: Record<string, string> = {
  start_mining: "Starting mining",
  stop_mining: "Stopping mining",
  reboot: "Rebooting miners",
  blink_led: "Blinking LEDs",
  download_logs: "Downloading logs",
  set_power_target: "Updating power target",
  set_cooling_mode: "Updating cooling mode",
  update_mining_pools: "Updating mining pools",
  update_miner_password: "Updating miner password",
  firmware_update: "Updating firmware",
  unpair: "Unpairing miners",
  curtail: "Starting curtailment",
  uncurtail: "Ending curtailment",
};

const metadata = (entry: ActivityEntry): ActivityMetadata => entry.metadata ?? {};

const metadataString = (entry: ActivityEntry, key: string): string | undefined => {
  const value = metadata(entry)[key];
  return typeof value === "string" && value.trim() ? value : undefined;
};

const metadataNumber = (entry: ActivityEntry, key: string): number | undefined => {
  const value = metadata(entry)[key];
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
};

const targetAfterColon = (description: string): string | undefined => {
  const match = description.match(/:\s*(.+)$/);
  return match?.[1]?.trim() || undefined;
};

const quotedTarget = (description: string): string | undefined => {
  const match = description.match(/"([^"]+)"/);
  return match?.[1]?.trim() || undefined;
};

const countLabel = (count: number, singular: string, plural = `${singular}s`): string =>
  `${count} ${count === 1 ? singular : plural}`;

const minerCountLabel = (count: number): string => countLabel(count, "miner");

const withTarget = (label: string, target?: string): string => (target ? `${label}: ${target}` : label);

const collectionNoun = (entry: ActivityEntry): string => {
  if (entry.scopeType) return formatLabel(entry.scopeType).toLowerCase();
  const match = entry.description.match(/^(?:Create|Update|Delete)\s+([^:]+):/i);
  return match?.[1]?.trim().toLowerCase() || "collection";
};

const displayName = (entry: ActivityEntry, ...metadataKeys: string[]): string | undefined => {
  for (const key of metadataKeys) {
    const value = metadataString(entry, key);
    if (value) return value;
  }
  return entry.scopeLabel || targetAfterColon(entry.description) || quotedTarget(entry.description);
};

const formatCompletedCommand = (entry: ActivityEntry, normalizedEventType: string): string => {
  const label = completedCommandDescriptions[normalizedEventType] ?? formatLabel(normalizedEventType);
  const successCount = metadataNumber(entry, "success_count");
  const failureCount = metadataNumber(entry, "failure_count");

  if (successCount === undefined || failureCount === undefined) {
    return label;
  }

  const totalCount = successCount + failureCount;
  if (totalCount === 0) return label;

  return `${label}: ${successCount}/${totalCount} ${totalCount === 1 ? "miner" : "miners"} completed`;
};

const formatDeletedSite = (entry: ActivityEntry): string | undefined => {
  const siteID = metadataNumber(entry, "site_id") ?? entry.description.match(/Deleted site (\d+)/)?.[1];
  const buildingCount = metadataNumber(entry, "deleted_building_count");
  const rackCount = metadataNumber(entry, "unassigned_rack_count");
  const minerCount = metadataNumber(entry, "unassigned_device_count");
  const responseProfileCount = metadataNumber(entry, "deleted_response_profile_count");

  const parts = [
    buildingCount !== undefined ? countLabel(buildingCount, "building") : undefined,
    rackCount !== undefined ? `${countLabel(rackCount, "rack")} unassigned` : undefined,
    minerCount !== undefined ? `${minerCountLabel(minerCount)} unassigned` : undefined,
    responseProfileCount !== undefined ? `${countLabel(responseProfileCount, "response profile")} deleted` : undefined,
  ].filter(Boolean);

  if (parts.length === 0) return undefined;
  return `Deleted site${siteID ? ` ${siteID}` : ""}: ${parts.join(", ")}`;
};

const formatDeletedBuilding = (entry: ActivityEntry): string | undefined => {
  const buildingID = metadataNumber(entry, "building_id") ?? entry.description.match(/Deleted building (\d+)/)?.[1];
  const rackCount = metadataNumber(entry, "unassigned_rack_count");
  if (rackCount === undefined) return undefined;
  return `Deleted building${buildingID ? ` ${buildingID}` : ""}: ${countLabel(rackCount, "rack")} unassigned`;
};

const descriptionFormatters: Record<string, (entry: ActivityEntry) => string | undefined> = {
  login: () => "Logged in",
  login_failed: () => "Couldn't log in",
  logout: () => "Logged out",
  create_admin_user: () => "Created admin account",
  create_user: (entry) => withTarget("Created user", displayName(entry, "target_username")),
  update_username: () => "Updated username",
  step_up_auth_failed: () => "Couldn't verify authentication",
  update_password: () => "Updated password",
  reset_password: (entry) => {
    const username = displayName(entry, "target_username");
    return username ? `Reset password for ${username}` : "Reset password";
  },
  deactivate_user: (entry) => withTarget("Deactivated user", displayName(entry, "target_username")),
  update_user_role: (entry) => {
    const username = displayName(entry, "target_username");
    return username ? `Updated role for ${username}` : "Updated user role";
  },
  create_api_key: () => "Created API key",
  revoke_api_key: () => "Revoked API key",

  create_collection: (entry) => withTarget(`Created ${collectionNoun(entry)}`, displayName(entry)),
  update_collection: (entry) => withTarget(`Updated ${collectionNoun(entry)}`, displayName(entry)),
  delete_collection: (entry) => withTarget(`Deleted ${collectionNoun(entry)}`, displayName(entry)),
  add_devices: (entry) => withTarget("Added miners to group", displayName(entry)),
  remove_devices: (entry) => withTarget("Removed miners from group", displayName(entry)),
  // The server reuses assign_devices_to_rack for the clear-rack path with a
  // "Cleared devices from rack" description; don't report the opposite action.
  assign_devices_to_rack: (entry) =>
    /^cleared\b/i.test(entry.description)
      ? withTarget("Cleared miners from rack", displayName(entry))
      : withTarget("Assigned miners to rack", displayName(entry)),
  set_rack_slot: (entry) => withTarget("Updated rack position", displayName(entry)),
  clear_rack_slot: (entry) => withTarget("Updated rack position", displayName(entry)),
  save_rack: (entry) => withTarget("Saved rack", displayName(entry)),
  unpair_miners: () => "Unpaired miners",
  rename_miners: () => "Renamed miners",

  create_pool: (entry) => withTarget("Created pool", displayName(entry, "pool_name")),
  update_pool: (entry) => withTarget("Updated pool", displayName(entry, "pool_name")),
  delete_pool: (entry) => withTarget("Deleted pool", displayName(entry, "pool_name")),

  create_role: (entry) => withTarget("Created role", displayName(entry, "role_name")),
  update_role: (entry) => withTarget("Updated role", displayName(entry, "role_name")),
  delete_role: (entry) => withTarget("Deleted role", displayName(entry, "role_name")),

  "site.created": (entry) => withTarget("Created site", displayName(entry, "site_name")),
  "site.updated": (entry) => withTarget("Updated site", displayName(entry, "site_name")),
  "site.deleted": formatDeletedSite,
  "building.created": (entry) => withTarget("Created building", displayName(entry, "building_name")),
  "building.updated": (entry) => withTarget("Updated building", displayName(entry, "building_name")),
  "building.deleted": formatDeletedBuilding,
  "building.assigned_to_site": () => "Assigned building to site",
  "racks.assigned_to_site": () => "Assigned racks to site",
  "building.rack_assigned": () => "Assigned racks to building",
  "devices.reassigned_to_site": () => "Reassigned miners to site",
  "devices.reassigned_to_building": () => "Reassigned miners to building",

  schedule_executed: (entry) => withTarget("Ran schedule", quotedTarget(entry.description)),
  schedule_window_ended: (entry) => withTarget("Ended schedule window", quotedTarget(entry.description)),
  schedule_completed: (entry) => withTarget("Completed schedule", quotedTarget(entry.description)),
  schedule_conflict_skip: (entry) => withTarget("Skipped schedule conflict", quotedTarget(entry.description)),
  schedule_skipped_due_to_curtailment: (entry) =>
    withTarget("Skipped schedule during curtailment", quotedTarget(entry.description)),

  curtailment_started: () => "Started curtailment",
  curtailment_admin_terminated: () => "Stopped curtailment",
  curtailment_admin_terminated_replay: () => "Curtailment already stopped",
  curtailment_updated: () => "Updated curtailment",
  curtailment_force_released: () => "Released curtailment ownership",

  command_preflight_blocked: (entry) => {
    const skippedCount = metadataNumber(entry, "skipped_count");
    return skippedCount === undefined
      ? "Command couldn't run"
      : `Command couldn't run: ${minerCountLabel(skippedCount)} excluded by filters`;
  },
  command_filter_skip: (entry) => {
    const skippedCount = metadataNumber(entry, "skipped_count");
    return skippedCount === undefined
      ? "Command ran with skipped miners"
      : `Command ran with ${minerCountLabel(skippedCount)} skipped`;
  },
};

function replaceCountToken(value: string, token: string, singular: string, plural = `${singular}s`): string {
  return value.replace(new RegExp(`(\\d+) ${token}\\(s\\)`, "gi"), (_, count: string) =>
    countLabel(Number(count), singular, plural),
  );
}

function cleanRawDescription(description: string): string {
  let value = description.trim();
  value = value.replace(/\s+\(id=\d+\)/g, "");
  value = value.replace(/\bfailed\b/gi, "not completed");
  value = value.replace(/\bforce-cleared\b/gi, "cleared");
  value = replaceCountToken(value, "rack", "rack");
  value = replaceCountToken(value, "building", "building");
  value = replaceCountToken(value, "device", "miner");
  value = replaceCountToken(value, "membership", "membership", "memberships");
  value = value.replace(/\bdevices\b/gi, "miners");
  value = value.replace(/\bdevice\b/gi, "miner");
  return value.replace(/^./, (c) => c.toUpperCase());
}

function cleanRawErrorMessage(message: string): string {
  let value = message.trim().replace(/\s+/g, " ");
  value = value.replace(/^internal:\s*/i, "");
  value = value.replace(/\bfor device\s+/gi, "for ");
  value = value.replace(/\bdeviceID\b/gi, "miner ID");
  value = value.replace(/\bfailed to\b/gi, "couldn't");
  value = value.replace(/\bnot completed to\b/gi, "couldn't");
  value = value.replace(/\bforce-cleared\b/gi, "cleared");
  value = value.replace(/\bdevices\b/gi, "miners");
  value = value.replace(/\bdevice\b/gi, "miner");
  return value.replace(/^./, (c) => c.toUpperCase());
}

function minerConnectionTarget(message: string): string | undefined {
  return message.match(/\b\d{1,3}(?:\.\d{1,3}){3}:\d+\b/)?.[0];
}

function minerTargetPhrase(message: string): string {
  const target = minerConnectionTarget(message);
  return target ? ` at ${target}` : "";
}

function formatKnownMinerConnectionError(message: string): string | undefined {
  const lower = message.toLowerCase();
  const target = minerTargetPhrase(message);

  if (
    lower.includes("i/o timeout") ||
    lower.includes("context deadline exceeded") ||
    lower.includes("deadlineexceeded")
  ) {
    return `Couldn't connect to miner${target}. Connection timed out.`;
  }

  if (lower.includes("connection refused")) {
    return `Couldn't connect to miner${target}. Connection was refused.`;
  }

  if (
    lower.includes("connect to miner") ||
    lower.includes("miner connection") ||
    lower.includes("verify miner communication")
  ) {
    return `Couldn't connect to miner${target}.`;
  }

  if (lower.includes("get miner status") || lower.includes("get mining status") || lower.includes("get summary")) {
    return "Couldn't read miner status.";
  }

  return undefined;
}

export function formatActivityDescription(entry: ActivityEntry): string {
  const normalizedEventType = baseEventType(entry.eventType);

  if (isCompletedEvent(entry.eventType)) {
    return formatCompletedCommand(entry, normalizedEventType);
  }

  if (startedCommandDescriptions[normalizedEventType] && entry.batchId) {
    return startedCommandDescriptions[normalizedEventType];
  }

  return descriptionFormatters[normalizedEventType]?.(entry) ?? cleanRawDescription(entry.description);
}

export function formatActivityErrorSummary(message: string): string {
  const normalized = message.trim();
  const lower = normalized.toLowerCase();

  if (lower === "invalid credentials") {
    return "Credentials didn't match.";
  }

  if (
    lower.includes("i/o timeout") ||
    lower.includes("context deadline exceeded") ||
    lower.includes("deadlineexceeded")
  ) {
    return "Miner didn't respond.";
  }

  if (
    lower.includes("connect to miner") ||
    lower.includes("miner connection") ||
    lower.includes("connection refused")
  ) {
    return "Couldn't connect to miner.";
  }

  if (lower.includes("verify miner communication")) {
    return "Couldn't verify miner communication.";
  }

  if (lower.includes("get miner status") || lower.includes("get mining status") || lower.includes("get summary")) {
    return "Couldn't read miner status.";
  }

  return formatActivityErrorMessage(normalized);
}

export function formatActivityErrorMessage(message: string): string {
  const normalized = message.trim();
  if (normalized.toLowerCase() === "invalid credentials") {
    return "Credentials didn't match.";
  }

  return formatKnownMinerConnectionError(normalized) ?? cleanRawErrorMessage(normalized);
}
