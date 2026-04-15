import { useCallback, useEffect, useMemo, useState } from "react";

import ManageMinersModal from "./ManageMinersModal";
import MinersPane from "./MinersPane";
import RackPane from "./RackPane";
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
import RackSettingsModal from "@/protoFleet/features/rackManagement/components/RackSettingsModal";
import { slotNumberToRowCol } from "@/protoFleet/features/rackManagement/utils/slotNumbering";

import { DismissCircle } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Dialog from "@/shared/components/Dialog";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { pushToast, STATUSES } from "@/shared/features/toaster";

/** Fetch all miner IDs eligible for a rack by paginating through the fleet API.
 *  Applies the same filter the user had active in MinerSelectionList so "select all"
 *  respects model/type filters. Miners in other racks are excluded. */
async function fetchAllSelectableMinerIds(rackLabel: string, listFilter?: MinerListFilter): Promise<string[]> {
  const filter = listFilter
    ? { ...listFilter, pairingStatuses: [PairingStatus.PAIRED] }
    : { pairingStatuses: [PairingStatus.PAIRED] };
  const snapshots = await fetchAllMinerSnapshots(filter);
  return Object.values(snapshots)
    .filter((m) => !m.rackLabel || m.rackLabel === rackLabel)
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

interface AssignMinersModalProps {
  show: boolean;
  rackSettings: RackFormData;
  existingRackId?: bigint;
  existingRacks: DeviceSet[];
  onDismiss: () => void;
  onSave: () => void;
  onDelete?: () => Promise<void> | void;
}

export default function AssignMinersModal({
  show,
  rackSettings: initialRackSettings,
  existingRackId,
  existingRacks,
  onDismiss,
  onSave,
  onDelete,
}: AssignMinersModalProps) {
  const { saveRack, getRackSlots, listGroupMembers } = useDeviceSets();

  // Fetch all miners for display data (name, IP, model, etc.)
  const { miners: minersMap } = useFleet({ pageSize: 1000 });
  const allMiners = useMemo(() => minersMap as Record<string, MinerStateSnapshot>, [minersMap]);

  // Rack settings (can be updated via RackSettingsModal)
  const [rackSettings, setRackSettings] = useState<RackFormData>(initialRackSettings);
  const totalSlots = rackSettings.rows * rackSettings.columns;
  const numberingOrigin = orderIndexToOrigin(rackSettings.orderIndex);

  // Core assignment state
  const [rackMiners, setRackMiners] = useState<string[]>([]);
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
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);

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

  // Popover dismiss — deselect cell
  const handlePopoverDismiss = useCallback(() => {
    setSelectedSlot(null);
    setShowSlotPopover(false);
  }, []);

  // SearchMinersModal confirm — add miner to rack and assign to selected slot
  const handleSearchMinerConfirm = useCallback(
    (minerId: string) => {
      if (!selectedSlot) return;

      // Add miner to rack if not already present
      setRackMiners((prev) => (prev.includes(minerId) ? prev : [...prev, minerId]));

      // Remove any existing assignment for this miner, then assign to selected slot
      setSlotAssignments((prev) => {
        const next = removeAssignmentByValue(prev, minerId);
        next[selectedSlot.key] = minerId;
        return next;
      });

      setSelectedSlot(null);
      setShowSearchMiners(false);
    },
    [selectedSlot],
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

  // ManageMinersModal confirm handler
  const handleManageMinersConfirm = useCallback(
    async (selectedIds: string[], allSelected: boolean, listFilter?: MinerListFilter) => {
      let finalIds = selectedIds;

      if (allSelected) {
        // When "select all" is active, selectedIds only contains the current page.
        // Paginate through all miners server-side to get the complete list, applying
        // the same filters the user had active (e.g. model/type) and excluding miners
        // in other racks. Use initialRackSettings.label because fleet data still
        // carries the original label even if the user edited it locally.
        try {
          setIsLoading(true);
          finalIds = await fetchAllSelectableMinerIds(initialRackSettings.label, listFilter);
        } catch {
          setErrorMsg("Failed to load all miners. Please try again.");
          return;
        } finally {
          setIsLoading(false);
        }
      }

      if (finalIds.length > totalSlots) {
        setErrorMsg(
          `Cannot add ${finalIds.length} miners with only ${totalSlots} available slots. Deselect some miners or update your rack settings.`,
        );
        return;
      }

      setRackMiners(finalIds);
      setShowManageMiners(false);

      // Remove assignments for miners no longer in rack
      const keepSet = new Set(finalIds);
      setSlotAssignments((prev) => filterAssignmentsByValues(prev, keepSet));
      setManualAssignmentCache((prev) => filterAssignmentsByValues(prev, keepSet));
    },
    [initialRackSettings.label, totalSlots],
  );

  // RackSettingsModal edit handler
  const handleRackSettingsUpdate = useCallback((formData: RackFormData) => {
    setRackSettings(formData);
    setShowRackSettings(false);
  }, []);

  // Save handler — single atomic RPC
  const handleSave = useCallback(async () => {
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
  }, [existingRackId, rackSettings, rackMiners, activeAssignments, saveRack, onSave]);

  const hasSubModalOpen = showRackSettings || showManageMiners || showSearchMiners || showDeleteConfirm;

  if (!show) return null;

  return (
    <>
      <FullScreenTwoPaneModal
        open={show}
        title={rackSettings.label}
        onDismiss={hasSubModalOpen ? undefined : onDismiss}
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
            onPopoverDismiss={handlePopoverDismiss}
            onHoverMiner={setHoveredMinerId}
          />
        }
      />

      {showRackSettings && (
        <RackSettingsModal
          show={showRackSettings}
          existingRacks={existingRacks}
          initialFormData={rackSettings}
          onDismiss={() => setShowRackSettings(false)}
          onContinue={handleRackSettingsUpdate}
        />
      )}

      {showManageMiners && (
        <ManageMinersModal
          show={showManageMiners}
          currentRackMiners={rackMiners}
          currentRackLabel={initialRackSettings.label}
          maxSlots={totalSlots}
          onDismiss={() => setShowManageMiners(false)}
          onConfirm={handleManageMinersConfirm}
        />
      )}

      {showSearchMiners && (
        <SearchMinersModal
          show={showSearchMiners}
          currentRackLabel={initialRackSettings.label}
          onDismiss={() => {
            setShowSearchMiners(false);
            setSelectedSlot(null);
          }}
          onConfirm={handleSearchMinerConfirm}
        />
      )}

      {showDeleteConfirm && onDelete && (
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
      )}
    </>
  );
}
