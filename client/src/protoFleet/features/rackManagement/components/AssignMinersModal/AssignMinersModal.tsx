import { useCallback, useEffect, useMemo, useState } from "react";

import ManageMinersModal from "./ManageMinersModal";
import MinersPane from "./MinersPane";
import RackPane from "./RackPane";
import { type AssignmentMode, orderIndexToOrigin, originLabel, type RackFormData } from "./types";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import { type DeviceCollection, type RackSlot } from "@/protoFleet/api/generated/collection/v1/collection_pb";
import {
  type MinerListFilter,
  type MinerStateSnapshot,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useCollections } from "@/protoFleet/api/useCollections";
import useFleet from "@/protoFleet/api/useFleet";
import RackSettingsModal from "@/protoFleet/features/rackManagement/components/RackSettingsModal";
import { slotNumberToRowCol } from "@/protoFleet/features/rackManagement/utils/slotNumbering";

import { DismissCircle } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { pushToast, STATUSES } from "@/shared/features/toaster";

/** Fetch all miner IDs eligible for a rack by paginating through the fleet API.
 *  Applies the same filter the user had active in MinerSelectionList so "select all"
 *  respects model/type filters. Miners in other racks are excluded. */
async function fetchAllSelectableMinerIds(rackLabel: string, listFilter?: MinerListFilter): Promise<string[]> {
  const ids: string[] = [];
  let cursor = "";
  // Merge the list filter with PAIRED pairing status (matching MinerSelectionList's useFleet call)
  const filter = listFilter
    ? { ...listFilter, pairingStatuses: [PairingStatus.PAIRED] }
    : { pairingStatuses: [PairingStatus.PAIRED] };
  do {
    const response = await fleetManagementClient.listMinerStateSnapshots({
      pageSize: 1000,
      cursor,
      filter,
    });
    for (const miner of response.miners) {
      // Include miners not in any rack, or already in this rack
      if (!miner.rackLabel || miner.rackLabel === rackLabel) {
        ids.push(miner.deviceIdentifier);
      }
    }
    cursor = response.cursor;
  } while (cursor);
  return ids;
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
  existingRacks: DeviceCollection[];
  onDismiss: () => void;
  onSave: () => void;
}

export default function AssignMinersModal({
  show,
  rackSettings: initialRackSettings,
  existingRackId,
  existingRacks,
  onDismiss,
  onSave,
}: AssignMinersModalProps) {
  const { saveRack, getRackSlots, listGroupMembers } = useCollections();

  // Fetch all miners for display data (name, IP, model, etc.)
  const { miners: minersMap } = useFleet({ scope: "local", pageSize: 1000 });
  const allMiners = useMemo(() => (minersMap ?? {}) as Record<string, MinerStateSnapshot>, [minersMap]);

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

  // Sub-modal visibility
  const [showRackSettings, setShowRackSettings] = useState(false);
  const [showManageMiners, setShowManageMiners] = useState(false);

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
      collectionId: existingRackId,
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
      collectionId: existingRackId,
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
    },
    [assignmentMode, slotAssignments, manualAssignmentCache],
  );

  // Manual slot click handler
  const handleSlotClick = useCallback(
    (row: number, col: number) => {
      if (assignmentMode !== "manual") return;
      const key = `${row}-${col}`;

      if (!selectedMinerId) return; // No miner selected, nothing to do

      // Slot occupied — do nothing
      if (slotAssignments[key]) return;

      // Assign selected miner to this slot (clear any existing assignment first)
      const newAssignments = removeAssignmentByValue(slotAssignments, selectedMinerId);
      newAssignments[key] = selectedMinerId;
      setSlotAssignments(newAssignments);
      setSelectedMinerId(null);
    },
    [assignmentMode, selectedMinerId, slotAssignments],
  );

  // When clicking an assigned slot with no miner selected, highlight the miner
  const handleAssignedSlotClick = useCallback(
    (deviceIdentifier: string) => {
      if (assignmentMode !== "manual") return;
      setSelectedMinerId(deviceIdentifier);
    },
    [assignmentMode],
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
          collectionId: existingRackId,
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
      setErrorMsg((err as Error)?.message ?? "Failed to save. Please try again.");
    } finally {
      setIsSaving(false);
    }
  }, [existingRackId, rackSettings, rackMiners, activeAssignments, saveRack, onSave]);

  const hasSubModalOpen = showRackSettings || showManageMiners;

  if (!show) return null;

  return (
    <>
      <Modal
        open={show}
        title={rackSettings.label}
        size="fullscreen"
        className="!flex !flex-col !overflow-hidden"
        bodyClassName="!flex-1 !min-h-0"
        divider={false}
        onDismiss={hasSubModalOpen ? undefined : onDismiss}
        buttons={[
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
            dismissModalOnClick: false,
          },
        ]}
      >
        {isLoading ? (
          <div className="flex h-full items-center justify-center">
            <ProgressCircular indeterminate />
          </div>
        ) : (
          <div className="flex h-full min-h-0 flex-col">
            {errorMsg && (
              <div className="shrink-0 px-2 pb-4">
                <Callout
                  intent="danger"
                  prefixIcon={<DismissCircle />}
                  title={errorMsg}
                  dismissible
                  onDismiss={() => setErrorMsg("")}
                />
              </div>
            )}
            <div className="flex min-h-0 flex-1 gap-6 phone:flex-col phone:overflow-y-auto tablet:flex-col tablet:overflow-y-auto">
              <div className="flex w-1/2 flex-col overflow-y-auto phone:order-2 phone:w-full phone:shrink-0 tablet:order-2 tablet:w-full tablet:shrink-0">
                <MinersPane
                  rackMiners={rackMiners}
                  miners={allMiners}
                  slotAssignments={activeAssignments}
                  assignmentMode={assignmentMode}
                  selectedMinerId={selectedMinerId}
                  rows={rackSettings.rows}
                  cols={rackSettings.columns}
                  numberingOrigin={numberingOrigin}
                  onModeChange={handleModeChange}
                  onSelectMiner={setSelectedMinerId}
                  onRemoveMiner={handleRemoveMiner}
                  onUnassignMiner={handleUnassignMiner}
                  onClearAssignments={handleClearAssignments}
                  onOpenManageMiners={() => setShowManageMiners(true)}
                />
              </div>
              <div className="flex w-1/2 flex-col overflow-y-auto rounded-xl bg-surface-5 p-4 phone:order-1 phone:max-h-[70vh] phone:w-full phone:shrink-0 tablet:order-1 tablet:max-h-[70vh] tablet:w-full tablet:shrink-0">
                <RackPane
                  rows={rackSettings.rows}
                  cols={rackSettings.columns}
                  numberingOrigin={numberingOrigin}
                  slotAssignments={activeAssignments}
                  selectedMinerId={selectedMinerId}
                  assignmentMode={assignmentMode}
                  assignedCount={assignedCount}
                  totalSlots={totalSlots}
                  originLabel={originLabel(numberingOrigin)}
                  onSlotClick={handleSlotClick}
                  onAssignedSlotClick={handleAssignedSlotClick}
                />
              </div>
            </div>
          </div>
        )}
      </Modal>

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
    </>
  );
}
