import { useEffect, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";

import { useBuildings } from "@/protoFleet/api/buildings";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import { PerDeviceBuildingConflictReason } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import {
  type MinerListFilter,
  MinerListFilterSchema,
  type MinerStateSnapshot,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useSites } from "@/protoFleet/api/sites";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import ParentPickerModal from "@/protoFleet/components/ParentPickerModal";
import { useFleetCreateFlow } from "@/protoFleet/features/fleetManagement/components/FleetCreateFlow/context";
import { applyFleetVisiblePairingStatuses } from "@/protoFleet/features/fleetManagement/utils/fleetVisiblePairingFilter";
import {
  getMinerBuildingLabel,
  getMinerRackLabel,
  getMinerSiteLabel,
} from "@/protoFleet/features/fleetManagement/utils/minerPlacement";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import { pushToast, removeToast, STATUSES, updateToast } from "@/shared/features/toaster";

export type ReparentKind = "rack" | "site" | "building";

interface MinerReparentPickerProps {
  kind: ReparentKind;
  // In all-mode this is the visible page only; the full set is
  // resolved via listMinerStateSnapshots before dispatch.
  deviceIdentifiers: string[];
  selectionMode: "subset" | "all";
  currentFilter?: MinerListFilter;
  totalCount?: number;
  // Snapshots keyed by deviceIdentifier — used by the rack guard to
  // detect cross-rack conflicts. Subset mode passes the caller's map;
  // all-mode builds it during resolveAllModeIds.
  miners?: Record<string, MinerStateSnapshot>;
  sourceLabel: string;
  successMessage: (count: number | bigint, target: "site" | "rack" | "building") => string;
  onClose: () => void;
  onRefetchMiners?: () => void;
}

const MAX_SNAPSHOT_PAGES = 50;
const SNAPSHOT_PAGE_SIZE = 1000;
const MAX_MINERS = MAX_SNAPSHOT_PAGES * SNAPSHOT_PAGE_SIZE;
// Matches `max_items: 10000` on AssignDevicesToSiteRequest.device_identifiers
// and AssignDevicesToBuildingRequest.device_identifiers — both flows have the
// same server-side cap, so one constant covers both.
const MAX_REASSIGN_BATCH = 10000;

// Capacity check for the bulk Add-to-rack path. Server-side
// AddDevicesToDeviceSet doesn't enforce slot count, so an over-fill
// here would persist invisibly until the operator opened the rack
// view. Discounts ids already in the rack — server uses
// `ON CONFLICT DO NOTHING`, so existing members aren't new additions.
// Returns null when the target has room.
const rackOverflowMessage = (rack: DeviceSet, currentMembers: Set<string>, ids: string[]): string | null => {
  const rackInfo = rack.typeDetails.case === "rackInfo" ? rack.typeDetails.value : undefined;
  if (!rackInfo) return null;
  const totalSlots = rackInfo.rows * rackInfo.columns;
  if (totalSlots <= 0) return null;
  const newAdditions = ids.filter((id) => !currentMembers.has(id)).length;
  const available = Math.max(0, totalSlots - rack.deviceCount);
  if (newAdditions <= available) return null;
  const label = rack.label || "rack";
  return `Can't add ${newAdditions} miners to "${label}" — only ${available} slot${available === 1 ? "" : "s"} available (${rack.deviceCount}/${totalSlots} full).`;
};

// Paginate listMinerStateSnapshots filtered to the target rack so the
// capacity guard can discount ids that are already members. Capped at
// MAX_MINERS for the same reason as resolveAllModeIds.
const resolveRackMembers = async (rackId: bigint, signal?: AbortSignal): Promise<Set<string>> => {
  const filter = create(MinerListFilterSchema, { rackIds: [rackId] });
  const members = new Set<string>();
  let cursor = "";
  let exhausted = false;
  for (let i = 0; i < MAX_SNAPSHOT_PAGES; i++) {
    let response;
    try {
      response = await fleetManagementClient.listMinerStateSnapshots(
        {
          pageSize: SNAPSHOT_PAGE_SIZE,
          cursor,
          filter,
        },
        { signal },
      );
    } catch (err) {
      // listMinerStateSnapshots rejects on abort; treat as a quiet
      // unmount, returning whatever members we accumulated so far so
      // the caller's abort check can short-circuit without a toast.
      if (signal?.aborted || (err as Error)?.name === "AbortError") {
        return members;
      }
      throw err;
    }
    if (signal?.aborted) return members;
    for (const miner of response.miners) members.add(miner.deviceIdentifier);
    if (!response.cursor) {
      exhausted = true;
      break;
    }
    cursor = response.cursor;
  }
  if (!exhausted) {
    throw new Error(`Target rack has more than ${MAX_MINERS} miners. Refresh the page and retry.`);
  }
  return members;
};

// Detect miners whose current rack lives at a different site than the
// target. `ReassignDevicesToSite` rejects these with
// `DEVICE_IN_RACK_AT_OTHER_SITE` and aborts the whole batch; we
// pre-warn the operator and orchestrate the unassign-from-rack step
// on confirm.
const groupRackSiteConflicts = (
  ids: string[],
  miners: Record<string, MinerStateSnapshot> | undefined,
  rackLabelToSiteId: Map<string, bigint | undefined>,
  targetSiteId: bigint,
): Map<string, string[]> => {
  const conflicts = new Map<string, string[]>();
  if (!miners) return conflicts;
  for (const id of ids) {
    const snapshot = miners[id];
    if (!snapshot) continue;
    const sourceLabel = getMinerRackLabel(snapshot);
    if (!sourceLabel) continue;
    const rackSiteId = rackLabelToSiteId.get(sourceLabel);
    // A miner in a site-less rack (rackSiteId undefined) moving to a real
    // site is also a conflict: it can't keep a direct site while in a
    // rack that has none, so force-clear unassigns it from the rack.
    // undefined !== targetSiteId, so it falls through to the flag below.
    if (rackSiteId === targetSiteId) continue;
    const bucket = conflicts.get(sourceLabel) ?? [];
    bucket.push(id);
    conflicts.set(sourceLabel, bucket);
  }
  return conflicts;
};

const resolveAllModeIds = async (
  filter: MinerListFilter,
  signal?: AbortSignal,
): Promise<{ ids: string[]; snapshots: Record<string, MinerStateSnapshot> }> => {
  const ids: string[] = [];
  const snapshots: Record<string, MinerStateSnapshot> = {};
  let cursor = "";
  let exhausted = false;
  for (let i = 0; i < MAX_SNAPSHOT_PAGES; i++) {
    let response;
    try {
      response = await fleetManagementClient.listMinerStateSnapshots(
        {
          pageSize: SNAPSHOT_PAGE_SIZE,
          cursor,
          filter,
        },
        { signal },
      );
    } catch (err) {
      // Same abort-on-unmount story as resolveRackMembers: return the
      // partial accumulators so the caller's signal.aborted gate can
      // swallow the early-exit quietly instead of routing to a toast.
      if (signal?.aborted || (err as Error)?.name === "AbortError") {
        return { ids, snapshots };
      }
      throw err;
    }
    if (signal?.aborted) return { ids, snapshots };
    for (const miner of response.miners) {
      ids.push(miner.deviceIdentifier);
      snapshots[miner.deviceIdentifier] = miner;
    }
    if (!response.cursor) {
      exhausted = true;
      break;
    }
    cursor = response.cursor;
  }
  if (!exhausted) {
    throw new Error(`Too many miners selected (over ${MAX_MINERS}). Filter the list and try again.`);
  }
  return { ids, snapshots };
};

type SiteMoveConfirmation = {
  targetSiteId: bigint;
  ids: string[];
  conflictsByLabel: Map<string, string[]>;
  labelToRackId: Map<string, bigint>;
};

// BuildingMoveConfirmation drives the "device in rack at other building"
// confirm dialog. Unlike SiteMoveConfirmation we lean on the server's
// own conflict response (PerDeviceBuildingConflict[]) instead of
// pre-scanning racks — the new AssignDevicesToBuilding RPC returns the
// per-device list directly.
type BuildingMoveConfirmation = {
  targetBuildingId: bigint;
  ids: string[];
  conflictCount: number;
};

// RackSiteStripConfirmation drives the "this rack has no site" confirm
// dialog. Adding a miner that has a site to a site-less rack strips its
// site (the rack dictates no placement); the server returns the
// conflicting count and writes nothing until the operator confirms.
type RackSiteStripConfirmation = {
  targetRackId: bigint;
  ids: string[];
  conflictCount: number;
};

const MinerReparentPicker = ({
  kind,
  deviceIdentifiers,
  selectionMode,
  currentFilter,
  totalCount,
  miners,
  sourceLabel,
  successMessage,
  onClose,
  onRefetchMiners,
}: MinerReparentPickerProps) => {
  const { assignDevicesToSite } = useSites();
  const { assignDevicesToBuilding } = useBuildings();
  const { assignDevicesToRack, getDeviceSet, listRacks } = useDeviceSets();
  const createFlow = useFleetCreateFlow();
  const [siteMoveConfirmation, setSiteMoveConfirmation] = useState<SiteMoveConfirmation | null>(null);
  const [siteMoveInFlight, setSiteMoveInFlight] = useState(false);
  const [buildingMoveConfirmation, setBuildingMoveConfirmation] = useState<BuildingMoveConfirmation | null>(null);
  const [buildingMoveInFlight, setBuildingMoveInFlight] = useState(false);
  const [rackSiteStripConfirmation, setRackSiteStripConfirmation] = useState<RackSiteStripConfirmation | null>(null);
  const [rackStripInFlight, setRackStripInFlight] = useState(false);
  // Resolver for the onConfirm promise during the cross-site confirm
  // dialog flow. We hand it off to the Dialog button handlers so
  // ParentPickerModal's handleSave only resolves (and calls onDismiss
  // → onClose) after the operator picks Continue or Cancel.
  const dialogResolveRef = useRef<(() => void) | null>(null);

  // Abort in-flight snapshot pagination and bulk RPCs on unmount so a
  // long-running resolveAllModeIds / resolveRackMembers / dispatch
  // loop doesn't keep firing after the operator dismisses the picker.
  const abortRef = useRef<AbortController | null>(null);
  if (abortRef.current === null) {
    abortRef.current = new AbortController();
  }
  useEffect(() => {
    return () => {
      abortRef.current?.abort();
    };
  }, []);

  const fetchAllRacks = () =>
    new Promise<DeviceSet[]>((resolve, reject) => {
      void listRacks({
        onSuccess: (racks) => resolve(racks),
        onError: (msg) => reject(new Error(msg)),
      });
    });

  const fetchRack = (rackId: bigint) =>
    new Promise<DeviceSet>((resolve, reject) => {
      void getDeviceSet({
        deviceSetId: rackId,
        onSuccess: resolve,
        onNotFound: () => reject(new Error("Couldn't find rack.")),
        onError: (msg) => reject(new Error(msg)),
      });
    });

  const dispatchSiteReassign = (targetSiteId: bigint, ids: string[], forceClearConflictingRackMembership = false) =>
    new Promise<void>((resolve) => {
      void assignDevicesToSite({
        targetSiteId,
        deviceIdentifiers: ids,
        signal: abortRef.current?.signal,
        forceClearConflictingRackMembership,
        onSuccess: (count) => {
          // Gate UI side-effects on unmount: the abort signal fires in
          // the picker's cleanup effect, so a late RPC resolution
          // would otherwise push a toast / refetch after the operator
          // dismissed the modal.
          if (abortRef.current?.signal.aborted) {
            resolve();
            return;
          }
          pushToast({ message: successMessage(count, "site"), status: STATUSES.success });
          onRefetchMiners?.();
          resolve();
        },
        onError: (msg) => {
          if (abortRef.current?.signal.aborted) {
            resolve();
            return;
          }
          pushToast({ message: `Couldn't move miners: ${msg}`, status: STATUSES.error });
          resolve();
        },
      });
    });

  // Cross-site move with conflicting rack memberships: a single
  // server-side transaction strips the prior rack rows and applies
  // the site write. Previously this was a client-side loop of
  // removeDevicesFromDeviceSet calls followed by assignDevicesToSite,
  // which left a window where a transport failure between the two
  // RPCs would orphan miners (rack-less but still on the old site).
  const dispatchSiteMoveWithUnassign = async (confirmation: SiteMoveConfirmation) => {
    setSiteMoveInFlight(true);
    const movingToast = pushToast({
      message: "Moving miners to the new site…",
      status: STATUSES.loading,
      longRunning: true,
    });
    await dispatchSiteReassign(confirmation.targetSiteId, confirmation.ids, true);
    removeToast(movingToast);
    setSiteMoveInFlight(false);
    setSiteMoveConfirmation(null);
  };

  const dispatchReparentToSite = async (
    targetSiteId: bigint,
    ids: string[],
    minerSnapshots: Record<string, MinerStateSnapshot> | undefined,
  ) => {
    if (ids.length > MAX_REASSIGN_BATCH) {
      pushToast({
        message: `Can't move more than ${MAX_REASSIGN_BATCH} miners to a site at once. Filter the list and try again.`,
        status: STATUSES.error,
      });
      return;
    }

    // Detect rack-at-other-site conflicts so we can warn before the
    // server rejects the whole batch with DEVICE_IN_RACK_AT_OTHER_SITE.
    let racks: DeviceSet[];
    try {
      racks = await fetchAllRacks();
    } catch (err) {
      const message = err instanceof Error ? err.message : "Couldn't load racks.";
      pushToast({ message, status: STATUSES.error });
      return;
    }
    const labelToSiteId = new Map<string, bigint | undefined>();
    const labelToRackId = new Map<string, bigint>();
    for (const rack of racks) {
      const info = rack.typeDetails.case === "rackInfo" ? rack.typeDetails.value : undefined;
      if (!rack.label) continue;
      labelToSiteId.set(rack.label, info?.siteId);
      labelToRackId.set(rack.label, rack.id);
    }

    const conflictsByLabel = groupRackSiteConflicts(ids, minerSnapshots, labelToSiteId, targetSiteId);
    if (conflictsByLabel.size === 0) {
      await dispatchSiteReassign(targetSiteId, ids);
      return;
    }

    // Park onConfirm's promise here — the Dialog's Continue/Cancel
    // handlers resolve it so ParentPickerModal only dismisses after
    // the operator's choice (and any orchestration) completes.
    setSiteMoveConfirmation({ targetSiteId, ids, conflictsByLabel, labelToRackId });
    await new Promise<void>((resolve) => {
      dialogResolveRef.current = resolve;
    });
  };

  const dispatchReparentToRack = async (targetRackId: bigint, ids: string[]) => {
    let rack: DeviceSet;
    let currentMembers: Set<string>;
    try {
      [rack, currentMembers] = await Promise.all([
        fetchRack(targetRackId),
        resolveRackMembers(targetRackId, abortRef.current?.signal),
      ]);
    } catch (err) {
      const message = err instanceof Error ? err.message : "Couldn't load rack.";
      pushToast({ message, status: STATUSES.error });
      return;
    }
    if (abortRef.current?.signal.aborted) return;
    const overflow = rackOverflowMessage(rack, currentMembers, ids);
    if (overflow) {
      pushToast({ message: overflow, status: STATUSES.error });
      return;
    }

    const result = await dispatchRackReassign(targetRackId, ids, false);
    if (result.ok) return;
    // Server flagged miners that would lose their site by joining this
    // site-less rack. Park the confirm dialog; Continue re-dispatches
    // with force to strip their site.
    setRackSiteStripConfirmation({ targetRackId, ids, conflictCount: result.conflictCount });
    await new Promise<void>((resolve) => {
      dialogResolveRef.current = resolve;
    });
  };

  // dispatchRackReassign performs the AssignDevicesToRack RPC. When
  // force=false and the target is a site-less rack holding miners that
  // have a site, the server returns site-strip conflicts; the caller
  // raises a confirm dialog and re-dispatches with force=true.
  const dispatchRackReassign = (
    targetRackId: bigint,
    ids: string[],
    force: boolean,
  ): Promise<{ ok: true } | { ok: false; conflictCount: number }> =>
    // AssignDevicesToRack clears prior rack membership and inserts the
    // new membership inside one server-side transaction, closing the
    // orphan window the old client-side remove-then-add loop had.
    new Promise((resolve) => {
      void assignDevicesToRack({
        targetRackId,
        deviceIdentifiers: ids,
        forceClearConflictingSite: force,
        signal: abortRef.current?.signal,
        onSuccess: (count) => {
          if (abortRef.current?.signal.aborted) {
            resolve({ ok: true });
            return;
          }
          pushToast({ message: successMessage(count, "rack"), status: STATUSES.success });
          onRefetchMiners?.();
          resolve({ ok: true });
        },
        onConflicts: (conflicts) => {
          if (abortRef.current?.signal.aborted) {
            resolve({ ok: true });
            return;
          }
          resolve({ ok: false, conflictCount: conflicts.length });
        },
        onError: (msg) => {
          if (abortRef.current?.signal.aborted) {
            resolve({ ok: true });
            return;
          }
          pushToast({ message: `Couldn't move miners to rack: ${msg}`, status: STATUSES.error });
          resolve({ ok: true });
        },
      });
    });

  const dispatchRackMoveWithStrip = async (confirmation: RackSiteStripConfirmation) => {
    setRackStripInFlight(true);
    const movingToast = pushToast({
      message: "Moving miners to the rack…",
      status: STATUSES.loading,
      longRunning: true,
    });
    await dispatchRackReassign(confirmation.targetRackId, confirmation.ids, true);
    removeToast(movingToast);
    setRackStripInFlight(false);
    setRackSiteStripConfirmation(null);
  };

  const handleRackStripDialogCancel = () => {
    setRackSiteStripConfirmation(null);
    settleDialog();
  };

  const handleRackStripDialogConfirm = async () => {
    if (!rackSiteStripConfirmation) return;
    await dispatchRackMoveWithStrip(rackSiteStripConfirmation);
    settleDialog();
  };

  // dispatchBuildingReassign performs the AssignDevicesToBuilding RPC.
  // When force=false we let the server enumerate per-device conflicts;
  // the caller raises a confirm dialog and re-dispatches with force=true.
  //
  // Conflicts come back in two flavors: DEVICE_IN_RACK_AT_OTHER_BUILDING
  // (clearable with force=true) and DEVICE_NOT_FOUND (force does nothing
  // — the device just doesn't exist in this org). Only the all-clearable
  // case opens the confirm dialog; a mixed or unclearable response
  // surfaces an error toast instead, since force-clear wouldn't unblock
  // those identifiers.
  const dispatchBuildingReassign = (
    targetBuildingId: bigint,
    ids: string[],
    force: boolean,
  ): Promise<{ ok: true } | { ok: false; clearableCount: number }> =>
    new Promise((resolve) => {
      void assignDevicesToBuilding({
        targetBuildingId,
        deviceIdentifiers: ids,
        forceClearConflictingRackMembership: force,
        signal: abortRef.current?.signal,
        onSuccess: (count) => {
          if (abortRef.current?.signal.aborted) {
            resolve({ ok: true });
            return;
          }
          pushToast({ message: successMessage(count, "building"), status: STATUSES.success });
          onRefetchMiners?.();
          resolve({ ok: true });
        },
        onError: (msg, conflicts) => {
          if (abortRef.current?.signal.aborted) {
            resolve({ ok: true });
            return;
          }
          if (conflicts.length > 0) {
            // Both IN_RACK_AT_OTHER_BUILDING and IN_RACK_AT_OTHER_SITE
            // are clearable by force_clear_conflicting_rack_membership
            // — the server drops the offending rack row in either case.
            // DEVICE_NOT_FOUND is not clearable.
            const clearable = conflicts.filter((c) => {
              const reason = c.reason as PerDeviceBuildingConflictReason;
              return (
                reason === PerDeviceBuildingConflictReason.DEVICE_IN_RACK_AT_OTHER_BUILDING ||
                reason === PerDeviceBuildingConflictReason.DEVICE_IN_RACK_AT_OTHER_SITE
              );
            });
            // Only raise the confirm dialog when every conflict can be
            // resolved by force-clear. Otherwise (e.g. DEVICE_NOT_FOUND
            // mixed in), Continue would re-run the RPC and still reject
            // — surface as an error toast and don't retry.
            if (clearable.length === conflicts.length) {
              resolve({ ok: false, clearableCount: clearable.length });
              return;
            }
            pushToast({
              message: `Couldn't move miners: ${conflicts.length} device(s) flagged with non-clearable conflicts.`,
              status: STATUSES.error,
            });
            resolve({ ok: true });
            return;
          }
          pushToast({ message: `Couldn't move miners: ${msg}`, status: STATUSES.error });
          resolve({ ok: true });
        },
      });
    });

  const dispatchBuildingMoveWithUnassign = async (confirmation: BuildingMoveConfirmation) => {
    setBuildingMoveInFlight(true);
    const movingToast = pushToast({
      message: "Moving miners to the new building…",
      status: STATUSES.loading,
      longRunning: true,
    });
    await dispatchBuildingReassign(confirmation.targetBuildingId, confirmation.ids, true);
    removeToast(movingToast);
    setBuildingMoveInFlight(false);
    setBuildingMoveConfirmation(null);
  };

  const dispatchReparentToBuilding = async (targetBuildingId: bigint, ids: string[]) => {
    // Same 10k cap as the site flow — server validation would reject
    // anyway, but failing here keeps the message specific.
    if (ids.length > MAX_REASSIGN_BATCH) {
      pushToast({
        message: `Can't move more than ${MAX_REASSIGN_BATCH} miners to a building at once. Filter the list and try again.`,
        status: STATUSES.error,
      });
      return;
    }
    const result = await dispatchBuildingReassign(targetBuildingId, ids, false);
    if (result.ok) return;
    // Server flagged cross-building conflicts that are all clearable.
    // Park onConfirm's promise on the dialog so ParentPickerModal only
    // dismisses once the operator picks Continue (force-clear) or Cancel.
    setBuildingMoveConfirmation({ targetBuildingId, ids, conflictCount: result.clearableCount });
    await new Promise<void>((resolve) => {
      dialogResolveRef.current = resolve;
    });
  };

  const dispatchReparent = (
    targetId: bigint,
    ids: string[],
    minerSnapshots: Record<string, MinerStateSnapshot> | undefined,
  ) => {
    if (kind === "site") return dispatchReparentToSite(targetId, ids, minerSnapshots);
    if (kind === "building") return dispatchReparentToBuilding(targetId, ids);
    return dispatchReparentToRack(targetId, ids);
  };

  const settleDialog = () => {
    const resolve = dialogResolveRef.current;
    dialogResolveRef.current = null;
    resolve?.();
  };

  const handleDialogCancel = () => {
    setSiteMoveConfirmation(null);
    settleDialog();
  };

  const handleDialogConfirm = async () => {
    if (!siteMoveConfirmation) return;
    await dispatchSiteMoveWithUnassign(siteMoveConfirmation);
    settleDialog();
  };

  const handleBuildingDialogCancel = () => {
    setBuildingMoveConfirmation(null);
    settleDialog();
  };

  const handleBuildingDialogConfirm = async () => {
    if (!buildingMoveConfirmation) return;
    await dispatchBuildingMoveWithUnassign(buildingMoveConfirmation);
    settleDialog();
  };

  // Resolve the operator's selection to a concrete id list plus the snapshots
  // (for conflict counting). Subset mode already has both; all-mode paginates
  // the snapshot endpoint behind a loading toast (same path as onConfirm).
  // Returns null on abort / error / empty so callers can bail quietly.
  const resolveSelectionIds = async (): Promise<{
    ids: string[];
    snapshots: Record<string, MinerStateSnapshot> | undefined;
  } | null> => {
    if (selectionMode === "all") {
      const effectiveFilter = applyFleetVisiblePairingStatuses(currentFilter);
      const loadingToast = pushToast({
        message: "Loading selected miners…",
        status: STATUSES.loading,
        longRunning: true,
      });
      let ids: string[];
      let snapshots: Record<string, MinerStateSnapshot>;
      try {
        const resolved = await resolveAllModeIds(effectiveFilter, abortRef.current?.signal);
        if (abortRef.current?.signal.aborted) {
          removeToast(loadingToast);
          return null;
        }
        ids = resolved.ids;
        snapshots = resolved.snapshots;
      } catch (err) {
        const message = err instanceof Error && err.message ? err.message : "Couldn't load selected miners. Try again.";
        updateToast(loadingToast, { message, status: STATUSES.error });
        return null;
      }
      removeToast(loadingToast);
      if (ids.length === 0) {
        pushToast({ message: "No miners selected.", status: STATUSES.queued });
        return null;
      }
      return { ids, snapshots };
    }
    if (deviceIdentifiers.length === 0) {
      pushToast({ message: "No miners selected.", status: STATUSES.queued });
      return null;
    }
    return { ids: deviceIdentifiers, snapshots: miners };
  };

  // Count selected miners that already have ANY placement (site, building, or
  // rack) — drives the create-flow pre-warn dialog, mirroring the reparent
  // flow. Every "New …" target displaces an existing placement: a new
  // rack/site re-stamps or clears it, and a new building cascades the
  // building's site onto the miner (which can differ from the miner's current
  // direct site), so a direct-site-only miner must warn here too.
  const countMinersWithPlacement = (
    ids: string[],
    snapshots: Record<string, MinerStateSnapshot> | undefined,
  ): number => {
    if (!snapshots) return 0;
    return ids.filter((id) => {
      const snapshot = snapshots[id];
      if (!snapshot) return false;
      return !!getMinerSiteLabel(snapshot) || !!getMinerBuildingLabel(snapshot) || !!getMinerRackLabel(snapshot);
    }).length;
  };

  // "New …" hand-off: resolve the selection, close the picker, then open the
  // hoisted create flow seeded with these miners. Each kind pre-warns when the
  // selection has placements the new parent would displace (conflictCount);
  // the provider shows the confirm dialog before entering the create flow.
  const createNewLaunchLabel = createFlow
    ? kind === "rack"
      ? "New rack"
      : kind === "building"
        ? "New building"
        : "New site"
    : undefined;

  const handleCreateNewLaunch = async () => {
    if (!createFlow) return;
    const resolved = await resolveSelectionIds();
    if (!resolved) return;
    const { ids, snapshots } = resolved;
    onClose();
    // Whether any selected miner is in a rack — gates the device assignment's
    // forceClearConflictingRackMembership (which the server treats as a
    // rack:manage action). Rack-less miners leave it off so site/building-only
    // operators aren't blocked.
    const racked = !!snapshots && ids.some((id) => !!snapshots[id] && !!getMinerRackLabel(snapshots[id]));
    if (kind === "rack") {
      createFlow.launchCreateRack({ minerIds: ids, conflictCount: countMinersWithPlacement(ids, snapshots) });
    } else if (kind === "building") {
      createFlow.launchCreateBuilding({
        rackIds: [],
        minerIds: ids,
        conflictCount: countMinersWithPlacement(ids, snapshots),
        forceClearRackMembership: racked,
      });
    } else {
      createFlow.launchCreateSite({
        buildingIds: [],
        rackIds: [],
        minerIds: ids,
        conflictCount: countMinersWithPlacement(ids, snapshots),
        forceClearRackMembership: racked,
      });
    }
  };

  const conflictRackLabels = siteMoveConfirmation ? Array.from(siteMoveConfirmation.conflictsByLabel.keys()) : [];
  const conflictCount = siteMoveConfirmation
    ? Array.from(siteMoveConfirmation.conflictsByLabel.values()).reduce((sum, list) => sum + list.length, 0)
    : 0;
  const conflictRacksSummary = conflictRackLabels.slice(0, 3).join(", ");
  const conflictRacksMore = conflictRackLabels.length > 3 ? ` and ${conflictRackLabels.length - 3} other rack(s)` : "";

  return (
    <>
      <ParentPickerModal
        kind={kind}
        show
        selectionMode="single"
        sourceLabel={
          selectionMode === "all" && totalCount !== undefined && totalCount !== deviceIdentifiers.length
            ? `${totalCount} miners`
            : sourceLabel
        }
        createNewLaunchLabel={createNewLaunchLabel}
        onCreateNewLaunch={createNewLaunchLabel ? () => void handleCreateNewLaunch() : undefined}
        onDismiss={onClose}
        // Returning a promise that only resolves after the dispatch
        // (including any cross-site confirm Dialog) completes keeps
        // ParentPickerModal from calling its own onDismiss → our
        // onClose before the Dialog has a chance to render.
        onConfirm={async (targetIds) => {
          const targetId = targetIds[0];
          if (targetId === undefined) return;

          let ids: string[];
          let snapshots: Record<string, MinerStateSnapshot> | undefined;
          if (selectionMode === "all") {
            // Mirror the miner table's default pairing-status scope so
            // "select all" stays aligned with the visible fleet rows.
            const effectiveFilter = applyFleetVisiblePairingStatuses(currentFilter);
            const loadingToast = pushToast({
              message: "Loading selected miners…",
              status: STATUSES.loading,
              longRunning: true,
            });
            try {
              const resolved = await resolveAllModeIds(effectiveFilter, abortRef.current?.signal);
              if (abortRef.current?.signal.aborted) {
                removeToast(loadingToast);
                return;
              }
              ids = resolved.ids;
              snapshots = resolved.snapshots;
            } catch (err) {
              const message =
                err instanceof Error && err.message ? err.message : "Couldn't load selected miners. Try again.";
              updateToast(loadingToast, { message, status: STATUSES.error });
              return;
            }
            removeToast(loadingToast);
            if (ids.length === 0) {
              pushToast({ message: "No miners selected.", status: STATUSES.queued });
              return;
            }
          } else {
            if (deviceIdentifiers.length === 0) {
              pushToast({ message: "No miners selected.", status: STATUSES.queued });
              return;
            }
            ids = deviceIdentifiers;
            snapshots = miners;
          }

          await dispatchReparent(targetId, ids, snapshots);
        }}
      />
      {siteMoveConfirmation ? (
        <Dialog
          open
          title="Move miners between sites?"
          subtitle={`${conflictCount} of the selected miners are currently in ${conflictRacksSummary}${conflictRacksMore}, which belong${conflictRackLabels.length === 1 ? "s" : ""} to a different site. Continuing will unassign them from those rack${conflictRackLabels.length === 1 ? "" : "s"} before moving them to the selected site.`}
          onDismiss={() => {
            if (siteMoveInFlight) return;
            handleDialogCancel();
          }}
          buttons={[
            {
              text: "Cancel",
              variant: variants.secondary,
              onClick: handleDialogCancel,
              disabled: siteMoveInFlight,
            },
            {
              text: "Continue",
              variant: variants.primary,
              onClick: () => {
                void handleDialogConfirm();
              },
              loading: siteMoveInFlight,
              disabled: siteMoveInFlight,
            },
          ]}
        />
      ) : null}
      {buildingMoveConfirmation ? (
        <Dialog
          open
          title="Move miners between buildings?"
          subtitle={`${buildingMoveConfirmation.conflictCount} of the selected miner${buildingMoveConfirmation.conflictCount === 1 ? " is" : "s are"} currently in a rack that belongs to a different building. Continuing will unassign ${buildingMoveConfirmation.conflictCount === 1 ? "it" : "them"} from those racks before moving to the selected building.`}
          onDismiss={() => {
            if (buildingMoveInFlight) return;
            handleBuildingDialogCancel();
          }}
          buttons={[
            {
              text: "Cancel",
              variant: variants.secondary,
              onClick: handleBuildingDialogCancel,
              disabled: buildingMoveInFlight,
            },
            {
              text: "Continue",
              variant: variants.primary,
              onClick: () => {
                void handleBuildingDialogConfirm();
              },
              loading: buildingMoveInFlight,
              disabled: buildingMoveInFlight,
            },
          ]}
        />
      ) : null}
      {rackSiteStripConfirmation ? (
        <Dialog
          open
          title="Move miners to an unassigned rack?"
          subtitle={`This rack has no site or building, so ${rackSiteStripConfirmation.conflictCount} of the selected miner${rackSiteStripConfirmation.conflictCount === 1 ? " is" : "s are"} currently assigned to one. Continuing will clear ${rackSiteStripConfirmation.conflictCount === 1 ? "its site/building assignment" : "their site/building assignments"} to match the rack.`}
          onDismiss={() => {
            if (rackStripInFlight) return;
            handleRackStripDialogCancel();
          }}
          buttons={[
            {
              text: "Cancel",
              variant: variants.secondary,
              onClick: handleRackStripDialogCancel,
              disabled: rackStripInFlight,
            },
            {
              text: "Continue",
              variant: variants.primary,
              onClick: () => {
                void handleRackStripDialogConfirm();
              },
              loading: rackStripInFlight,
              disabled: rackStripInFlight,
            },
          ]}
        />
      ) : null}
    </>
  );
};

export default MinerReparentPicker;
