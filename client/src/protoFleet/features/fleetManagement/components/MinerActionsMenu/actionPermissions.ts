import { useMemo } from "react";

import { type BulkAction } from "../BulkActions/types";
import { deviceActions, groupActions, performanceActions, settingsActions, type SupportedAction } from "./constants";
import { usePermissions } from "@/protoFleet/store";

// ACTION_PERMISSIONS maps each miner action to the catalog key the
// caller must hold to invoke it. Keys come from the server-side
// rpc_permissions.go table: the action's RPC is gated on the matching
// PermMiner* / PermRackManage / PermCurtailmentManage entry, so the UI
// hides actions a caller cannot exercise. Entries omitted from the map
// are treated as "no specific key required" and stay visible — used
// for view-only affordances like "View miner" on SingleMinerActionsMenu.
//
// Keep this aligned with rpc_permissions.go's MinerCommandService
// mapping; the server still enforces every gate regardless of what the
// UI shows.
export const ACTION_PERMISSIONS: Partial<Record<SupportedAction, string>> = {
  [deviceActions.blinkLEDs]: "miner:blink_led",
  [deviceActions.downloadLogs]: "miner:download_logs",
  [deviceActions.firmwareUpdate]: "miner:firmware_update",
  [deviceActions.reboot]: "miner:reboot",
  [deviceActions.shutdown]: "miner:stop_mining",
  [deviceActions.wakeUp]: "miner:start_mining",
  [deviceActions.unpair]: "miner:unpair",
  // factoryReset is referenced in capability tables but not exposed via
  // popoverActions today; gate on miner:unpair as the closest semantic
  // match if/when it surfaces in the UI.
  [deviceActions.factoryReset]: "miner:unpair",

  [performanceActions.managePower]: "miner:set_power_target",
  [performanceActions.curtail]: "curtailment:manage",

  [settingsActions.miningPool]: "miner:update_pools",
  [settingsActions.coolingMode]: "miner:set_cooling_mode",
  [settingsActions.rename]: "miner:rename",
  [settingsActions.updateWorkerNames]: "miner:update_worker_names",
  [settingsActions.security]: "miner:update_password",

  // Adding miners to a collection/group goes through
  // DeviceCollectionServiceAddDevicesToCollection, server-gated on
  // rack:manage.
  [groupActions.addToGroup]: "rack:manage",
};

/**
 * Filter a list of {@link BulkAction}s down to the ones whose backing
 * RPC the caller is allowed to invoke. Action types outside
 * {@link ACTION_PERMISSIONS} (e.g. SingleMinerActionsMenu's
 * `viewMiner`) pass through unfiltered — those have no server RPC and
 * therefore no permission requirement.
 *
 * The server enforces every action gate regardless; this hook is purely
 * for show/hide UX so a caller doesn't click into a 403.
 */
export const usePermittedActions = <ActionType extends string>(
  actions: ReadonlyArray<BulkAction<ActionType>>,
): BulkAction<ActionType>[] => {
  const permissions = usePermissions();
  return useMemo(() => {
    return actions.filter((action) => {
      const requiredKey = ACTION_PERMISSIONS[action.action as SupportedAction];
      return !requiredKey || permissions.includes(requiredKey);
    });
  }, [actions, permissions]);
};
