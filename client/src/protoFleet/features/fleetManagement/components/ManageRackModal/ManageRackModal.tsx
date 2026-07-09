import { useCallback, useEffect, useMemo, useState } from "react";

import ManageMinersModal from "./ManageMinersModal";
import MinersPane from "./MinersPane";
import RackPane from "./RackPane";
import ReparentWarningDialog from "./ReparentWarningDialog";
import ScanMinerQrModal from "./ScanMinerQrModal";
import SearchMinersModal from "./SearchMinersModal";
import { type AssignmentMode, orderIndexToOrigin, originLabel, type RackFormData, type SelectedSlot } from "./types";
import { fetchAllMinerSnapshots } from "@/protoFleet/api/fetchAllMinerSnapshots";
import { type DeviceSet, type RackSlot } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import {
  type MinerListFilter,
  type MinerStateSnapshot,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import useFleet from "@/protoFleet/api/useFleet";
import FullScreenTwoPaneModal from "@/protoFleet/components/FullScreenTwoPaneModal";
import type { MinerEligibility } from "@/protoFleet/components/MinerSelectionList";
import RackSettingsModal from "@/protoFleet/features/fleetManagement/components/RackSettingsModal";
import { isMinerSnapshotIneligible } from "@/protoFleet/features/fleetManagement/utils/minerPlacement";
import { slotNumberToRowCol } from "@/protoFleet/features/fleetManagement/utils/slotNumbering";
import { useHasPermission } from "@/protoFleet/store";

import { DismissCircle } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Dialog from "@/shared/components/Dialog";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { pushToast, STATUSES } from "@/shared/features/toaster";

/** Fetch all miner IDs eligible for a rack by paginating through the fleet API.
 *  Applies the same filter the user had active in MinerSelectionList so "select all"
 *  respects model/subnet filters. Miners in a different rack/building/site are
 *  excluded id-based (matches the list's eligibility predicate) so "select all"
 *  can't pull in ineligible miners even if the assignable-only toggle was off. */
async function fetchAllSelectableMinerIds(
  eligibility: MinerEligibility,
  listFilter?: MinerListFilter,
): Promise<string[]> {
  const filter = listFilter
    ? { ...listFilter, pairingStatuses: [PairingStatus.PAIRED] }
    : { pairingStatuses: [PairingStatus.PAIRED] };
  const snapshots = await fetchAllMinerSnapshots(filter);
  return Object.values(snapshots)
    .filter((m) => !isMinerSnapshotIneligible(m, eligibility))
    .map((m) => m.deviceIdentifier);
}

/** Remove the first entry whose value matches `target` from a record, returning a shallow copy. */
function removeAssignmentByValue(record: Record<string, string>, target: string): Record<string, string> {
  const next = { ...record };
  for (const [k, v] of Object.entries(next)) {
    if (v === target) {
      delete next[k];
      break;
    }
  }
  return next;
}

/** Keep only entries whose value is in `keepSet`, returning a shallow copy. */
function filterAssignmentsByValues(record: Record<string, string>, keepSet: Set<string>): Record<string, string> {
  const next: Record<string, string> = {};
  for (const [k, v] of Object.entries(record)) {
    if (keepSet.has(v)) next[k] = v;
  }
  return next;
}

interface ManageRackModalProps {
  show: boolean;
  rackSettings: RackFormData;
  existingRackId?: bigint;
  existingRacks: DeviceSet[];
  // Pre-seeds the new rack's miner list (e.g. from a bulk "Add to rack →
  // New rack" flow) so the selected miners land in the left pane ready
  // for slot assignment. Ignored in edit mode (existingRackId set).
  seededMinerIds?: string[];
  // Page-header site scope (single-site only). Forwarded to the embedded
  // RackSettingsModal so a new rack created within a site scope keeps its Site
  // field locked to that scope. Ignored for an existing rack (edit).
  scopedSiteId?: bigint;
  onDismiss: () => void;
  onSave: () => void;
  onDelete?: () => Promise<void> | void;
}

export default function ManageRackModal({
  show,
  rackSettings: initialRackSettings,
  existingRackId,
  existingRacks,
  seededMinerIds,
  scopedSiteId,
  onDismiss,
  onSave,
  onDelete,
}: ManageRackModalProps) {
  const { saveRack, getRackSlots, listGroupMembers } = useDeviceSets();
  // Rack placement (site/building) is a site:manage action, enforced server-
  // side on SaveRack. A rack:manage-only operator edits rack contents without
  // touching placement, so we omit placement from the request (preserving the
  // rack's current site/building) rather than sending an explicit change.
  const canManagePlacement = useHasPermission("site:manage");

  // Fetch all miners for display data (name, IP, model, etc.)
  const { miners: minersMap } = useFleet({ pageSize: 1000 });
  const allMiners = useMemo(() => minersMap as Record<string, MinerStateSnapshot>, [minersMap]);

  // Rack settings (can be updated via RackSettingsModal)
  const [rackSettings, setRackSettings] = useState<RackFormData>(initialRackSettings);
  const totalSlots = rackSettings.rows * rackSettings.columns;
  const numberingOrigin = orderIndexToOrigin(rackSettings.orderIndex);

  // Target-rack placement for the selection modals' eligibility filter.
  // rackSettings always reflects the rack's LIVE persisted placement: a
  // Site/Building change in Rack Settings is persisted immediately on Continue
  // (handleRackSettingsUpdate), which also cascades the rack's members to the
  // new placement. So by the time this filter runs, the rack and its members
  // are already at the placement in rackSettings — no current-vs-pending split,
  // and a miner already at the new destination reads as assignable. A new rack
  // has no persisted placement, so rackSettings is the intended placement.
  const eligibility = useMemo<MinerEligibility>(
    () => ({
      rackId: existingRackId,
      siteId: rackSettings.siteId,
      buildingId: rackSettings.buildingId,
    }),
    [existingRackId, rackSettings.siteId, rackSettings.buildingId],
  );

  // Core assignment state. A new rack (no existingRackId) can be seeded
  // with miners from a bulk "Add to rack → New rack" flow; edit mode
  // ignores the seed and loads the rack's real membership below.
  const [rackMiners, setRackMiners] = useState<string[]>(() => (existingRackId ? [] : (seededMinerIds ?? [])));
  const [slotAssignments, setSlotAssignments] = useState<Record<string, string>>({});
  const [assignmentMode, setAssignmentMode] = useState<AssignmentMode>("manual");
  const [manualAssignmentCache, setManualAssignmentCache] = useState<Record<string, string>>({});
  const [selectedMinerId, setSelectedMinerId] = useState<string | null>(null);

  // Cell-first selection state
  const [selectedSlot, setSelectedSlot] = useState<SelectedSlot | null>(null);
  const [showSlotPopover, setShowSlotPopover] = useState(false);
  const [hoveredMinerId, setHoveredMinerId] = useState<string | null>(null);

  // Sub-modal visibility
  const [showRackSettings, setShowRackSettings] = useState(false);
  const [showManageMiners, setShowManageMiners] = useState(false);
  const [showSearchMiners, setShowSearchMiners] = useState(false);
  const [showScanQr, setShowScanQr] = useState(false);
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);

  // Pending reparent confirmation. Set when a confirm action would pull miners
  // out of a rack/building/site they're currently assigned to; `onConfirm`
  // runs the deferred action once the operator accepts the warning (#672).
  const [reparentConfirm, setReparentConfirm] = useState<{ count: number; onConfirm: () => void } | null>(null);

  // Loading / error state
  const [isLoading, setIsLoading] = useState(!!existingRackId);
  const [loadFailed, setLoadFailed] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [errorMsg, setErrorMsg] = useState("");

  // No longer need initial state snapshots — saveRack replaces membership atomically.

  // Fetch existing data for edit mode
  useEffect(() => {
    if (!existingRackId) return;

    let cancelled = false;
    let loadedMembers = false;
    let loadedSlots = false;
    let members: string[] = [];
    let slots: RackSlot[] = [];

    const maybeFinish = () => {
      if (!loadedMembers || !loadedSlots || cancelled) return;
      setRackMiners(members);

      const assignments: Record<string, string> = {};
      for (const slot of slots) {
        if (slot.position) {
          assignments[`${slot.position.row}-${slot.position.column}`] = slot.deviceIdentifier;
        }
      }
      setSlotAssignments(assignments);
      setManualAssignmentCache(assignments);
      setIsLoading(false);
    };

    listGroupMembers({
      deviceSetId: existingRackId,
      onSuccess: (ids) => {
        members = ids;
        loadedMembers = true;
        maybeFinish();
      },
      onError: () => {
        if (!cancelled) {
          setIsLoading(false);
          setLoadFailed(true);
          setErrorMsg("Failed to load rack data. Please close and try again.");
        }
      },
    });

    getRackSlots({
      deviceSetId: existingRackId,
      onSuccess: (s) => {
        slots = s;
        loadedSlots = true;
        maybeFinish();
      },
      onError: () => {
        if (!cancelled) {
          setIsLoading(false);
          setLoadFailed(true);
          setErrorMsg("Failed to load rack data. Please close and try again.");
        }
      },
    });

    return () => {
      cancelled = true;
    };
  }, [existingRackId, listGroupMembers, getRackSlots]);

  // Compute the active assignments based on mode
  const activeAssignments = useMemo(() => {
    if (assignmentMode === "manual") return slotAssignments;

    // Build auto-assignments based on sort order
    const sorted = [...rackMiners];
    if (assignmentMode === "byName") {
      sorted.sort((a, b) => {
        const nameA = allMiners[a]?.name || a;
        const nameB = allMiners[b]?.name || b;
        return nameA.localeCompare(nameB);
      });
    } else {
      // byNetwork — sort by zero-padded IP octets
      const padIp = (ip: string) => ip.replace(/\d+/g, (n) => n.padStart(3, "0"));
      sorted.sort((a, b) => {
        const ipA = allMiners[a]?.ipAddress || "";
        const ipB = allMiners[b]?.ipAddress || "";
        return padIp(ipA).localeCompare(padIp(ipB));
      });
    }

    const auto: Record<string, string> = {};
    const slotsCount = Math.min(sorted.length, totalSlots);
    for (let i = 0; i < slotsCount; i++) {
      const { row, col } = slotNumberToRowCol(i + 1, rackSettings.rows, rackSettings.columns, numberingOrigin);
      auto[`${row}-${col}`] = sorted[i];
    }
    return auto;
  }, [
    assignmentMode,
    slotAssignments,
    rackMiners,
    allMiners,
    totalSlots,
    rackSettings.rows,
    rackSettings.columns,
    numberingOrigin,
  ]);

  const assignedCount = Object.keys(activeAssignments).length;

  // Mode switching with cache
  const handleModeChange = useCallback(
    (mode: AssignmentMode) => {
      if (assignmentMode === "manual") {
        setManualAssignmentCache({ ...slotAssignments });
      }
      if (mode === "manual") {
        setSlotAssignments({ ...manualAssignmentCache });
      }
      setAssignmentMode(mode);
      setSelectedMinerId(null);
      setSelectedSlot(null);
      setShowSlotPopover(false);
    },
    [assignmentMode, slotAssignments, manualAssignmentCache],
  );

  // Cell click handler — if a miner is selected, assign directly; otherwise show popover
  const handleCellClick = useCallback(
    (row: number, col: number) => {
      if (assignmentMode !== "manual") return;
      const key = `${row}-${col}`;

      // Miner-first flow: a miner is selected and the slot is empty — assign immediately
      if (selectedMinerId && !slotAssignments[key]) {
        setSlotAssignments((prev) => {
          const next = removeAssignmentByValue(prev, selectedMinerId);
          next[key] = selectedMinerId;
          return next;
        });
        setSelectedMinerId(null);
        return;
      }

      // Cell-first flow: no miner selected — show popover
      setSelectedSlot({ row, col, key });
      setShowSlotPopover(true);
      setSelectedMinerId(null);
    },
    [assignmentMode, selectedMinerId, slotAssignments],
  );

  // Popover: "Select from list" — keep cell selected, wait for miner click
  const handleSelectFromList = useCallback(() => {
    setShowSlotPopover(false);
  }, []);

  // Popover: "Search miners" — open SearchMinersModal
  const handleSearchMiners = useCallback(() => {
    setShowSlotPopover(false);
    setShowSearchMiners(true);
  }, []);

  // Popover: "Scan QR code" — open ScanMinerQrModal
  const handleScanQr = useCallback(() => {
    setShowSlotPopover(false);
    setShowScanQr(true);
  }, []);

  // Popover dismiss — deselect cell
  const handlePopoverDismiss = useCallback(() => {
    setSelectedSlot(null);
    setShowSlotPopover(false);
  }, []);

  // Show the reparent warning (#672) when `count` > 0, else run `proceed`
  // directly. `proceed` runs once the operator accepts the warning. Callers pass
  // the reassignment count from a reliable source (the selection list's
  // per-row placement, or the scanned miner's snapshot) rather than the parent's
  // first-page-only `allMiners` cache, so the warning isn't missed for miners
  // outside that page.
  const promptReparent = useCallback((count: number, proceed: () => void) => {
    if (count === 0) {
      proceed();
      return;
    }
    setReparentConfirm({ count, onConfirm: proceed });
  }, []);

  // SearchMinersModal confirm — add miner to rack and assign to selected slot.
  // The modal reports the reassignment flag from the row it selected (exact even
  // for fleets larger than the display page).
  const handleSearchMinerConfirm = useCallback(
    (minerId: string, isReassignment: boolean) => {
      if (!selectedSlot) return;
      const slotKey = selectedSlot.key;
      promptReparent(isReassignment ? 1 : 0, () => {
        // Add miner to rack if not already present
        setRackMiners((prev) => (prev.includes(minerId) ? prev : [...prev, minerId]));
        // Remove any existing assignment for this miner, then assign to selected slot
        setSlotAssignments((prev) => {
          const next = removeAssignmentByValue(prev, minerId);
          next[slotKey] = minerId;
          return next;
        });
        setSelectedSlot(null);
        setShowSearchMiners(false);
      });
    },
    [selectedSlot, promptReparent],
  );

  // ScanMinerQrModal confirm — identical placement to search, but keyed off
  // the resolved device identifier from the scanned serial. The scan modal
  // computes the reassignment flag from the resolved snapshot's placement.
  const handleScanMinerConfirm = useCallback(
    (minerId: string, isReassignment: boolean) => {
      if (!selectedSlot) return;
      const slotKey = selectedSlot.key;
      promptReparent(isReassignment ? 1 : 0, () => {
        setRackMiners((prev) => (prev.includes(minerId) ? prev : [...prev, minerId]));
        setSlotAssignments((prev) => {
          const next = removeAssignmentByValue(prev, minerId);
          next[slotKey] = minerId;
          return next;
        });
        setSelectedSlot(null);
        setShowScanQr(false);
      });
    },
    [selectedSlot, promptReparent],
  );

  // Miner selection handler — when a slot is awaiting, assign miner to it
  const handleSelectMiner = useCallback(
    (deviceId: string | null) => {
      if (selectedSlot && deviceId) {
        // Assign this miner to the selected slot
        setRackMiners((prev) => (prev.includes(deviceId) ? prev : [...prev, deviceId]));
        setSlotAssignments((prev) => {
          const next = removeAssignmentByValue(prev, deviceId);
          next[selectedSlot.key] = deviceId;
          return next;
        });
        setSelectedSlot(null);
        setSelectedMinerId(null);
      } else {
        setSelectedMinerId(deviceId);
      }
    },
    [selectedSlot],
  );

  // Clear all assignments
  const handleClearAssignments = useCallback(() => {
    setSlotAssignments({});
    setManualAssignmentCache({});
    setSelectedMinerId(null);
  }, []);

  // Remove miner from rack
  const handleRemoveMiner = useCallback(
    (deviceId: string) => {
      setRackMiners((prev) => prev.filter((id) => id !== deviceId));
      setSlotAssignments((prev) => removeAssignmentByValue(prev, deviceId));
      setManualAssignmentCache((prev) => removeAssignmentByValue(prev, deviceId));
      if (selectedMinerId === deviceId) setSelectedMinerId(null);
    },
    [selectedMinerId],
  );

  // Unassign miner from slot (keep in rack)
  const handleUnassignMiner = useCallback(
    (deviceId: string) => {
      setSlotAssignments((prev) => removeAssignmentByValue(prev, deviceId));
      setManualAssignmentCache((prev) => removeAssignmentByValue(prev, deviceId));
      if (selectedMinerId === deviceId) setSelectedMinerId(null);
    },
    [selectedMinerId],
  );

  // ManageMinersModal confirm handler. Returns an error string for the still-open
  // modal to surface (or undefined on success) — the parent's own callout sits
  // behind the modal, so select-all overflow/load errors must go back up.
  const handleManageMinersConfirm = useCallback(
    async (
      selectedIds: string[],
      allSelected: boolean,
      listFilter: MinerListFilter | undefined,
      reassignedItems: string[],
    ): Promise<string | undefined> => {
      let finalIds = selectedIds;
      // "Select all" resolves to the assignable set server-side (ineligible
      // miners already excluded), so it can never reparent. An explicit
      // selection can, and `reassignedItems` reports exactly which picks are
      // assigned elsewhere.
      let reassignedCount = reassignedItems.length;

      if (allSelected) {
        // When "select all" is active, selectedIds only contains the current page.
        // Paginate through all miners server-side to get the complete list, applying
        // the same filters the user had active (e.g. model/subnet) and excluding
        // miners in a different rack/building/site (id-based).
        try {
          setIsLoading(true);
          finalIds = await fetchAllSelectableMinerIds(eligibility, listFilter);
        } catch {
          return "Failed to load all miners. Please try again.";
        } finally {
          setIsLoading(false);
        }
        reassignedCount = 0;
      }

      if (finalIds.length > totalSlots) {
        return `Cannot add ${finalIds.length} miners with only ${totalSlots} available slots. Deselect some miners or update your rack settings.`;
      }

      promptReparent(reassignedCount, () => {
        setRackMiners(finalIds);
        setShowManageMiners(false);

        // Remove assignments for miners no longer in rack
        const keepSet = new Set(finalIds);
        setSlotAssignments((prev) => filterAssignmentsByValues(prev, keepSet));
        setManualAssignmentCache((prev) => filterAssignmentsByValues(prev, keepSet));
      });
      return undefined;
    },
    [eligibility, totalSlots, promptReparent],
  );

  // RackSettingsModal edit handler. For an existing rack, a Site/Building
  // change is persisted right here (not deferred to the final Save) so the
  // rack — and its server-cascaded members — are already at the new placement
  // before the operator opens Manage Miners; otherwise the assignable-only
  // list would judge miners against a placement that isn't live yet. Only a
  // real placement change triggers this; label/zone/dimension edits stay in
  // memory until Save. Requires site:manage (already enforced on SaveRack).
  const handleRackSettingsUpdate = useCallback(
    async (formData: RackFormData) => {
      const placementChanged =
        existingRackId !== undefined &&
        canManagePlacement &&
        (formData.siteId !== rackSettings.siteId || formData.buildingId !== rackSettings.buildingId);

      if (placementChanged) {
        const slotAssignmentsList = Object.entries(activeAssignments).map(([key, deviceId]) => {
          const [row, col] = key.split("-").map(Number);
          return { deviceIdentifier: deviceId, row, column: col };
        });
        try {
          await new Promise<void>((resolve, reject) => {
            saveRack({
              deviceSetId: existingRackId,
              label: formData.label,
              zone: formData.zone,
              rows: formData.rows,
              columns: formData.columns,
              orderIndex: formData.orderIndex,
              coolingType: formData.coolingType,
              deviceIdentifiers: rackMiners,
              slotAssignments: slotAssignmentsList,
              siteId: formData.siteId ?? 0n,
              buildingId: formData.buildingId ?? 0n,
              onSuccess: () => resolve(),
              onError: (msg) => reject(new Error(msg)),
            });
          });
        } catch (err) {
          pushToast({
            message: getErrorMessage(err, "Failed to update rack placement. Please try again."),
            status: STATUSES.error,
          });
          // Keep Rack Settings open (don't apply) so the operator can retry.
          return;
        }
      }

      setRackSettings(formData);
      setShowRackSettings(false);
    },
    [existingRackId, canManagePlacement, rackSettings, rackMiners, activeAssignments, saveRack],
  );

  // Save handler — single atomic RPC
  const handleSave = useCallback(async () => {
    // Capacity guard. handleManageMinersConfirm enforces this when miners are
    // added through the sub-modal, but a seeded new rack (bulk "New rack")
    // populates rackMiners directly and bypasses that path — saveRack accepts
    // members beyond the slot count, so an over-fill would persist silently.
    if (rackMiners.length > totalSlots) {
      setErrorMsg(
        `Cannot add ${rackMiners.length} miners with only ${totalSlots} available slots. Deselect some miners or update your rack settings.`,
      );
      return;
    }

    setIsSaving(true);
    setErrorMsg("");

    try {
      // Build slot assignments from the active assignments map
      const slotAssignmentsList = Object.entries(activeAssignments).map(([key, deviceId]) => {
        const [row, col] = key.split("-").map(Number);
        return { deviceIdentifier: deviceId, row, column: col };
      });

      await new Promise<void>((resolve, reject) => {
        saveRack({
          deviceSetId: existingRackId,
          label: rackSettings.label,
          zone: rackSettings.zone,
          rows: rackSettings.rows,
          columns: rackSettings.columns,
          orderIndex: rackSettings.orderIndex,
          coolingType: rackSettings.coolingType,
          deviceIdentifiers: rackMiners,
          slotAssignments: slotAssignmentsList,
          // Placement from the dropdowns, only when the operator can manage
          // site placement — otherwise omit it (preserve current / create
          // unplaced) so a rack:manage-only edit isn't rejected server-side.
          // On edit, send an explicit 0 for an Unassigned selection so it
          // actually clears (omitting would preserve). On create there's
          // nothing to preserve, so unfilled levels are undefined → NULL.
          siteId: canManagePlacement
            ? existingRackId !== undefined
              ? (rackSettings.siteId ?? 0n)
              : rackSettings.siteId
            : undefined,
          buildingId: canManagePlacement
            ? existingRackId !== undefined
              ? (rackSettings.buildingId ?? 0n)
              : rackSettings.buildingId
            : undefined,
          onSuccess: () => resolve(),
          onError: (msg) => reject(new Error(msg)),
        });
      });

      pushToast({
        message: existingRackId ? `Rack "${rackSettings.label}" updated` : `Rack "${rackSettings.label}" created`,
        status: STATUSES.success,
      });
      onSave();
    } catch (err) {
      setErrorMsg(getErrorMessage(err, "Failed to save. Please try again."));
    } finally {
      setIsSaving(false);
    }
  }, [existingRackId, rackSettings, rackMiners, totalSlots, activeAssignments, canManagePlacement, saveRack, onSave]);

  if (!show) return null;

  return (
    <>
      <FullScreenTwoPaneModal
        open={show}
        title={rackSettings.label}
        onDismiss={onDismiss}
        isBusy={isSaving}
        buttons={[
          ...(onDelete
            ? [
                {
                  text: "Delete Rack",
                  variant: variants.secondaryDanger,
                  onClick: () => setShowDeleteConfirm(true),
                },
              ]
            : []),
          {
            text: "Edit Rack Settings",
            variant: variants.secondary,
            onClick: () => setShowRackSettings(true),
          },
          {
            text: "Manage Miners",
            variant: variants.secondary,
            onClick: () => setShowManageMiners(true),
          },
          {
            text: isSaving ? "Saving..." : "Save",
            variant: variants.primary,
            disabled: isSaving || isLoading || loadFailed,
            loading: isSaving,
            onClick: handleSave,
          },
        ]}
        abovePanes={
          errorMsg ? (
            <div className="shrink-0 px-2 pb-4">
              <Callout
                intent="danger"
                prefixIcon={<DismissCircle />}
                title={errorMsg}
                dismissible
                onDismiss={() => setErrorMsg("")}
              />
            </div>
          ) : undefined
        }
        loadingState={
          isLoading ? (
            <div className="flex flex-1 items-center justify-center">
              <ProgressCircular indeterminate />
            </div>
          ) : undefined
        }
        primaryPane={
          <MinersPane
            rackMiners={rackMiners}
            miners={allMiners}
            slotAssignments={activeAssignments}
            assignmentMode={assignmentMode}
            selectedMinerId={selectedMinerId}
            selectedSlot={selectedSlot}
            rows={rackSettings.rows}
            cols={rackSettings.columns}
            numberingOrigin={numberingOrigin}
            onModeChange={handleModeChange}
            onSelectMiner={handleSelectMiner}
            onRemoveMiner={handleRemoveMiner}
            onUnassignMiner={handleUnassignMiner}
            onClearAssignments={handleClearAssignments}
            hoveredMinerId={hoveredMinerId}
            onOpenManageMiners={() => setShowManageMiners(true)}
          />
        }
        secondaryPane={
          <RackPane
            rows={rackSettings.rows}
            cols={rackSettings.columns}
            numberingOrigin={numberingOrigin}
            slotAssignments={activeAssignments}
            assignmentMode={assignmentMode}
            assignedCount={assignedCount}
            totalSlots={totalSlots}
            originLabel={originLabel(numberingOrigin)}
            selectedSlotKey={selectedSlot?.key ?? null}
            showPopover={showSlotPopover}
            hasMiners={rackMiners.length > 0}
            onCellClick={handleCellClick}
            onSelectFromList={handleSelectFromList}
            onSearchMiners={handleSearchMiners}
            onScanQr={handleScanQr}
            onPopoverDismiss={handlePopoverDismiss}
            onHoverMiner={setHoveredMinerId}
          />
        }
      />

      {showRackSettings ? (
        <RackSettingsModal
          show={showRackSettings}
          existingRacks={existingRacks}
          initialFormData={rackSettings}
          existingRack={existingRackId !== undefined}
          defaultSiteId={scopedSiteId}
          onDismiss={() => setShowRackSettings(false)}
          onContinue={handleRackSettingsUpdate}
        />
      ) : null}

      {showManageMiners ? (
        <ManageMinersModal
          show={showManageMiners}
          currentRackMiners={rackMiners}
          eligibility={eligibility}
          targetRackLabel={rackSettings.label}
          maxSlots={totalSlots}
          onDismiss={() => setShowManageMiners(false)}
          onConfirm={handleManageMinersConfirm}
        />
      ) : null}

      {showSearchMiners ? (
        <SearchMinersModal
          show={showSearchMiners}
          eligibility={eligibility}
          targetRackLabel={rackSettings.label}
          onDismiss={() => {
            setShowSearchMiners(false);
            setSelectedSlot(null);
          }}
          onConfirm={handleSearchMinerConfirm}
        />
      ) : null}

      {showScanQr ? (
        <ScanMinerQrModal
          show={showScanQr}
          currentRackLabel={initialRackSettings.label}
          eligibility={eligibility}
          onDismiss={() => {
            setShowScanQr(false);
            setSelectedSlot(null);
          }}
          onConfirm={handleScanMinerConfirm}
        />
      ) : null}

      {reparentConfirm ? (
        <ReparentWarningDialog
          count={reparentConfirm.count}
          rackLabel={rackSettings.label}
          onCancel={() => setReparentConfirm(null)}
          onConfirm={() => {
            const proceed = reparentConfirm.onConfirm;
            setReparentConfirm(null);
            proceed();
          }}
        />
      ) : null}

      {showDeleteConfirm && onDelete ? (
        <Dialog
          title={`Delete "${rackSettings.label}"?`}
          subtitle="This action cannot be undone. The miners in this rack will not be affected."
          onDismiss={() => setShowDeleteConfirm(false)}
          buttons={[
            {
              text: "Cancel",
              onClick: () => setShowDeleteConfirm(false),
              variant: variants.secondary,
            },
            {
              text: "Delete",
              onClick: async () => {
                setIsDeleting(true);
                try {
                  await onDelete();
                } catch {
                  setIsDeleting(false);
                  setShowDeleteConfirm(false);
                }
              },
              variant: variants.danger,
              loading: isDeleting,
            },
          ]}
        />
      ) : null}
    </>
  );
}
