import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";

import { fetchAllSelectableMinerIds } from "./fetchAllSelectableMinerIds";
import ManageMinersModal from "./ManageMinersModal";
import MinersPane from "./MinersPane";
import RackPane from "./RackPane";
import ReparentWarningDialog from "./ReparentWarningDialog";
import ScanMinerQrModal, { type ScanAssignmentOutcome } from "./ScanMinerQrModal";
import SearchMinersModal from "./SearchMinersModal";
import { type AssignmentMode, orderIndexToOrigin, originLabel, type RackFormData, type SelectedSlot } from "./types";
import { useRackMinerScope } from "./useRackMinerScope";
import {
  type DeviceSet,
  type RackSlot,
  RackSlotPositionSchema,
  RackSlotSchema,
} from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import {
  type MinerListFilter,
  type MinerStateSnapshot,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import useFleet from "@/protoFleet/api/useFleet";
import FullScreenTwoPaneModal from "@/protoFleet/components/FullScreenTwoPaneModal";
import type { MinerEligibility } from "@/protoFleet/components/MinerSelectionList";
import RackSettingsModal from "@/protoFleet/features/fleetManagement/components/RackSettingsModal";
import { slotNumberToRowCol } from "@/protoFleet/features/fleetManagement/utils/slotNumbering";
import { useHasPermission } from "@/protoFleet/store";

import { DismissCircle } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Dialog from "@/shared/components/Dialog";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { pushToast, STATUSES } from "@/shared/features/toaster";

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

/** Drop every entry whose value is in `dropSet`, returning a shallow copy. */
function dropAssignmentsByValues(record: Record<string, string>, dropSet: Set<string>): Record<string, string> {
  const next: Record<string, string> = {};
  for (const [k, v] of Object.entries(record)) {
    if (!dropSet.has(v)) next[k] = v;
  }
  return next;
}

// What a membership write reports back. Not-ok with no `error` is the operator
// declining a warning: nothing to tell them, but the caller must not treat it as
// success. An `error` is theirs to render — the parent's callout sits behind the
// pickers, so a picker that stays open has to surface it itself.
type CommitOutcome = { ok: true } | { ok: false; error?: string };

interface ManageRackModalProps {
  show: boolean;
  rackSettings: RackFormData;
  // The rack always exists by the time this modal opens: Rack Settings creates
  // it on its CTA, so there is no staged-create mode to support.
  existingRackId: bigint;
  existingRacks: DeviceSet[];
  // Page-header site scope (single-site only). Forwarded to the embedded
  // RackSettingsModal.
  scopedSiteId?: bigint;
  onDismiss: () => void;
  onSave: () => void;
  // Fired after a write lands that the host's own list/overview reflects — the
  // Rack Settings save, and each membership commit. Parents should refetch in
  // the background so the rack list stays consistent even if the operator
  // dismisses without pressing the final placement Save.
  onSettingsPersisted?: () => void;
  onDelete?: () => Promise<void> | void;
}

export default function ManageRackModal({
  show,
  rackSettings: initialRackSettings,
  existingRackId,
  existingRacks,
  scopedSiteId,
  onDismiss,
  onSave,
  onSettingsPersisted,
  onDelete,
}: ManageRackModalProps) {
  const { assignDevicesToRack, updateRack, getRackSlots, listGroupMembers } = useDeviceSets();
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
  // Site/Building change in Rack Settings is persisted immediately on Save
  // (handleRackSettingsUpdate), which also cascades the rack's members to the
  // new placement. So by the time this filter runs, the rack and its members
  // are already at the placement in rackSettings — no current-vs-pending split,
  // and a miner already at the new destination reads as assignable.
  const eligibility = useMemo<MinerEligibility>(
    () => ({
      rackId: existingRackId,
      siteId: rackSettings.siteId,
      buildingId: rackSettings.buildingId,
    }),
    [existingRackId, rackSettings.siteId, rackSettings.buildingId],
  );

  // Core assignment state, loaded from the rack's real membership below. A
  // bulk "Add to rack → New rack" flow seeds its miners onto the create call
  // itself, so they arrive here as persisted members like any others.
  const [rackMiners, setRackMiners] = useState<string[]>([]);
  // What commitMembership diffs against, rather than the `rackMiners` closure.
  // The scan undo runs from a callback captured before its own add landed, and
  // against that stale snapshot the delta comes out empty — so the undo wrote
  // nothing and left the scanned miner in the rack. Updated synchronously, so
  // every commit sees the membership the previous one produced.
  const rackMinersRef = useRef<string[]>([]);
  const applyRackMiners = useCallback((next: string[] | ((prev: string[]) => string[])) => {
    const value = typeof next === "function" ? next(rackMinersRef.current) : next;
    rackMinersRef.current = value;
    setRackMiners(value);
  }, []);
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
  // Resolves false when the undo could not be persisted, so the scanner can
  // stop instead of rescanning over a miner that is still in the rack.
  const scanUndoRef = useRef<(() => Promise<boolean>) | null>(null);

  // Pending reparent confirmation, from either of two sources: the picker
  // reporting miners currently placed elsewhere (#672), and the server refusing
  // to strip a miner's site for a site-less rack. `onConfirm` runs the deferred
  // action; `onCancel` lets the server-conflict path report the refusal back to
  // the write that is awaiting an answer.
  const [reparentConfirm, setReparentConfirm] = useState<{
    count: number;
    onConfirm: () => void;
    onCancel?: () => void;
  } | null>(null);

  // Placement as loaded from the server, keyed by miner: a "row-col" cell or
  // "unplaced". The Save delta is this compared against the working set, so a
  // miner whose cell never moved is never sent. Membership commits keep it in
  // step, entering newcomers as "unplaced" so joining a rack doesn't read as
  // pending placement dirt on the Save gate.
  const [initialPlacement, setInitialPlacement] = useState<Map<string, string>>(new Map());

  // Loading / error state
  const [isLoading, setIsLoading] = useState(true);
  const [loadFailed, setLoadFailed] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  // `isSaving` lags a render behind, so the ref is what actually keeps two
  // writes — a Save and a picker's membership commit — from overlapping.
  const savingRef = useRef(false);
  // How many miners the server refused to strip, handed from the failed attempt
  // to the confirmation prompt. A ref, not state: nothing renders from it — the
  // dialog reads the count off reparentConfirm.
  const pendingConflictCountRef = useRef(0);
  const [errorMsg, setErrorMsg] = useState("");

  // Load the rack's members and slots.
  useEffect(() => {
    let cancelled = false;
    let loadedMembers = false;
    let loadedSlots = false;
    let members: string[] = [];
    let slots: RackSlot[] = [];

    const maybeFinish = () => {
      if (!loadedMembers || !loadedSlots || cancelled) return;
      applyRackMiners(members);

      const assignments: Record<string, string> = {};
      for (const slot of slots) {
        if (slot.position) {
          assignments[`${slot.position.row}-${slot.position.column}`] = slot.deviceIdentifier;
        }
      }
      setSlotAssignments(assignments);
      setManualAssignmentCache(assignments);

      const placed = new Map<string, string>();
      for (const [key, deviceId] of Object.entries(assignments)) placed.set(deviceId, key);
      setInitialPlacement(new Map(members.map((id) => [id, placed.get(id) ?? "unplaced"])));
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
  }, [existingRackId, listGroupMembers, getRackSlots, applyRackMiners]);

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

  // The reparent warning (#672), awaited. Two things raise it: the picker
  // reporting miners currently placed elsewhere, and the server refusing to
  // strip a miner's site for a site-less rack. The second interrupts a write
  // that needs the answer, so both take the promise form — which also lets the
  // pickers keep the write's outcome in scope and report it themselves.
  //
  // Callers pass the reassignment count from a reliable source (the selection
  // list's per-row placement, or the scanned miner's snapshot) rather than the
  // parent's first-page-only `allMiners` cache, so the warning isn't missed for
  // miners outside that page. Zero resolves straight through.
  const confirmReparent = useCallback(
    (count: number) =>
      new Promise<boolean>((resolve) => {
        if (count === 0) {
          resolve(true);
          return;
        }
        setReparentConfirm({
          count,
          onConfirm: () => resolve(true),
          onCancel: () => resolve(false),
        });
      }),
    [],
  );

  // One AssignDevicesToRack. Unset targetRackId unassigns; set assigns, and
  // `slots` rides along in the same transaction. Resolves "conflict" when the
  // server refused a site strip (nothing was written).
  const dispatchAssign = useCallback(
    (deviceIdentifiers: string[], targetRackId: bigint | undefined, force: boolean, slots?: RackSlot[]) =>
      new Promise<"ok" | "conflict">((resolve, reject) => {
        void assignDevicesToRack({
          targetRackId,
          deviceIdentifiers,
          slotAssignments: slots,
          forceClearConflictingSite: force,
          onSuccess: () => resolve("ok"),
          onConflicts: (conflicts) => {
            pendingConflictCountRef.current = conflicts.length;
            resolve("conflict");
          },
          onError: (msg) => reject(new Error(msg)),
        });
      }),
    [assignDevicesToRack],
  );

  // Runs a write, and on a site-strip refusal asks the operator and retries with
  // the strip forced. Shared by the membership commits and the placement Save so
  // the confirmation behaves identically wherever the refusal surfaces.
  const dispatchWithSiteStripConfirm = useCallback(
    async (deviceIdentifiers: string[], targetRackId: bigint | undefined, slots?: RackSlot[]): Promise<boolean> => {
      if ((await dispatchAssign(deviceIdentifiers, targetRackId, false, slots)) === "ok") return true;
      if (!(await confirmReparent(pendingConflictCountRef.current))) return false;
      return (await dispatchAssign(deviceIdentifiers, targetRackId, true, slots)) === "ok";
    },
    [dispatchAssign, confirmReparent],
  );

  // Everything that references a miner the rack no longer holds. Called the
  // moment an unassign lands rather than after the whole commit, so a failure
  // later in that commit can't leave a removed miner on screen.
  const forgetMiners = useCallback(
    (ids: string[]) => {
      const gone = new Set(ids);
      applyRackMiners((prev) => prev.filter((id) => !gone.has(id)));
      setSlotAssignments((prev) => dropAssignmentsByValues(prev, gone));
      setManualAssignmentCache((prev) => dropAssignmentsByValues(prev, gone));
      setInitialPlacement((prev) => {
        const next = new Map(prev);
        for (const id of ids) next.delete(id);
        return next;
      });
      setSelectedMinerId((prev) => (prev !== null && gone.has(prev) ? null : prev));
    },
    [applyRackMiners],
  );

  // Membership commit. The miner pickers own rack membership, so a confirmed
  // selection is written straight away and only placement stays staged for Save.
  // Newcomers go in without a slot and land in the load-time snapshot as
  // "unplaced", so freshly-committed membership doesn't read as pending
  // placement dirt on the Save gate.
  //
  // Callers own the newcomer half of the working-set update — each entry point
  // merges rows differently — and should skip it unless `ok`. The removal half
  // is applied here, per phase, because removals and additions are two separate
  // RPCs and the second can fail after the first has already persisted.
  const commitMembership = useCallback(
    async (added: string[], removed: string[]): Promise<CommitOutcome> => {
      if (savingRef.current) return { ok: false, error: "Another change is still saving. Please try again." };
      const currentIds = new Set(rackMinersRef.current);
      const newcomers = added.filter((id) => !currentIds.has(id));
      const leavers = removed.filter((id) => currentIds.has(id));
      // Nothing to write. The caller may still have a placement change to stage
      // (e.g. re-placing a miner that's already a member).
      if (newcomers.length === 0 && leavers.length === 0) return { ok: true };

      // Capacity guard — mirrors the server's. Membership is what fills a rack,
      // so this is the check that has to happen here rather than on Save.
      const nextCount = currentIds.size + newcomers.length - leavers.length;
      if (totalSlots > 0 && nextCount > totalSlots) {
        return {
          ok: false,
          error: `Cannot hold ${nextCount} miners with only ${totalSlots} available slots. Deselect some miners or update your rack settings.`,
        };
      }

      savingRef.current = true;
      setErrorMsg("");
      setIsSaving(true);
      try {
        // Removals first: they free their slots before any newcomer lands, and
        // the reverse order would trip the server's capacity check on a swap. An
        // unassign has no target rack to strip a site for, so its conflict
        // branch never fires — it goes through the same helper for uniformity.
        if (leavers.length > 0) {
          if (!(await dispatchWithSiteStripConfirm(leavers, undefined))) return { ok: false };
          // Persisted. Reflect it locally and on the host's list now, so a
          // failure in the newcomer write below still leaves both truthful.
          forgetMiners(leavers);
          onSettingsPersisted?.();
        }
        if (newcomers.length > 0 && !(await dispatchWithSiteStripConfirm(newcomers, existingRackId))) {
          return { ok: false };
        }
      } catch (err) {
        return { ok: false, error: getErrorMessage(err, "Failed to update the rack's miners. Please try again.") };
      } finally {
        savingRef.current = false;
        setIsSaving(false);
      }

      setInitialPlacement((prev) => {
        const next = new Map(prev);
        for (const id of newcomers) next.set(id, "unplaced");
        return next;
      });
      // Member counts changed on the host's rack list.
      onSettingsPersisted?.();
      return { ok: true };
    },
    [totalSlots, existingRackId, dispatchWithSiteStripConfirm, forgetMiners, onSettingsPersisted],
  );

  // SearchMinersModal confirm — commit the miner into the rack if it isn't a
  // member yet, then stage it at the selected slot. The modal reports the
  // reassignment flag from the row it selected (exact even for fleets larger
  // than the display page).
  const handleSearchMinerConfirm = useCallback(
    async (minerId: string, isReassignment: boolean) => {
      if (!selectedSlot) return;
      const slotKey = selectedSlot.key;
      if (!(await confirmReparent(isReassignment ? 1 : 0))) return;

      const outcome = await commitMembership([minerId], []);
      if (!outcome.ok) {
        // This picker has no callout of its own, and one miner at one slot is
        // nothing to preserve — close it so the parent's error is what the
        // operator sees rather than an unchanged list.
        if (outcome.error) setErrorMsg(outcome.error);
        setShowSearchMiners(false);
        setSelectedSlot(null);
        return;
      }

      applyRackMiners((prev) => (prev.includes(minerId) ? prev : [...prev, minerId]));
      // Remove any existing assignment for this miner, then assign to selected slot
      setSlotAssignments((prev) => {
        const next = removeAssignmentByValue(prev, minerId);
        next[slotKey] = minerId;
        return next;
      });
      setSelectedSlot(null);
      setShowSearchMiners(false);
    },
    [selectedSlot, confirmReparent, commitMembership, applyRackMiners],
  );

  const handleScanMinerAssign = useCallback(
    async (minerId: string): Promise<ScanAssignmentOutcome> => {
      if (!selectedSlot) return { failed: true, message: "Select a rack slot, then scan a miner." };

      const wasMember = rackMinersRef.current.includes(minerId);
      // Membership commits before the modal reports the assignment, so the
      // "assigned" screen never claims a miner that isn't actually in the rack.
      const outcome = await commitMembership([minerId], []);
      if (!outcome.ok) return { failed: true, message: outcome.error };

      const previousSlotAssignments = slotAssignments;
      const assignedSlot = selectedSlot;
      const nextSlotAssignments = removeAssignmentByValue(slotAssignments, minerId);
      nextSlotAssignments[assignedSlot.key] = minerId;

      applyRackMiners((prev) => (prev.includes(minerId) ? prev : [...prev, minerId]));
      setSlotAssignments(nextSlotAssignments);

      // Undo has to reverse the write too, not just the staged cell — but only
      // for a miner this scan actually added. One that was already a member
      // keeps its membership and just loses the new cell. commitMembership
      // diffs against rackMinersRef, so it sees the add this closure predates.
      scanUndoRef.current = async () => {
        if (wasMember) {
          setSlotAssignments(previousSlotAssignments);
          setSelectedSlot(assignedSlot);
          return true;
        }
        const undone = await commitMembership([], [minerId]);
        if (!undone.ok) {
          // Close the scanner so the parent's callout is visible, matching the
          // confirm path: the miner is still in the rack at the slot it was
          // scanned into, and rescanning would hide that behind the scanning
          // screen. selectedSlot stays put so the grid still reads truthfully.
          if (undone.error) setErrorMsg(undone.error);
          setShowScanQr(false);
          return false;
        }
        // forgetMiners already dropped the miner and its cell, so only the cells
        // the scan displaced are left to restore.
        setSlotAssignments(previousSlotAssignments);
        setSelectedSlot(assignedSlot);
        return true;
      };

      return {
        slotLabel: getSlotLabel(assignedSlot),
        hasNextSlot: !!getNextAssignableSlot(assignedSlot, nextSlotAssignments),
      };
    },
    [getNextAssignableSlot, getSlotLabel, selectedSlot, slotAssignments, commitMembership, applyRackMiners],
  );

  // Scanned miners already assigned elsewhere use the same reparent warning as
  // search/list assignment. The success dialog is reserved for immediate
  // assignments that did not require that warning.
  const handleScanMinerConfirm = useCallback(
    async (minerId: string, isReassignment: boolean) => {
      if (!selectedSlot) return;
      const slotKey = selectedSlot.key;
      if (!(await confirmReparent(isReassignment ? 1 : 0))) return;

      const outcome = await commitMembership([minerId], []);
      if (!outcome.ok) {
        // Close the scanner so the parent's callout is visible — the scan phase
        // has already left the screen the message would have gone to.
        if (outcome.error) setErrorMsg(outcome.error);
        setShowScanQr(false);
        setSelectedSlot(null);
        scanUndoRef.current = null;
        return;
      }

      applyRackMiners((prev) => (prev.includes(minerId) ? prev : [...prev, minerId]));
      setSlotAssignments((prev) => {
        const next = removeAssignmentByValue(prev, minerId);
        next[slotKey] = minerId;
        return next;
      });
      setSelectedSlot(null);
      setShowScanQr(false);
      scanUndoRef.current = null;
    },
    [selectedSlot, confirmReparent, commitMembership, applyRackMiners],
  );

  // No undo registered means there is nothing to fail, so the scanner is free
  // to rescan. A registered one that reports false keeps the scanner closed on
  // the error this modal just set.
  const handleScanAssignmentUndo = useCallback(async () => {
    const undone = (await scanUndoRef.current?.()) ?? true;
    scanUndoRef.current = null;
    return undone;
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
        applyRackMiners((prev) => (prev.includes(deviceId) ? prev : [...prev, deviceId]));
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
    [selectedSlot, applyRackMiners],
  );

  // Clear all assignments
  const handleClearAssignments = useCallback(() => {
    setSlotAssignments({});
    setManualAssignmentCache({});
    setSelectedMinerId(null);
  }, []);

  // Row-level "Remove from rack" — an immediate unassign (the miner keeps
  // existing; it just leaves the rack). The commit's forgetMiners drops the row
  // once the write lands, so a failure leaves the list truthful.
  const handleRemoveMiner = useCallback(
    async (deviceId: string) => {
      const outcome = await commitMembership([], [deviceId]);
      if (!outcome.ok && outcome.error) setErrorMsg(outcome.error);
    },
    [commitMembership],
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
  // behind the modal, so select-all overflow/load errors and the membership
  // write's own failure must go back up. That is why the reparent warning is
  // awaited here rather than deferring the write to a callback: the outcome has
  // to still be in scope to return.
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

      // The picker owns membership, so the delta is written here rather than
      // staged. Accepting the reparent warning is what authorizes that write.
      const keepSet = new Set(finalIds);
      const removed = rackMinersRef.current.filter((id) => !keepSet.has(id));
      if (!(await confirmReparent(reassignedCount))) return undefined;

      // Failure leaves the picker open with the selection intact to retry, so
      // the message goes back to it rather than the callout behind it.
      const outcome = await commitMembership(finalIds, removed);
      if (!outcome.ok) return outcome.error;

      // The removed miners' cells went with them in the commit's forgetMiners,
      // so the surviving assignments are already the right ones.
      applyRackMiners(finalIds);
      setShowManageMiners(false);
      return undefined;
    },
    [eligibility, totalSlots, confirmReparent, commitMembership, applyRackMiners],
  );

  // Rack Settings "Save" handler: persists label/zone/dimensions AND placement
  // in a single atomic UpdateDeviceSet, then cascades the rack's CURRENT server
  // members to the new placement — all server-side, in one transaction. This is
  // why the eligibility filter above can trust rackSettings as the rack's live
  // placement: by the time the operator opens Manage Miners, the rack and its
  // members are already there. Membership is untouched here, so the modal's
  // draft rackMiners can't leak into a settings-only change.
  const handleRackSettingsUpdate = useCallback(
    async (formData: RackFormData) => {
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
            deviceSetId: existingRackId,
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
      pushToast({ message: `Rack "${applied.label}" saved`, status: STATUSES.success });
      setRackSettings(applied);
      setShowRackSettings(false);
    },
    [existingRackId, canManagePlacement, rackSettings, updateRack, onSettingsPersisted],
  );

  // One RackSlot per miner whose cell differs from what loaded: position set to
  // the new cell, position omitted to clear it. Miners the operator never moved
  // aren't named at all, so a concurrent placement change elsewhere in the rack
  // survives this save — the whole reason Save no longer goes through SaveRack,
  // which replaced the rack's entire member set from a possibly-stale snapshot.
  const placementDelta = useMemo<RackSlot[]>(() => {
    const cellByMiner = new Map<string, string>();
    for (const [key, deviceId] of Object.entries(activeAssignments)) cellByMiner.set(deviceId, key);

    const delta: RackSlot[] = [];
    for (const deviceId of rackMiners) {
      const before = initialPlacement.get(deviceId) ?? "unplaced";
      const after = cellByMiner.get(deviceId) ?? "unplaced";
      if (before === after) continue;
      if (after === "unplaced") {
        delta.push(create(RackSlotSchema, { deviceIdentifier: deviceId }));
        continue;
      }
      const [row, column] = after.split("-").map(Number);
      delta.push(
        create(RackSlotSchema, {
          deviceIdentifier: deviceId,
          position: create(RackSlotPositionSchema, { row, column }),
        }),
      );
    }
    return delta;
  }, [activeAssignments, rackMiners, initialPlacement]);

  // Save owns slot placement only. Membership commits in the pickers, and
  // label/zone/dimensions/site/building commit in Rack Settings — so by the time
  // the operator gets here the only unwritten thing left is where each miner
  // sits in the grid.
  const handleSave = useCallback(async () => {
    if (savingRef.current) return;
    // Nothing to write. Close rather than write nothing and toast as though
    // something changed — reviewing the grid and keeping it as-is is a
    // legitimate outcome, not an error.
    if (placementDelta.length === 0) {
      onSave();
      return;
    }

    savingRef.current = true;
    setIsSaving(true);
    setErrorMsg("");
    try {
      // Every named miner is already a member; re-asserting membership is a
      // no-op server-side (and is what lets the slots ride the same
      // transaction), so this cannot move a miner between racks.
      const ok = await dispatchWithSiteStripConfirm(
        placementDelta.map((slot) => slot.deviceIdentifier),
        existingRackId,
        placementDelta,
      );
      if (!ok) return;
      pushToast({ message: `Miner positions saved for "${rackSettings.label}"`, status: STATUSES.success });
      onSave();
    } catch (err) {
      setErrorMsg(getErrorMessage(err, "Failed to save. Please try again."));
    } finally {
      savingRef.current = false;
      setIsSaving(false);
    }
  }, [placementDelta, existingRackId, rackSettings.label, dispatchWithSiteStripConfirm, onSave]);

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
            // No dirty gate: the no-op case closes in handleSave, where it can
            // be told apart from a real save. The load guards stay — a delta
            // diffed against a placement that never arrived isn't a write.
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
          existingRack
          defaultSiteId={scopedSiteId}
          onDismiss={() => setShowRackSettings(false)}
          onSubmit={handleRackSettingsUpdate}
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
          saving={isSaving}
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
          onCancel={() => {
            const abandon = reparentConfirm.onCancel;
            setReparentConfirm(null);
            abandon?.();
          }}
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
