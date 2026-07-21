import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import ManageRacksModal from "../ManageRacksModal";
import { type RackSelectionDelta } from "../ManageRacksModal/rackSelectionDelta";
import SearchRacksModal from "../SearchRacksModal";
import { type AssignmentEntry, buildByNameAssignments, buildManualAssignments } from "./assignmentMath";
import BuildingGridPane from "./BuildingGridPane";
import { assignedRackScope, buildingRackScope } from "./buildingRackScope";
import BuildingRacksPane, { type AssignedRackRow } from "./BuildingRacksPane";
import RackReparentWarningDialog, { type ReparentedRack } from "./RackReparentWarningDialog";
import { type BuildingAssignmentMode, type GridCellKey, parseCellKey } from "./types";
import { type RackPlacementInput, useBuildings } from "@/protoFleet/api/buildings";
import { type Building, type BuildingRack } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import FullScreenTwoPaneModal from "@/protoFleet/components/FullScreenTwoPaneModal";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";
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
  // Opens BuildingSettingsModal stacked on top of this manage modal.
  onEditDetails: () => void;
  // Opens BuildingDeleteDialog via the page-level useBuildingModals.
  // Mirrors ManageRackModal's header Delete CTA.
  onDeleteRequested: () => void;
  // Fires after a successful save so the host page can refresh its
  // building cache (rack counts, layout fields change). The rack
  // pickers (ManageRacksModal / SearchRacksModal) fetch their own
  // sibling-building labels via listBuildingsBySite, so no
  // siblingBuildings prop is plumbed through.
  onSaved?: (updated: Building) => void;
  // Count of miners assigned directly to this building (no rack), shown as a
  // count line under the racks list. Set when the building was created from a
  // bulk "New building" action seeded with loose miners.
  unassignedMinerCount?: number;
}

// Proto caps `racks` at 1000 per AssignRacksToBuildingRequest, so the Save
// dispatch chunks to this size.
const RACKS_PER_RPC = 1000;

const ManageBuildingModal = ({
  open,
  building,
  siteName,
  onDismiss,
  onEditDetails,
  onDeleteRequested,
  onSaved,
  unassignedMinerCount,
}: ManageBuildingModalProps) => {
  const { listBuildingRacks, assignRacksToBuilding } = useBuildings();

  // Rack-fetch scope forwarded to both pickers so they list only racks
  // eligible for this building (its site + site-unassigned) instead of the
  // whole org. Derived from the building's own site — not the header
  // SitePicker — so it stays correct on the unscoped /buildings/:id and
  // /sites/:id routes where the persisted header selection may be an
  // unrelated site. Computed once here so the rule lives in one place.
  const rackScope = useMemo(() => buildingRackScope(building.siteId ?? 0n), [building.siteId]);

  // Broadened fetch scope forwarded to both pickers for the "Show assigned
  // racks" toggle. Only the header all-sites case broadens the fetch (to a
  // global list, surfacing cross-site racks); a scoped header is guaranteed by
  // scope-sync (#764) to equal the building's own site, so it reuses rackScope.
  // Reading the persisted active-site directly from the store (not useActiveSite)
  // keeps the header dependency in one place AND avoids coupling the modal to a
  // React Router context — it stays renderable in isolated hosts/tests, matching
  // the Part A goal of route-independent pickers.
  const activeSite = useFleetStore((state) => state.ui.activeSite);
  const assignedScope = useMemo(
    () => assignedRackScope(building.siteId ?? 0n, activeSite.kind === "all"),
    [building.siteId, activeSite.kind],
  );

  // Pending reparent confirm — set when the operator picks an already-placed
  // rack. Accepting it STAGES the move into the working set (parity with the
  // miner-side promptReparent → setRackMiners: a reparent is staged and only
  // persisted on the outer Save); cancelling leaves the picker open and nothing
  // changes. The rack lands in the working set, so the picker seeds it as
  // "in this building" on reopen (buildRackPickerItem) — never re-derived as a
  // reassignment row — and the actual move rides handleSave.
  const [reparentConfirm, setReparentConfirm] = useState<{ racks: ReparentedRack[]; onConfirm: () => void } | null>(
    null,
  );

  // Aisles / racks_per_aisle are read straight from the building prop —
  // BuildingSettingsModal owns those fields now and threads any edits back
  // through onSaved → host refetch → new building prop. This modal only
  // owns rack placement.
  const aislesNum = building.aisles ?? 0;
  const racksPerAisleNum = building.racksPerAisle ?? 0;

  const [entries, setEntries] = useState<AssignmentEntry[]>([]);
  // Manual is the operator's default mental model (and matches
  // ManageRackModal's default) — surface that mode immediately so the
  // assigned-racks list is clickable on first render.
  const [assignmentMode, setAssignmentMode] = useState<BuildingAssignmentMode>("manual");
  const [selectedRackId, setSelectedRackId] = useState<bigint | null>(null);
  const [selectedCellKey, setSelectedCellKey] = useState<GridCellKey | null>(null);
  // Popover only renders while a cell-first click is still awaiting a choice;
  // dismissed by "Select from list" (cell stays selected, popover closes so
  // the operator can click a left-pane row), "Search racks" (opens picker),
  // or click-outside / Escape. Mirrors RackPane.showPopover.
  const [showCellPopover, setShowCellPopover] = useState(false);
  // Hover bridge: grid → list. When the operator hovers an assigned cell,
  // BuildingGridPane sets this to that cell's rackId and the matching row
  // picks up a hover highlight (mirrors ManageRackModal.hoveredMinerId).
  const [hoveredRackId, setHoveredRackId] = useState<bigint | null>(null);
  const [showManageRacks, setShowManageRacks] = useState(false);
  const [showSearchRacks, setShowSearchRacks] = useState(false);

  const [isLoading, setIsLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [isSaving, setIsSaving] = useState(false);
  const [errorMsg, setErrorMsg] = useState("");

  // Snapshot of the server's positions at load time so Save only fires
  // assignRacksToBuilding for racks whose position actually changed. Keyed
  // by rackId → "aisle:position" (or "unplaced") so we can string-compare.
  const initialPlacementRef = useRef<Map<string, string>>(new Map());

  // Synchronous in-flight guard for Save dispatches. setState batching
  // means the `isSaving` prop driving the button's `disabled` lags one
  // render behind the click — a double-click would otherwise reach the
  // dispatch path twice. Mirrors useSiteModals' savingRef pattern.
  const savingRef = useRef(false);

  // (Re)load assignments when the modal opens.
  useEffect(() => {
    if (!open) return;
    // State reset between buildings is handled by the host
    // (BuildingModals) keying ManageBuildingModal on building.id so
    // React unmounts/remounts on switch — avoids the setState-in-
    // effect anti-pattern. Within a single mount, building.id is
    // stable so this effect runs once.
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

  const activeAssignments: Record<GridCellKey, bigint> = useMemo(() => {
    if (assignmentMode === "byName") {
      return buildByNameAssignments(entries, aislesNum, racksPerAisleNum);
    }
    return buildManualAssignments(entries, aislesNum, racksPerAisleNum);
  }, [assignmentMode, entries, aislesNum, racksPerAisleNum]);

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

  // Reverse-lookup rackId → cellKey shared by the left-pane position
  // labels and the assigned-count summary. Recomputed when assignments
  // change so byName mode reflects auto-placement.
  const rackToCell = useMemo(() => {
    const m = new Map<string, GridCellKey>();
    for (const [key, rackId] of Object.entries(activeAssignments)) {
      m.set(rackId.toString(), key);
    }
    return m;
  }, [activeAssignments]);

  // Assigned-racks list shown in the left pane. positionLabel is derived
  // from the activeAssignments so byName mode shows the auto-placement.
  const assignedRacks: AssignedRackRow[] = useMemo(
    () =>
      [...entries]
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
        }),
    [entries, rackToCell],
  );

  const handleModeChange = useCallback((mode: BuildingAssignmentMode) => {
    setAssignmentMode(mode);
    setSelectedRackId(null);
    setSelectedCellKey(null);
    setShowCellPopover(false);
  }, []);

  // Rack-row select handler. Two-step manual flow: if a cell is already
  // selected, drop the rack into it and clear both selections; otherwise
  // toggle the rack's selected state (so the operator can then click a
  // cell to place it).
  const handleSelectRack = useCallback(
    (rackId: bigint | null) => {
      if (rackId !== null && selectedCellKey !== null) {
        const { aisle, position } = parseCellKey(selectedCellKey);
        setEntries((prev) =>
          prev.map((e) => {
            if (e.rackId === rackId) return { ...e, aisleIndex: aisle, positionInAisle: position };
            // Cell holds at most one rack; clear any prior occupant.
            if (e.aisleIndex === aisle && e.positionInAisle === position) {
              return { ...e, aisleIndex: undefined, positionInAisle: undefined };
            }
            return e;
          }),
        );
        setSelectedRackId(null);
        setSelectedCellKey(null);
        setShowCellPopover(false);
        return;
      }
      setSelectedRackId(rackId);
    },
    [selectedCellKey],
  );

  // Cell click handler. Two flows:
  //  1) rack-first — a rack row is selected → drop it into this cell and
  //     clear both selections (no popover);
  //  2) cell-first — no rack selected → mark this cell selected and open
  //     the popover so the operator can pick "Select from list" or
  //     "Search racks". Mirrors ManageRackModal.handleCellClick.
  // Only fires in manual mode (BuildingGridPane gates on this).
  const handleCellClick = useCallback(
    (aisle: number, position: number, key: GridCellKey) => {
      if (assignmentMode !== "manual") return;
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
        setShowCellPopover(false);
        return;
      }
      setSelectedCellKey(key);
      setShowCellPopover(true);
    },
    [assignmentMode, selectedRackId],
  );

  // Popover dismissal — clicking the overlay or pressing Escape collapses
  // the cell-first flow entirely (deselect cell + hide popover).
  const handlePopoverDismiss = useCallback(() => {
    setSelectedCellKey(null);
    setShowCellPopover(false);
  }, []);

  // Popover "Select from list" — keep cell selected, hide popover, wait
  // for a rack-row click. The list pane's selectedCellHint stays visible
  // so the operator knows what they're filling.
  const handleSelectFromList = useCallback(() => {
    setShowCellPopover(false);
  }, []);

  // Popover "Search racks" — hand off to SearchRacksModal; the picked rack
  // gets added (if new) and assigned to the still-selected cell.
  const handleSearchRacks = useCallback(() => {
    setShowCellPopover(false);
    setShowSearchRacks(true);
  }, []);

  // Gate a reparent behind the warning dialog, then STAGE it on confirm — the
  // rack joins the working set but nothing is written until the outer Save.
  // Parity with the miner side (promptReparent → setRackMiners): a reparent is
  // staged, and the actual move rides handleSave's member-only
  // AssignRacksToBuilding (targetBuildingId → the rack moves out of its old
  // building, cascading its miners) exactly like any newly-added rack. A staged
  // rack is seeded into the picker, so buildRackPickerItem shows it as
  // "in this building" on reopen — never a reassignment row that a later
  // toggle-off / Select all / deselect could silently drop. Cancelling leaves
  // the working set untouched and the picker open.
  const promptReparent = useCallback((racks: ReparentedRack[], apply: () => void) => {
    setReparentConfirm({
      racks,
      onConfirm: () => {
        setReparentConfirm(null);
        apply();
      },
    });
  }, []);

  // SearchRacksModal confirm — add the rack to the working set if missing
  // and assign to the cell that was selected when the popover opened. When the
  // rack is currently placed elsewhere, commit the move behind the reparent
  // confirm (its miners move with it) before staging the cell.
  const handleSearchRackConfirm = useCallback(
    (rackId: bigint, label: string, reparent?: ReparentedRack) => {
      const targetKey = selectedCellKey;
      if (targetKey === null) {
        setShowSearchRacks(false);
        return;
      }
      const apply = () => {
        const { aisle, position } = parseCellKey(targetKey);
        setEntries((prev) => {
          const idStr = rackId.toString();
          const exists = prev.some((e) => e.rackId.toString() === idStr);
          const next = prev.map((e) => {
            if (e.rackId === rackId) return { ...e, aisleIndex: aisle, positionInAisle: position };
            if (e.aisleIndex === aisle && e.positionInAisle === position) {
              return { ...e, aisleIndex: undefined, positionInAisle: undefined };
            }
            return e;
          });
          if (!exists) {
            next.push({ rackId, label, aisleIndex: aisle, positionInAisle: position });
          }
          return next;
        });
        setSelectedCellKey(null);
        setSelectedRackId(null);
        setShowSearchRacks(false);
      };
      if (reparent) {
        promptReparent([reparent], apply);
      } else {
        apply();
      }
    },
    [selectedCellKey, promptReparent],
  );

  const handleRemoveRack = useCallback((rackId: bigint) => {
    setEntries((prev) => prev.filter((e) => e.rackId !== rackId));
    setSelectedRackId((prev) => (prev === rackId ? null : prev));
  }, []);

  const currentRackIds = useMemo(() => entries.map((e) => e.rackId), [entries]);

  // ManageRacksModal confirm — apply the delta against the working
  // set. `added` joins entries (unplaced) without disturbing existing
  // positions; `removed` drops only those entries. Racks not in either
  // list are untouched, so a seeded rack that didn't appear in the
  // picker's listRacks response (race / paging gap) is preserved.
  // `delta.reassigned` (racks currently placed elsewhere) commits the move
  // behind the reparent confirm — their miners move with them — before the
  // working-set change lands.
  const handleManageRacksConfirm = useCallback(
    (delta: RackSelectionDelta) => {
      const apply = () => {
        const removedSet = new Set(delta.removed.map((id) => id.toString()));
        setEntries((prev) => {
          const kept = prev.filter((e) => !removedSet.has(e.rackId.toString()));
          const knownIds = new Set(kept.map((e) => e.rackId.toString()));
          const newcomers: AssignmentEntry[] = [];
          for (const a of delta.added) {
            if (knownIds.has(a.rackId.toString())) continue;
            newcomers.push({ rackId: a.rackId, label: a.label });
          }
          return [...kept, ...newcomers];
        });
        setSelectedRackId(null);
        setSelectedCellKey(null);
        setShowManageRacks(false);
      };
      if (delta.reassigned.length > 0) {
        promptReparent(delta.reassigned, apply);
      } else {
        apply();
      }
    },
    [promptReparent],
  );

  // Save: walk activeAssignments, diff against the load-time snapshot, and
  // fire AssignRacksToBuilding once per target building bucket.
  //
  // All racks staying in this building (placements, unplacements,
  // swaps, "move into occupied cell") ship as a single mixed batch.
  // The server's AssignRacksToBuilding transaction now runs a two-pass
  // write internally — pass 1 clears every requested rack's cell, then
  // pass 2 writes the new (aisle, position) values — so the partial
  // unique index uk_device_set_rack_building_position can't collide
  // mid-batch. That removes the old client-side vacate-then-place
  // split (and its "retry to finish saving" partial-failure path).
  //
  // Racks removed from this building go in a second call with
  // targetBuildingId=undefined since they need a different building
  // bucket. Layout writes live in BuildingSettingsModal.
  const handleSave = useCallback(async () => {
    if (savingRef.current) return;

    // Capacity guard — mirrors ManageRackModal's slot check and the
    // server's AssignRacksToBuilding cap. A building holds at most
    // aisles×racks_per_aisle racks (placed or unplaced); reject before
    // dispatch so an over-capacity working set never reaches the RPC.
    // `entries` is the racks staying in this building (removed ones are
    // already filtered out). Skipped when the grid is unconfigured
    // (capacity 0): racks can be staged before a layout is set.
    const capacity = aislesNum * racksPerAisleNum;
    if (capacity > 0 && entries.length > capacity) {
      setErrorMsg(
        `This building has ${capacity} rack positions (${aislesNum} aisles × ${racksPerAisleNum} per aisle), ` +
          `but ${entries.length} racks are assigned. Remove some racks or increase the layout.`,
      );
      return;
    }

    savingRef.current = true;
    setErrorMsg("");
    setIsSaving(true);
    try {
      const initial = initialPlacementRef.current;
      const currentIds = new Set(entries.map((e) => e.rackId.toString()));

      const inBuilding: RackPlacementInput[] = [];
      const unassign: RackPlacementInput[] = [];

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

        // Single mixed batch.
        //   - placedKey present → place at the new (aisle, position).
        //     Covers both first-time placement and moves; the server's
        //     pass-1 clear handles any prior occupant inside the batch.
        //   - placedKey absent + prior previously placed → send a
        //     member-only entry. The server NULLs the rack's cell in
        //     pass 1 (no pass-2 write because no position is supplied).
        //   - placedKey absent + prior === "missing" → rack is new to
        //     the working set with no chosen cell yet. Send a member-
        //     only assign so the BE links the rack to this building
        //     even without a position. Without this branch, racks
        //     added via Manage racks but never dragged to a cell
        //     silently drop on save.
        if (placedKey) {
          const { aisle, position } = parseCellKey(placedKey);
          inBuilding.push({
            rackId: entry.rackId,
            aisleIndex: aisle,
            positionInAisle: position,
          });
        } else {
          inBuilding.push({ rackId: entry.rackId });
        }
      }

      // Racks removed from this building (in snapshot, not in entries)
      // need an explicit unassign — different target building bucket so
      // they can't ride the in-building batch.
      for (const idStr of initial.keys()) {
        if (currentIds.has(idStr)) continue;
        unassign.push({ rackId: BigInt(idStr) });
      }

      // Buildings can be 100×100 = 10,000 cells, and this modal loads
      // every page, so a large floor-plan save with >1000 changed/
      // removed racks would otherwise hit request validation. Chunk
      // each phase into RPC-sized batches (RACKS_PER_RPC), dispatched
      // sequentially so a mid-chain failure stops the chain (handled by
      // the catch blocks below). Vacate-before-place is enforced across
      // chunks by the two-pass dispatch below — the server only orders
      // clear-then-place within a single RPC, so unassigns and cell-
      // clears must all complete before any place runs.
      // Tracks whether any chunk has committed so the catch below can
      // distinguish "nothing landed" from "partial commit" — operator
      // needs to know to refresh before retrying when chunks N..M ran
      // before chunk N+1 failed.
      let savedAtLeastOne = false;
      const dispatch = async (racks: RackPlacementInput[], targetBuildingId?: bigint) => {
        if (racks.length === 0) return;
        for (let i = 0; i < racks.length; i += RACKS_PER_RPC) {
          const chunk = racks.slice(i, i + RACKS_PER_RPC);
          await new Promise<void>((resolve, reject) => {
            void assignRacksToBuilding({
              racks: chunk,
              targetBuildingId,
              onSuccess: () => resolve(),
              onError: (msg) => reject(new Error(msg)),
            });
          });
          savedAtLeastOne = true;
        }
      };

      // Two-pass shape across chunks: vacate ALL cells before placing
      // ANY rack at a new cell. The server's clear-then-place ordering
      // only applies within a single AssignRacksToBuilding tx, so a
      // >1000-rack save where chunk 2 still owns the cell chunk 1 is
      // trying to claim would trip uk_device_set_rack_building_position.
      //
      // Partition the in-building bucket so the vacate pass also clears
      // the OLD cell of every mover — a rack with both a snapshot
      // position and a new place entry. Otherwise a cross-chunk swap
      // (rack A's new cell is rack B's old cell, A lands in chunk 1, B
      // in chunk 2) would still trip the partial unique index because
      // B's old cell wouldn't vacate until chunk 2 runs.
      //   - vacate entries (no aisle/position) — racks staying in the
      //     building but clearing their cell. Includes:
      //       * explicit cell-clear entries built above,
      //       * a synthetic pre-place vacate for every mover, dedup'd by
      //         rackId so we never send two clears for the same rack.
      //   - place entries (with aisle/position) — racks landing at a
      //     specific cell. These can only run after every vacate above
      //     (plus the unassign bucket) has committed.
      const inBuildingVacate: RackPlacementInput[] = [];
      const inBuildingPlace: RackPlacementInput[] = [];
      const seenVacate = new Set<string>();
      for (const entry of inBuilding) {
        const idStr = entry.rackId.toString();
        const prior = initial.get(idStr) ?? "missing";
        const wasPlaced = prior !== "unplaced" && prior !== "missing";

        if (entry.aisleIndex !== undefined && entry.positionInAisle !== undefined) {
          // Mover (had a prior cell) → schedule a pre-place vacate so
          // its old cell is free before any placement chunk runs.
          if (wasPlaced && !seenVacate.has(idStr)) {
            inBuildingVacate.push({ rackId: entry.rackId });
            seenVacate.add(idStr);
          }
          inBuildingPlace.push(entry);
        } else if (!seenVacate.has(idStr)) {
          // Already a cell-clear-in-place entry.
          inBuildingVacate.push({ rackId: entry.rackId });
          seenVacate.add(idStr);
        }
      }

      try {
        // Pass 1: all vacates (unassign bucket + in-building cell-
        // clears). dispatch short-circuits when the list is empty.
        await dispatch(unassign, undefined);
        await dispatch(inBuildingVacate, building.id);

        // Pass 2: all places. By now every cell that will be reused
        // has been vacated, so no two writes collide on the partial
        // unique index — even across >1000-rack chunked saves.
        await dispatch(inBuildingPlace, building.id);
      } catch (err) {
        const detail = err instanceof Error ? err.message : "Failed to save rack positions";
        if (savedAtLeastOne) {
          setErrorMsg(
            `${detail}. Some changes were saved before the error — refresh the building view to see the current state, then retry to apply the remaining changes.`,
          );
        } else {
          setErrorMsg(`Failed to save rack positions: ${detail}.`);
        }
        return;
      }

      pushToast({ message: `Building "${building.name}" saved`, status: STATUSES.success });
      onSaved?.(building);
      onDismiss();
    } finally {
      savingRef.current = false;
      setIsSaving(false);
    }
  }, [building, rackToCell, entries, aislesNum, racksPerAisleNum, assignRacksToBuilding, onSaved, onDismiss]);

  if (!open) return null;

  const siteId = building.siteId ?? 0n;
  const totalCells = aislesNum * racksPerAisleNum;
  const assignedCount = rackToCell.size;
  const title = building.name || "Manage building";
  const subtitle = siteName ? `in ${siteName}` : undefined;

  return (
    <>
      <FullScreenTwoPaneModal
        open={open}
        title={subtitle ? `${title} — ${subtitle}` : title}
        // Dismiss without Save writes nothing: reparents stage into the working
        // set instead of committing on confirm, so the server is untouched until
        // Save and a plain dismiss needs no host refresh.
        onDismiss={onDismiss}
        isBusy={isSaving}
        buttons={[
          {
            text: "Delete building",
            variant: variants.secondaryDanger,
            onClick: onDeleteRequested,
            disabled: isSaving,
            testId: "manage-building-delete",
          },
          {
            text: "Building settings",
            variant: variants.secondary,
            onClick: onEditDetails,
            disabled: isSaving,
            testId: "manage-building-edit-details",
          },
          {
            // Mirror of ManageRackModal's "Manage Miners" header CTA —
            // the entry point for bulk rack add/remove.
            text: "Manage racks",
            variant: variants.secondary,
            onClick: () => setShowManageRacks(true),
            disabled: isSaving || isLoading || !!loadError,
            testId: "manage-building-manage-racks",
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
            assignmentMode={assignmentMode}
            onModeChange={handleModeChange}
            assignedRacks={assignedRacks}
            selectedRackId={selectedRackId}
            selectedCellKey={selectedCellKey}
            hoveredRackId={hoveredRackId}
            onHoverRack={setHoveredRackId}
            onSelectRack={handleSelectRack}
            onRemoveRack={handleRemoveRack}
            onOpenManageRacks={() => setShowManageRacks(true)}
            unassignedMinerCount={unassignedMinerCount}
            saving={isSaving}
          />
        }
        secondaryPane={
          <BuildingGridPane
            aisles={aislesNum}
            racksPerAisle={racksPerAisleNum}
            cellLabels={cellLabels}
            cellRackIds={activeAssignments}
            onCellClick={assignmentMode === "manual" ? handleCellClick : undefined}
            selectedCellKey={selectedCellKey}
            showPopover={showCellPopover}
            onSelectFromList={handleSelectFromList}
            onSearchRacks={handleSearchRacks}
            onPopoverDismiss={handlePopoverDismiss}
            hasRacks={entries.length > 0}
            hoveredRackId={hoveredRackId}
            onHoverRack={setHoveredRackId}
            onOpenSettings={onEditDetails}
            assignedCount={assignedCount}
            totalCells={totalCells}
          />
        }
      />

      {showManageRacks ? (
        <ManageRacksModal
          open={showManageRacks}
          siteId={siteId}
          currentBuildingId={building.id}
          scope={rackScope}
          assignedScope={assignedScope}
          allSites={activeSite.kind === "all"}
          buildingName={building.name}
          initialSelectedRackIds={currentRackIds}
          onDismiss={() => setShowManageRacks(false)}
          onConfirm={handleManageRacksConfirm}
        />
      ) : null}

      {showSearchRacks ? (
        <SearchRacksModal
          open={showSearchRacks}
          siteId={siteId}
          currentBuildingId={building.id}
          scope={rackScope}
          assignedScope={assignedScope}
          assignedRackIds={currentRackIds}
          buildingName={building.name}
          onDismiss={() => {
            setShowSearchRacks(false);
            setSelectedCellKey(null);
          }}
          onConfirm={handleSearchRackConfirm}
        />
      ) : null}

      {reparentConfirm ? (
        <RackReparentWarningDialog
          racks={reparentConfirm.racks}
          buildingName={building.name}
          // onConfirm stages the move into the working set and clears this
          // dialog; the actual write happens on the outer Save.
          onCancel={() => setReparentConfirm(null)}
          onConfirm={reparentConfirm.onConfirm}
        />
      ) : null}
    </>
  );
};

export default ManageBuildingModal;
