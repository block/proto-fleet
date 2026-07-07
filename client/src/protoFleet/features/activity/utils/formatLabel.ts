import { baseEventType } from "@/protoFleet/features/activity/utils/eventType";

const labelMap: Record<string, string> = {
  login: "Logged in",
  login_failed: "Couldn't log in",
  logout: "Logged out",
  create_admin_user: "Created admin account",
  create_user: "Created user",
  update_username: "Updated username",
  step_up_auth_failed: "Couldn't verify authentication",
  update_password: "Updated password",
  reset_password: "Reset password",
  deactivate_user: "Deactivated user",
  update_user_role: "Updated user role",
  create_api_key: "Created API key",
  revoke_api_key: "Revoked API key",

  start_mining: "Start mining",
  stop_mining: "Stop mining",
  reboot: "Reboot miners",
  blink_led: "Blink LEDs",
  download_logs: "Download logs",
  set_power_target: "Update power target",
  set_cooling_mode: "Update cooling mode",
  update_mining_pools: "Update mining pools",
  update_miner_password: "Update miner password",
  firmware_update: "Update firmware",
  unpair: "Unpair miners",
  curtail: "Start curtailment",
  uncurtail: "End curtailment",
  command_preflight_blocked: "Command couldn't run",
  command_filter_skip: "Command ran with skipped miners",

  create_collection: "Created collection",
  update_collection: "Updated collection",
  delete_collection: "Deleted collection",
  add_devices: "Added miners",
  remove_devices: "Removed miners",
  assign_devices_to_rack: "Assigned miners to rack",
  set_rack_slot: "Updated rack position",
  clear_rack_slot: "Updated rack position",
  save_rack: "Saved rack",
  unpair_miners: "Unpaired miners",
  rename_miners: "Renamed miners",

  create_pool: "Created pool",
  update_pool: "Updated pool",
  delete_pool: "Deleted pool",

  create_role: "Created role",
  update_role: "Updated role",
  delete_role: "Deleted role",

  schedule_executed: "Ran schedule",
  schedule_window_ended: "Ended schedule window",
  schedule_completed: "Completed schedule",
  schedule_conflict_skip: "Skipped schedule conflict",
  schedule_skipped_due_to_curtailment: "Skipped schedule during curtailment",

  "site.created": "Created site",
  "site.updated": "Updated site",
  "site.deleted": "Deleted site",
  "building.created": "Created building",
  "building.updated": "Updated building",
  "building.deleted": "Deleted building",
  "building.assigned_to_site": "Assigned building to site",
  "racks.assigned_to_site": "Assigned racks to site",
  "building.rack_assigned": "Assigned rack to building",
  "devices.reassigned_to_site": "Reassigned miners to site",
  "devices.reassigned_to_building": "Reassigned miners to building",

  curtailment_started: "Started curtailment",
  curtailment_admin_terminated: "Stopped curtailment",
  curtailment_admin_terminated_replay: "Curtailment already stopped",
  curtailment_updated: "Updated curtailment",
  curtailment_force_released: "Released curtailment ownership",

  auth: "Authentication",
  device_command: "Miner command",
  fleet_management: "Fleet management",
  collection: "Collection",
  pool: "Pool",
  schedule: "Schedule",
  curtailment: "Curtailment",
  system: "System",

  rack: "Rack",
  group: "Group",
  site: "Site",
  building: "Building",
  miner: "Miner",
  device: "Miner",
  org: "Organization",
};

const filterLabelMap: Record<string, string> = {
  login: "Log in",
  login_failed: "Couldn't log in",
  logout: "Log out",
  create_admin_user: "Create admin account",
  create_user: "Create user",
  update_username: "Update username",
  step_up_auth_failed: "Couldn't verify authentication",
  update_password: "Update password",
  reset_password: "Reset password",
  deactivate_user: "Deactivate user",
  update_user_role: "Update user role",
  create_api_key: "Create API key",
  revoke_api_key: "Revoke API key",

  start_mining: "Start mining",
  stop_mining: "Stop mining",
  reboot: "Reboot miners",
  blink_led: "Blink LEDs",
  download_logs: "Download logs",
  set_power_target: "Update power target",
  set_cooling_mode: "Update cooling mode",
  update_mining_pools: "Update mining pools",
  update_miner_password: "Update miner password",
  firmware_update: "Update firmware",
  unpair: "Unpair miners",
  curtail: "Start curtailment",
  uncurtail: "End curtailment",
  command_preflight_blocked: "Command couldn't run",
  command_filter_skip: "Command ran with skipped miners",

  create_collection: "Create collection",
  update_collection: "Update collection",
  delete_collection: "Delete collection",
  add_devices: "Add miners",
  remove_devices: "Remove miners",
  assign_devices_to_rack: "Assign miners to rack",
  set_rack_slot: "Update rack position",
  clear_rack_slot: "Update rack position",
  save_rack: "Save rack",
  unpair_miners: "Unpair miners",
  rename_miners: "Rename miners",

  create_pool: "Create pool",
  update_pool: "Update pool",
  delete_pool: "Delete pool",

  create_role: "Create role",
  update_role: "Update role",
  delete_role: "Delete role",

  schedule_executed: "Run schedule",
  schedule_window_ended: "End schedule window",
  schedule_completed: "Complete schedule",
  schedule_conflict_skip: "Skip schedule conflict",
  schedule_skipped_due_to_curtailment: "Skip schedule during curtailment",

  "site.created": "Create site",
  "site.updated": "Update site",
  "site.deleted": "Delete site",
  "building.created": "Create building",
  "building.updated": "Update building",
  "building.deleted": "Delete building",
  "building.assigned_to_site": "Assign building to site",
  "racks.assigned_to_site": "Assign racks to site",
  "building.rack_assigned": "Assign rack to building",
  "devices.reassigned_to_site": "Reassign miners to site",
  "devices.reassigned_to_building": "Reassign miners to building",

  curtailment_started: "Start curtailment",
  curtailment_admin_terminated: "Stop curtailment",
  curtailment_admin_terminated_replay: "Curtailment already stopped",
  curtailment_updated: "Update curtailment",
  curtailment_force_released: "Release curtailment ownership",
};

const acronymMap: Record<string, string> = {
  api: "API",
  id: "ID",
  led: "LED",
  url: "URL",
};

const fallbackLabel = (str: string): string => {
  const words = str
    .replace(/[._-]+/g, " ")
    .trim()
    .split(/\s+/)
    .filter(Boolean)
    .map((word) => acronymMap[word.toLowerCase()] ?? word.toLowerCase());

  if (words.length === 0) return "";

  return words.join(" ").replace(/^./, (c) => c.toUpperCase());
};

export const formatLabel = (str: string) => {
  const normalized = baseEventType(str);
  return labelMap[normalized] ?? fallbackLabel(normalized);
};

export const formatActivityFilterLabel = (str: string) => {
  const normalized = baseEventType(str);
  return filterLabelMap[normalized] ?? fallbackLabel(normalized);
};
