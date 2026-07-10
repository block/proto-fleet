import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import ManageMinersModal from "./ManageMinersModal";
import MinersPane from "./MinersPane";
import RackPane from "./RackPane";
import ReparentWarningDialog from "./ReparentWarningDialog";
import ScanMinerQrModal, { type ScanAssignmentResult } from "./ScanMinerQrModal";
import SearchMinersModal from "./SearchMinersModal";
import { type AssignmentMode, orderIndexToOrigin, originLabel, type RackFormData, type SelectedSlot } from "./types";
import { useRackMinerScope } from "./useRackMinerScope";
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
  // Fired after the Rack Settings "Continue" persists an EXISTING rack's
  // settings (label/placement/zone/dims) — which happens before the final
  // miner Save. Parents should refetch in the background so the rack list /
  // overview stays consistent even if the operator dismisses the modal
  // without pressing Save. No-op for a new rack (nothing is persisted yet).
  onSettingsPersisted?: () => void;
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
  onSettingsPersisted,
  onDelete,
}: ManageRackModalProps) {
  const { saveRack, updateRack, getDeviceSet, getRackSlots, listGroupMembers } = useDeviceSets();
  // Rack placement (site/building) is a site:manage action, enforced server-
  // side on SaveRack and UpdateDeviceSet. A rack:manage-only operator edits
  // rack contents and metadata (label/zone/dims) without touching placement,
  // so we omit placement from the request (preserving the rack's current
  // site/building) rather than sending an explicit change.
  const canManagePlacement = useHasPermission("site:manage");

  // Header SitePicker scope, forwarded to the miner-selection sub-modals.
  const scope = useRackMinerScope();

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
  const preserveSelectedSlotForPopoverAction = useRef(false);
  const [hoveredMinerId, setHoveredMinerId] = useState<string | null>(null);

  // Sub-modal visibility
  const [showRackSettings, setShowRackSettings] = useState(false);
  const [showManageMiners, setShowManageMiners] = useState(false);
  const [showSearchMiners, setShowSearchMiners] = useState(false);
  const [showScanQr, setShowScanQr] = useState(false);
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const scanUndoRef = useRef<(() => void) | null>(null);

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

  const getSlotByNumber = useCallback(
    (slotNumber: number): SelectedSlot => {
      const { row, col } = slotNumberToRowCol(slotNumber, rackSettings.rows, rackSettings.columns, numberingOrigin);
      return { row, col, key: `${row}-${col}` };
    },
    [rackSettings.rows, rackSettings.columns, numberingOrigin],
  );

  const getSlotNumber = useCallback(
    (slot: SelectedSlot): number | null => {
      for (let slotNumber = 1; slotNumber <= totalSlots; slotNumber++) {
        if (getSlotByNumber(slotNumber).key === slot.key) return slotNumber;
      }
      return null;
    },
    [getSlotByNumber, totalSlots],
  );

  const getSlotLabel = useCallback(
    (slot: SelectedSlot): string => {
      const slotNumber = getSlotNumber(slot);
      return slotNumber ? `Slot ${slotNumber}` : "Selected slot";
    },
    [getSlotNumber],
  );

  const getNextAssignableSlot = useCallback(
    (fromSlot: SelectedSlot, assignments: Record<string, string>): SelectedSlot | null => {
      const fromSlotNumber = getSlotNumber(fromSlot);
      if (!fromSlotNumber) return null;

      for (let slotNumber = fromSlotNumber + 1; slotNumber <= totalSlots; slotNumber++) {
        const slot = getSlotByNumber(slotNumber);
        if (!assignments[slot.key]) return slot;
      }
      return null;
    },
    [getSlotByNumber, getSlotNumber, totalSlots],
  );

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
  const preserveSelectedSlotThroughActionSheetClose = useCallback(() => {
    preserveSelectedSlotForPopoverAction.current = true;
    queueMicrotask(() => {
      preserveSelectedSlotForPopoverAction.current = false;
    });
  }, []);

  const handleSelectFromList = useCallback(() => {
    preserveSelectedSlotThroughActionSheetClose();
    setShowSlotPopover(false);
  }, [preserveSelectedSlotThroughActionSheetClose]);

  // Popover: "Search miners" — open SearchMinersModal
  const handleSearchMiners = useCallback(() => {
    preserveSelectedSlotThroughActionSheetClose();
    setShowSlotPopover(false);
    setShowSearchMiners(true);
  }, [preserveSelectedSlotThroughActionSheetClose]);

  // Popover: "Scan to assign" — open ScanMinerQrModal
  const handleScanQr = useCallback(() => {
    preserveSelectedSlotThroughActionSheetClose();
    setShowSlotPopover(false);
    setShowScanQr(true);
    scanUndoRef.current = null;
  }, [preserveSelectedSlotThroughActionSheetClose]);

  // Popover dismiss — close canceled slot actions and clear the slot context.
  // `handleSelectFromList` preserves the selected slot for the intentional
  // cell-first assignment flow.
  const handlePopoverDismiss = useCallback(() => {
    setShowSlotPopover(false);
    if (preserveSelectedSlotForPopoverAction.current) {
      return;
    }
    setSelectedSlot(null);
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

  const handleScanMinerAssign = useCallback(
    (minerId: string): ScanAssignmentResult | null => {
      if (!selectedSlot) return null;

      const previousRackMiners = rackMiners;
      const previousSlotAssignments = slotAssignments;
      const assignedSlot = selectedSlot;
      const nextSlotAssignments = removeAssignmentByValue(slotAssignments, minerId);
      nextSlotAssignments[assignedSlot.key] = minerId;

      setRackMiners((prev) => (prev.includes(minerId) ? prev : [...prev, minerId]));
      setSlotAssignments(nextSlotAssignments);

      scanUndoRef.current = () => {
        setRackMiners(previousRackMiners);
        setSlotAssignments(previousSlotAssignments);
        setSelectedSlot(assignedSlot);
      };

      return {
        slotLabel: getSlotLabel(assignedSlot),
        hasNextSlot: !!getNextAssignableSlot(assignedSlot, nextSlotAssignments),
      };
    },
    [getNextAssignableSlot, getSlotLabel, rackMiners, selectedSlot, slotAssignments],
  );

  // Scanned miners already assigned elsewhere use the same reparent warning as
  // search/list assignment. The success dialog is reserved for immediate
  // assignments that did not require that warning.
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
        scanUndoRef.current = null;
      });
    },
    [selectedSlot, promptReparent],
  );

  const handleScanAssignmentUndo = useCallback(() => {
    scanUndoRef.current?.();
    scanUndoRef.current = null;
  }, []);

  const handleScanNextSlot = useCallback(() => {
    if (!selectedSlot) return false;

    const nextSlot = getNextAssignableSlot(selectedSlot, slotAssignments);
    if (!nextSlot) return false;

    scanUndoRef.current = null;
    setSelectedSlot(nextSlot);
    return true;
  }, [getNextAssignableSlot, selectedSlot, slotAssignments]);

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

  // RackSettingsModal "Continue" handler. For an EXISTING rack, Continue is
  // the settings save: it persists label/zone/dimensions AND placement in a
  // single atomic UpdateDeviceSet, then cascades the rack's CURRENT server
  // members to the new placement — all server-side, in one transaction. This
  // is why the eligibility filter above can trust rackSettings as the rack's
  // live placement: by the time the operator opens Manage Miners, the rack and
  // its members are already there. Membership is untouched here — "Continue
  // saves settings, Save saves miners" — so the modal's draft rackMiners can't
  // leak into a settings-only change.
  //
  // A NEW rack doesn't exist yet, so there's nothing to persist; its settings
  // (including placement) ride on the create in handleSave.
  const handleRackSettingsUpdate = useCallback(
    async (formData: RackFormData) => {
      const rackId = existingRackId;
      if (rackId !== undefined) {
        // Only send placement when the operator actually changed site/building
        // this edit (compared to what the form was seeded with). A metadata-only
        // edit (label/zone/dims) omits placement, so UpdateDeviceSet preserves
        // the rack's CURRENT server placement — a stale cached value can't
        // re-parent a rack that another session moved while this modal was open.
        // Zone stays authoritative even with placement omitted (the settings
        // path treats an empty zone as an explicit clear).
        const placementChanged =
          canManagePlacement &&
          (formData.siteId !== rackSettings.siteId || formData.buildingId !== rackSettings.buildingId);
        let updated: DeviceSet | undefined;
        try {
          await new Promise<void>((resolve, reject) => {
            updateRack({
              deviceSetId: rackId,
              label: formData.label,
              zone: formData.zone,
              rows: formData.rows,
              columns: formData.columns,
              orderIndex: formData.orderIndex,
              coolingType: formData.coolingType,
              // Unset level -> 0n unassign when the operator did change placement.
              siteId: placementChanged ? (formData.siteId ?? 0n) : undefined,
              buildingId: placementChanged ? (formData.buildingId ?? 0n) : undefined,
              onSuccess: (ds) => {
                updated = ds;
                resolve();
              },
              onError: (msg) => reject(new Error(msg)),
            });
          });
        } catch (err) {
          pushToast({
            message: getErrorMessage(err, "Failed to update rack settings. Please try again."),
            status: STATUSES.error,
          });
          // Keep Rack Settings open (don't apply) so the operator can retry.
          return;
        }
        // Adopt the server's AUTHORITATIVE placement from the response, not the
        // submitted formData: when placement was omitted (metadata-only edit)
        // the server kept whatever the rack's current site/building is — which
        // may differ from the stale formData values if another session moved it.
        // The eligibility filter reads rackSettings placement, so trusting the
        // response keeps the miner list scoped to where the rack really is.
        const serverRackInfo = updated?.typeDetails.case === "rackInfo" ? updated.typeDetails.value : undefined;
        const applied: RackFormData = serverRackInfo
          ? {
              ...formData,
              siteId: serverRackInfo.siteId,
              buildingId: serverRackInfo.buildingId,
              zone: serverRackInfo.zone,
            }
          : formData;
        // Settings are now live on the server. Let the parent refetch so its
        // rack list/overview reflects the new label/placement even if the
        // operator dismisses without pressing the final miner Save.
        onSettingsPersisted?.();
        setRackSettings(applied);
        setShowRackSettings(false);
        return;
      }

      setRackSettings(formData);
      setShowRackSettings(false);
    },
    [existingRackId, canManagePlacement, rackSettings, updateRack, onSettingsPersisted],
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

      // Placement rides on CREATE only. An existing rack's Site/Building (and
      // zone/dimensions) are already persisted on the Rack Settings "Continue"
      // — Continue saves settings, Save saves miners — so an edit Save omits
      // placement: it preserves the rack's current server placement and can't
      // clobber a move made by another session while this modal was open.
      const sendPlacement = canManagePlacement && existingRackId === undefined;

      // Existing-rack Save is miners-only, but SaveRack always rewrites
      // rack_info, so re-send the rack's CURRENT server metadata rather than the
      // modal's cached copy — otherwise a concurrent zone/dimension edit from
      // another session would be reverted (and stale dims could mis-validate the
      // new slots). Best-effort: fall back to the cached values on a fetch miss
      // so a transient error can't block the miner save.
      let meta = {
        label: rackSettings.label,
        zone: rackSettings.zone,
        rows: rackSettings.rows,
        columns: rackSettings.columns,
        orderIndex: rackSettings.orderIndex,
        coolingType: rackSettings.coolingType,
      };
      if (existingRackId !== undefined) {
        await new Promise<void>((resolve) => {
          getDeviceSet({
            deviceSetId: existingRackId,
            onSuccess: (ds) => {
              if (ds.typeDetails.case === "rackInfo") {
                const ri = ds.typeDetails.value;
                meta = {
                  label: ds.label,
                  zone: ri.zone,
                  rows: ri.rows,
                  columns: ri.columns,
                  orderIndex: ri.orderIndex,
                  coolingType: ri.coolingType,
                };
              }
              resolve();
            },
            onNotFound: () => resolve(),
            onError: () => resolve(),
          });
        });
      }

      await new Promise<void>((resolve, reject) => {
        saveRack({
          deviceSetId: existingRackId,
          label: meta.label,
          zone: meta.zone,
          rows: meta.rows,
          columns: meta.columns,
          orderIndex: meta.orderIndex,
          coolingType: meta.coolingType,
          deviceIdentifiers: rackMiners,
          slotAssignments: slotAssignmentsList,
          // Create sends its chosen placement (unset level → NULL), gated on
          // site:manage. Edit omits placement (persisted on Continue).
          siteId: sendPlacement ? (rackSettings.siteId ?? 0n) : undefined,
          buildingId: sendPlacement ? (rackSettings.buildingId ?? 0n) : undefined,
          onSuccess: () => resolve(),
          onError: (msg) => reject(new Error(msg)),
        });
      });

      pushToast({
        message: existingRackId ? `Rack "${meta.label}" updated` : `Rack "${meta.label}" created`,
        status: STATUSES.success,
      });
      onSave();
    } catch (err) {
      setErrorMsg(getErrorMessage(err, "Failed to save. Please try again."));
    } finally {
      setIsSaving(false);
    }
  }, [
    existingRackId,
    rackSettings,
    rackMiners,
    totalSlots,
    activeAssignments,
    canManagePlacement,
    getDeviceSet,
    saveRack,
    onSave,
  ]);

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
          scope={scope}
          onDismiss={() => setShowManageMiners(false)}
          onConfirm={handleManageMinersConfirm}
        />
      ) : null}

      {showSearchMiners ? (
        <SearchMinersModal
          show={showSearchMiners}
          eligibility={eligibility}
          targetRackLabel={rackSettings.label}
          scope={scope}
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
          currentRackLabel={rackSettings.label}
          eligibility={eligibility}
          targetSlotLabel={selectedSlot ? getSlotLabel(selectedSlot) : "selected slot"}
          onDismiss={() => {
            setShowScanQr(false);
            setSelectedSlot(null);
            scanUndoRef.current = null;
          }}
          onAssign={handleScanMinerAssign}
          onConfirm={handleScanMinerConfirm}
          onUndoAssignment={handleScanAssignmentUndo}
          onScanNextSlot={handleScanNextSlot}
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
