import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import SearchRacksModal from "../SearchRacksModal";
import BuildingGridPane from "./BuildingGridPane";
import BuildingRacksPane, { type AssignedRackRow } from "./BuildingRacksPane";
import { type BuildingAssignmentMode, cellKey, type GridCellKey, parseCellKey } from "./types";
import { type BuildingFormValues, useBuildings } from "@/protoFleet/api/buildings";
import { type Building, type BuildingRack } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import FullScreenTwoPaneModal from "@/protoFleet/components/FullScreenTwoPaneModal";
import { DismissCircle } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { pushToast, STATUSES } from "@/shared/features/toaster";

interface ManageBuildingModalProps {
  open: boolean;
  building: Building;
  siteName?: string;
  onDismiss: () => void;
  // Opens BuildingDetailsModal stacked on top of this manage modal.
  onEditDetails: () => void;
  // Fires after a successful save so the host page can refresh its
  // building cache (rack counts, layout fields change).
  onSaved?: (updated: Building) => void;
}

interface AssignmentEntry {
  rackId: bigint;
  label: string;
  aisleIndex?: number;
  positionInAisle?: number;
}

const parseNonNegativeInt = (text: string): number | null => {
  const t = text.trim();
  if (t === "") return 0;
  const n = Number(t);
  if (!Number.isFinite(n) || n < 0 || !Number.isInteger(n)) return null;
  return n;
};

// Compute the auto (byName) assignment map. Sort assigned racks by label and
// fill grid cells row-major (aisle 0 first, then aisle 1, ...) up to capacity.
const buildByNameAssignments = (
  entries: AssignmentEntry[],
  aisles: number,
  racksPerAisle: number,
): Record<GridCellKey, bigint> => {
  if (aisles <= 0 || racksPerAisle <= 0) return {};
  const sorted = [...entries].sort((a, b) => a.label.localeCompare(b.label));
  const out: Record<GridCellKey, bigint> = {};
  let idx = 0;
  outer: for (let aisle = 0; aisle < aisles; aisle++) {
    for (let position = 0; position < racksPerAisle; position++) {
      if (idx >= sorted.length) break outer;
      out[cellKey(aisle, position)] = sorted[idx].rackId;
      idx++;
    }
  }
  return out;
};

// Map manual entries → cellKey → rackId. Entries with no position are
// excluded so the grid renders them as floating (visible in the list, no
// cell highlighted). Out-of-bounds positions are dropped — a shrunken
// layout silently drops cells that no longer exist, matching the
// AssignMinersModal pattern of "membership outlives placement".
const buildManualAssignments = (
  entries: AssignmentEntry[],
  aisles: number,
  racksPerAisle: number,
): Record<GridCellKey, bigint> => {
  const out: Record<GridCellKey, bigint> = {};
  for (const e of entries) {
    if (e.aisleIndex === undefined || e.positionInAisle === undefined) continue;
    if (e.aisleIndex < 0 || e.aisleIndex >= aisles) continue;
    if (e.positionInAisle < 0 || e.positionInAisle >= racksPerAisle) continue;
    out[cellKey(e.aisleIndex, e.positionInAisle)] = e.rackId;
  }
  return out;
};

const ManageBuildingModal = ({
  open,
  building,
  siteName,
  onDismiss,
  onEditDetails,
  onSaved,
}: ManageBuildingModalProps) => {
  const { listBuildingRacks, updateBuilding, assignRackToBuilding } = useBuildings();

  // Layout state — drives both the grid dimensions and the UpdateBuilding
  // write on Save. We carry the numeric text in state (not the parsed
  // number) so trailing-empty inputs read naturally.
  const [aislesText, setAislesText] = useState(building.aisles > 0 ? String(building.aisles) : "");
  const [racksPerAisleText, setRacksPerAisleText] = useState(
    building.racksPerAisle > 0 ? String(building.racksPerAisle) : "",
  );
  const [aislesError, setAislesError] = useState<string | null>(null);
  const [racksPerAisleError, setRacksPerAisleError] = useState<string | null>(null);

  const [entries, setEntries] = useState<AssignmentEntry[]>([]);
  const [assignmentMode, setAssignmentMode] = useState<BuildingAssignmentMode>("byName");
  const [selectedRackId, setSelectedRackId] = useState<bigint | null>(null);
  const [selectedCellKey, setSelectedCellKey] = useState<GridCellKey | null>(null);
  const [showSearchRacks, setShowSearchRacks] = useState(false);

  const [isLoading, setIsLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [isSaving, setIsSaving] = useState(false);
  const [errorMsg, setErrorMsg] = useState("");

  // Snapshot of the server's positions at load time so Save only fires
  // assignRackToBuilding for racks whose position actually changed. Keyed
  // by rackId → "aisle:position" (or "unplaced") so we can string-compare.
  const initialPlacementRef = useRef<Map<string, string>>(new Map());

  // (Re)load assignments when the modal opens. Conditional render in the
  // host ensures each open creates a fresh mount, so initial isLoading=true
  // / loadError=null carry the "reset on open" intent without a synchronous
  // setState here (which the react-hooks/set-state-in-effect rule rejects).
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    const controller = new AbortController();
    void listBuildingRacks({
      buildingId: building.id,
      signal: controller.signal,
      onSuccess: (racks: BuildingRack[]) => {
        if (cancelled) return;
        const parsed: AssignmentEntry[] = racks.map((r) => ({
          rackId: r.rackId,
          label: r.rackLabel,
          aisleIndex: r.aisleIndex,
          positionInAisle: r.positionInAisle,
        }));
        setEntries(parsed);
        const snapshot = new Map<string, string>();
        for (const e of parsed) {
          snapshot.set(
            e.rackId.toString(),
            e.aisleIndex !== undefined && e.positionInAisle !== undefined
              ? `${e.aisleIndex}:${e.positionInAisle}`
              : "unplaced",
          );
        }
        initialPlacementRef.current = snapshot;
        setIsLoading(false);
      },
      onError: (msg) => {
        if (cancelled) return;
        setLoadError(msg);
        setEntries([]);
        setIsLoading(false);
      },
    });
    return () => {
      cancelled = true;
      controller.abort();
    };
  }, [open, building.id, listBuildingRacks]);

  const aislesNum = useMemo(() => {
    const n = parseNonNegativeInt(aislesText);
    return n ?? 0;
  }, [aislesText]);
  const racksPerAisleNum = useMemo(() => {
    const n = parseNonNegativeInt(racksPerAisleText);
    return n ?? 0;
  }, [racksPerAisleText]);

  const activeAssignments: Record<GridCellKey, bigint> = useMemo(() => {
    if (assignmentMode === "byName") {
      return buildByNameAssignments(entries, aislesNum, racksPerAisleNum);
    }
    return buildManualAssignments(entries, aislesNum, racksPerAisleNum);
  }, [assignmentMode, entries, aislesNum, racksPerAisleNum]);

  // Lookup tables keyed by rack id → AssignmentEntry; lets us turn an
  // activeAssignments cellKey → rack label without scanning the entries
  // array every render.
  const entriesById = useMemo(() => {
    const m = new Map<string, AssignmentEntry>();
    for (const e of entries) m.set(e.rackId.toString(), e);
    return m;
  }, [entries]);

  const cellLabels: Record<GridCellKey, string> = useMemo(() => {
    const out: Record<GridCellKey, string> = {};
    for (const [key, rackId] of Object.entries(activeAssignments)) {
      const entry = entriesById.get(rackId.toString());
      out[key] = entry?.label ?? "";
    }
    return out;
  }, [activeAssignments, entriesById]);

  // Assigned-racks list shown in the left pane. positionLabel is derived
  // from the activeAssignments so byName mode shows the auto-placement.
  const assignedRacks: AssignedRackRow[] = useMemo(() => {
    // Reverse-lookup rackId → cellKey for the position label.
    const rackToCell = new Map<string, GridCellKey>();
    for (const [key, rackId] of Object.entries(activeAssignments)) {
      rackToCell.set(rackId.toString(), key);
    }
    return [...entries]
      .sort((a, b) => a.label.localeCompare(b.label))
      .map((e) => {
        const placedKey = rackToCell.get(e.rackId.toString());
        const positionLabel = placedKey
          ? (() => {
              const { aisle, position } = parseCellKey(placedKey);
              return `Aisle ${aisle + 1}, position ${position + 1}`;
            })()
          : undefined;
        return { rackId: e.rackId, label: e.label, positionLabel };
      });
  }, [entries, activeAssignments]);

  const handleAislesChange = useCallback((v: string) => {
    setAislesText(v);
    if (parseNonNegativeInt(v) === null) {
      setAislesError("Enter a whole number ≥ 0");
    } else {
      setAislesError(null);
    }
  }, []);

  const handleRacksPerAisleChange = useCallback((v: string) => {
    setRacksPerAisleText(v);
    if (parseNonNegativeInt(v) === null) {
      setRacksPerAisleError("Enter a whole number ≥ 0");
    } else {
      setRacksPerAisleError(null);
    }
  }, []);

  const handleModeChange = useCallback((mode: BuildingAssignmentMode) => {
    setAssignmentMode(mode);
    setSelectedRackId(null);
    setSelectedCellKey(null);
  }, []);

  const handleSelectRack = useCallback(
    (rackId: bigint | null) => {
      // Rack-first flow: if a cell was selected, place this rack there.
      if (rackId !== null && selectedCellKey !== null) {
        const { aisle, position } = parseCellKey(selectedCellKey);
        setEntries((prev) =>
          prev.map((e) => {
            if (e.rackId === rackId) return { ...e, aisleIndex: aisle, positionInAisle: position };
            // Clear another rack that might have been at the same cell.
            if (e.aisleIndex === aisle && e.positionInAisle === position) {
              return { ...e, aisleIndex: undefined, positionInAisle: undefined };
            }
            return e;
          }),
        );
        setSelectedRackId(null);
        setSelectedCellKey(null);
        return;
      }
      setSelectedRackId(rackId);
    },
    [selectedCellKey],
  );

  const handleCellClick = useCallback(
    (aisle: number, position: number, key: GridCellKey) => {
      if (assignmentMode !== "manual") return;
      // Cell-first flow with a selected rack: place it here immediately.
      if (selectedRackId !== null) {
        setEntries((prev) =>
          prev.map((e) => {
            if (e.rackId === selectedRackId) return { ...e, aisleIndex: aisle, positionInAisle: position };
            if (e.aisleIndex === aisle && e.positionInAisle === position) {
              return { ...e, aisleIndex: undefined, positionInAisle: undefined };
            }
            return e;
          }),
        );
        setSelectedRackId(null);
        setSelectedCellKey(null);
        return;
      }
      // Otherwise just toggle the cell selection so a follow-up rack click
      // places into it.
      setSelectedCellKey((prev) => (prev === key ? null : key));
    },
    [assignmentMode, selectedRackId],
  );

  const handleRemoveRack = useCallback((rackId: bigint) => {
    setEntries((prev) => prev.filter((e) => e.rackId !== rackId));
    setSelectedRackId((prev) => (prev === rackId ? null : prev));
  }, []);

  const handleAddRack = useCallback((rackId: bigint, label: string) => {
    setEntries((prev) => {
      if (prev.some((e) => e.rackId === rackId)) return prev;
      // New racks start unplaced — byName mode will auto-place on next
      // render; manual mode lets the operator drop them into a cell.
      return [...prev, { rackId, label }];
    });
  }, []);

  const handleSearchConfirm = useCallback(
    (rackId: bigint, label: string) => {
      handleAddRack(rackId, label);
      setShowSearchRacks(false);
    },
    [handleAddRack],
  );

  const alreadyAddedRackIds = useMemo(() => entries.map((e) => e.rackId), [entries]);

  // Save:
  //   1. UpdateBuilding if aisles / racks_per_aisle changed.
  //   2. For each rack whose placement changed vs the load-time snapshot,
  //      call AssignRackToBuilding. Newly added racks (not in the snapshot)
  //      also go through this call so the server learns about them.
  //   3. Single refetch implicit via parent (onSaved).
  const handleSave = useCallback(async () => {
    setErrorMsg("");
    if (aislesError || racksPerAisleError) {
      setErrorMsg("Fix the highlighted layout fields before saving.");
      return;
    }
    if (parseNonNegativeInt(aislesText) === null || parseNonNegativeInt(racksPerAisleText) === null) {
      setErrorMsg("Layout fields must be whole numbers.");
      return;
    }
    setIsSaving(true);
    try {
      // Step 1: persist layout if changed.
      const layoutChanged = aislesNum !== building.aisles || racksPerAisleNum !== building.racksPerAisle;
      let updated = building;
      if (layoutChanged) {
        const values: BuildingFormValues = {
          name: building.name,
          description: building.description,
          powerCapacityMw: building.powerKw > 0 ? building.powerKw / 1000 : 0,
          overheadKw: building.overheadKw,
          aisles: aislesNum,
          racksPerAisle: racksPerAisleNum,
        };
        const next = await new Promise<Building | null>((resolve) => {
          void updateBuilding({
            id: building.id,
            values,
            onSuccess: (b) => resolve(b),
            onError: (msg) => {
              setErrorMsg(`Failed to save layout: ${msg}`);
              resolve(null);
            },
          });
        });
        if (!next) {
          setIsSaving(false);
          return;
        }
        updated = next;
      }

      // Step 2: walk activeAssignments and diff against the initial
      // placement snapshot. The active map already reflects byName's
      // implicit positions, so byName saves persist auto-placement just
      // like manual does — the operator's final view becomes the stored
      // state regardless of which mode they were in.
      const rackToCell = new Map<string, GridCellKey>();
      for (const [key, rackId] of Object.entries(activeAssignments)) {
        rackToCell.set(rackId.toString(), key);
      }
      const initial = initialPlacementRef.current;
      const writes: Promise<void>[] = [];
      for (const entry of entries) {
        const idStr = entry.rackId.toString();
        const placedKey = rackToCell.get(idStr);
        const next = placedKey
          ? (() => {
              const { aisle, position } = parseCellKey(placedKey);
              return `${aisle}:${position}`;
            })()
          : "unplaced";
        const prior = initial.get(idStr) ?? "missing";
        if (prior === next) continue;
        const aisle = placedKey ? parseCellKey(placedKey).aisle : undefined;
        const position = placedKey ? parseCellKey(placedKey).position : undefined;
        writes.push(
          new Promise<void>((resolve, reject) => {
            void assignRackToBuilding({
              rackId: entry.rackId,
              buildingId: building.id,
              aisleIndex: aisle,
              positionInAisle: position,
              onSuccess: () => resolve(),
              onError: (msg) => reject(new Error(msg)),
            });
          }),
        );
      }
      // Step 2b: racks removed from this building (in the snapshot but not
      // in the current entries list) need an explicit unassign so the BE
      // drops the building_id.
      const currentIds = new Set(entries.map((e) => e.rackId.toString()));
      for (const idStr of initial.keys()) {
        if (currentIds.has(idStr)) continue;
        writes.push(
          new Promise<void>((resolve, reject) => {
            void assignRackToBuilding({
              rackId: BigInt(idStr),
              buildingId: undefined,
              onSuccess: () => resolve(),
              onError: (msg) => reject(new Error(msg)),
            });
          }),
        );
      }

      try {
        await Promise.all(writes);
      } catch (err) {
        setErrorMsg(err instanceof Error ? err.message : "Failed to save rack positions.");
        setIsSaving(false);
        return;
      }

      pushToast({ message: `Building "${updated.name}" saved`, status: STATUSES.success });
      // Refresh the snapshot so a follow-up Save inside the same modal
      // session doesn't re-fire the same writes.
      const refreshed = new Map<string, string>();
      for (const [key, rackId] of Object.entries(activeAssignments)) {
        refreshed.set(rackId.toString(), key.replace("-", ":"));
      }
      // Entries with no cell are unplaced in the refreshed snapshot.
      for (const e of entries) {
        if (!refreshed.has(e.rackId.toString())) refreshed.set(e.rackId.toString(), "unplaced");
      }
      initialPlacementRef.current = refreshed;
      onSaved?.(updated);
    } finally {
      setIsSaving(false);
    }
  }, [
    aislesError,
    racksPerAisleError,
    aislesText,
    racksPerAisleText,
    aislesNum,
    racksPerAisleNum,
    building,
    activeAssignments,
    entries,
    updateBuilding,
    assignRackToBuilding,
    onSaved,
  ]);

  if (!open) return null;

  // siteId fallback to 0n is safe — the SearchRacksModal effect bails on
  // unset, and the modal only mounts when open.
  const siteId = building.siteId ?? 0n;
  const totalCells = aislesNum * racksPerAisleNum;
  const assignedCount = Object.keys(activeAssignments).length;
  const title = building.name || "Manage building";
  const subtitle = siteName ? `in ${siteName}` : undefined;

  return (
    <>
      <FullScreenTwoPaneModal
        open={open}
        title={subtitle ? `${title} — ${subtitle}` : title}
        onDismiss={onDismiss}
        isBusy={isSaving}
        buttons={[
          {
            text: "Edit building",
            variant: variants.secondary,
            onClick: onEditDetails,
            disabled: isSaving,
            testId: "manage-building-edit-details",
          },
          {
            text: isSaving ? "Saving…" : "Save",
            variant: variants.primary,
            onClick: handleSave,
            disabled: isSaving || isLoading || !!loadError,
            loading: isSaving,
            testId: "manage-building-save",
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
          ) : loadError ? (
            <div className="shrink-0 px-2 pb-4">
              <Callout intent="danger" prefixIcon={<DismissCircle />} title={`Couldn't load racks: ${loadError}`} />
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
          <BuildingRacksPane
            aislesText={aislesText}
            racksPerAisleText={racksPerAisleText}
            aislesError={aislesError}
            racksPerAisleError={racksPerAisleError}
            onAislesChange={handleAislesChange}
            onRacksPerAisleChange={handleRacksPerAisleChange}
            assignmentMode={assignmentMode}
            onModeChange={handleModeChange}
            assignedRacks={assignedRacks}
            selectedRackId={selectedRackId}
            onSelectRack={handleSelectRack}
            onRemoveRack={handleRemoveRack}
            onOpenSearchRacks={() => setShowSearchRacks(true)}
            saving={isSaving}
          />
        }
        secondaryPane={
          <BuildingGridPane
            aisles={aislesNum}
            racksPerAisle={racksPerAisleNum}
            cellLabels={cellLabels}
            onCellClick={assignmentMode === "manual" ? handleCellClick : undefined}
            selectedCellKey={selectedCellKey}
            assignedCount={assignedCount}
            totalCells={totalCells}
          />
        }
      />

      {showSearchRacks ? (
        <SearchRacksModal
          open={showSearchRacks}
          siteId={siteId}
          currentBuildingId={building.id}
          alreadyAddedRackIds={alreadyAddedRackIds}
          onDismiss={() => setShowSearchRacks(false)}
          onConfirm={handleSearchConfirm}
        />
      ) : null}
    </>
  );
};

export default ManageBuildingModal;
