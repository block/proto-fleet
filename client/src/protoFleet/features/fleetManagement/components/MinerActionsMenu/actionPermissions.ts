import { useMemo } from "react";

import { type BulkAction } from "../BulkActions/types";
import { deviceActions, groupActions, performanceActions, settingsActions, type SupportedAction } from "./constants";
import { usePermissions } from "@/protoFleet/store";

// ACTION_PERMISSIONS maps every SupportedAction to the catalog key(s)
// the caller must hold to invoke its backing RPC. A `readonly string[]`
// value is treated as an AND requirement — used for flows that issue
// multiple RPCs (e.g. the pool-assignment surface lists pools first,
// then writes pool config to the miner).
//
// The map is a full `Record<SupportedAction, ...>` rather than a
// `Partial<>` so adding a new SupportedAction without a permission
// entry fails TypeScript compilation, not at runtime. Lookups for
// action strings outside SupportedAction (e.g. `viewMiner` on
// SingleMinerActionsMenu) are intentional: those actions have no RPC
// and no permission requirement, and fall through the string-keyed
// lookup below as `undefined`.
//
// Keep this aligned with rpc_permissions.go's gates; the server still
// enforces every key regardless of what the UI shows. This filter is a
// UX improvement, not a security boundary.
//
// Scope caveat: the filter uses org-scope semantics via
// EffectivePermissions.FlatKeys() projected onto UserInfo.permissions,
// so a *site-scoped* grant for a miner action will still surface the
// action in the menu even though the click will be denied for miners
// outside the granted site. Site-aware filtering needs the menu to
// know the targeted miner's site and is deferred — the server gate
// catches the 403 regardless.
export const ACTION_PERMISSIONS: Record<SupportedAction, string | readonly string[]> = {
  [deviceActions.blinkLEDs]: "miner:blink_led",
  [deviceActions.downloadLogs]: "miner:download_logs",
  [deviceActions.firmwareUpdate]: ["miner:firmware_update", "miner:reboot"],
  [deviceActions.reboot]: "miner:reboot",
  [deviceActions.shutdown]: "miner:stop_mining",
  [deviceActions.wakeUp]: "miner:start_mining",
  // Unpair in the UI dispatches to FleetManagementService.DeleteMiners,
  // which the server gates on miner:delete (not miner:unpair — that
  // catalog key gates the MinerCommandService.Unpair RPC, which the
  // menu does not call).
  [deviceActions.unpair]: "miner:delete",
  // factoryReset is in the SupportedAction union but not exposed via
  // popoverActions today; gate on miner:delete as the closest
  // destructive-action partner if/when it surfaces.
  [deviceActions.factoryReset]: "miner:delete",

  [performanceActions.managePower]: "miner:set_power_target",
  [performanceActions.curtail]: "curtailment:manage",

  // Pool assignment opens PoolSelectionPage, which calls usePools()
  // (ListPools / pool:read) before writing the miner-side pool config
  // (UpdateMiningPools / miner:update_pools). Roles missing pool:read
  // would otherwise enter a flow that 403s on the very first request;
  // require both up front.
  [settingsActions.miningPool]: ["miner:update_pools", "pool:read"],
  [settingsActions.coolingMode]: "miner:set_cooling_mode",
  [settingsActions.rename]: "miner:rename",
  [settingsActions.updateWorkerNames]: "miner:update_worker_names",
  [settingsActions.security]: "miner:update_password",

  // Each add-to flow lists candidates (read) before writing membership
  // (manage); require both up front so the picker doesn't 403 on the
  // first list request. Additional `*:read` entries cover preflight
  // reads the flow performs before the write:
  //   - addToGroup: FleetGroupActionsMenu resolves scoped devices via
  //     ListMinerStateSnapshots → miner:read.
  //   - addToRack: ParentPickerModal hydrates building-name hints via
  //     ListBuildings → site:read; all-mode + capacity preflight reads
  //     miner snapshots → miner:read.
  //   - addToSite: rack-site conflict preflight calls ListRacks →
  //     rack:read; all-mode resolves miners → miner:read.
  [groupActions.addToGroup]: ["rack:manage", "rack:read", "miner:read"],
  [groupActions.addToRack]: ["rack:manage", "rack:read", "site:read", "miner:read"],
  [groupActions.addToBuilding]: ["site:manage", "site:read"],
  [groupActions.addToSite]: ["site:manage", "site:read", "rack:read", "miner:read"],
};

// hasAllRequired returns true when every required key is present in
// the caller's effective permission set. A single-string requirement
// is treated as a one-element AND set.
const hasAllRequired = (required: string | readonly string[], permissions: readonly string[]): boolean => {
  if (typeof required === "string") {
    return permissions.includes(required);
  }
  return required.every((key) => permissions.includes(key));
};

/**
 * Filter a list of {@link BulkAction}s down to the ones whose backing
 * RPC(s) the caller is allowed to invoke. Action types outside
 * {@link ACTION_PERMISSIONS} (e.g. SingleMinerActionsMenu's
 * `viewMiner`) pass through unfiltered — those have no server RPC and
 * therefore no permission requirement. The string-keyed lookup is
 * intentional: it lets unmapped action strings return undefined
 * without an unsound `as SupportedAction` cast.
 *
 * The server enforces every action gate regardless; this hook is purely
 * for show/hide UX so a caller doesn't click into a 403.
 */
export const usePermittedActions = <ActionType extends string>(
  actions: ReadonlyArray<BulkAction<ActionType>>,
): BulkAction<ActionType>[] => {
  const permissions = usePermissions();
  return useMemo(() => {
    const lookup: Record<string, string | readonly string[] | undefined> = ACTION_PERMISSIONS;
    return actions.filter((action) => {
      const required = lookup[action.action];
      return required === undefined || hasAllRequired(required, permissions);
    });
  }, [actions, permissions]);
};
