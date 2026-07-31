import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import BuildingRacksPicker, { type CreatedRack } from "../BuildingRacksPicker";
import { type RackSelectionDelta } from "../ManageRacksModal/rackSelectionDelta";
import SearchRacksModal from "../SearchRacksModal";
import {
  type AssignmentEntry,
  buildByNameAssignments,
  buildManualAssignments,
  buildPlacementDelta,
  isPlacementDeltaEmpty,
} from "./assignmentMath";
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
  // State rather than a ref because the Save CTA's dirty gate is derived
  // from it — a ref wouldn't re-derive when the load resolves.
  const [initialPlacement, setInitialPlacement] = useState<Map<string, string>>(new Map());

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
        setInitialPlacement(snapshot);
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

  // The exact batches Save would dispatch.
  const placementDelta = useMemo(
    () => buildPlacementDelta(entries, rackToCell, initialPlacement),
    [entries, rackToCell, initialPlacement],
  );

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

  // Gate a reparent behind the warning dialog; confirming runs the caller's
  // membership commit, which moves the rack into this building via a
  // member-only AssignRacksToBuilding (targetBuildingId → the rack leaves its
  // old building, cascading its miners) exactly like any newly-added rack.
  // Once committed the rack is seeded into the picker, so buildRackPickerItem
  // shows it as "in this building" on reopen — never a reassignment row that a
  // later toggle-off / Select all / deselect could silently drop. Cancelling
  // writes nothing and leaves the picker open.
  const promptReparent = useCallback((racks: ReparentedRack[], apply: () => void) => {
    setReparentConfirm({
      racks,
      onConfirm: () => {
        setReparentConfirm(null);
        apply();
      },
    });
  }, []);

  // Chunked AssignRacksToBuilding dispatcher shared by the membership commits
  // and the placement Save. Buildings can be 100×100 = 10,000 cells and this
  // modal loads every page, so a large batch would otherwise blow past the
  // proto's 1000-rack request cap. Chunks run sequentially so a mid-chain
  // failure stops the chain; onChunkCommitted lets the caller tell "nothing
  // landed" from "partial commit".
  const dispatchAssign = useCallback(
    async (racks: RackPlacementInput[], targetBuildingId?: bigint, onChunkCommitted?: () => void) => {
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
        onChunkCommitted?.();
      }
    },
    [assignRacksToBuilding],
  );

  // Membership commit. The rack pickers own building membership, so a confirmed
  // selection is written straight away and only placement stays staged for
  // Save. Newcomers go in as member-only assigns (no cell) and land in the
  // load-time snapshot as "unplaced", so freshly-committed membership doesn't
  // read as pending placement dirt on the Save gate.
  //
  // The caller owns the working-set update — each entry point merges rows
  // differently — and should skip it when this returns false.
  // Drops racks from every piece of local state that keys off them, for use the
  // moment an unassign is known to have persisted.
  const forgetRacks = useCallback((rackIds: bigint[]) => {
    const gone = new Set(rackIds.map((id) => id.toString()));
    setEntries((prev) => prev.filter((e) => !gone.has(e.rackId.toString())));
    setInitialPlacement((prev) => {
      const next = new Map(prev);
      for (const id of gone) next.delete(id);
      return next;
    });
    setSelectedRackId((prev) => (prev !== null && gone.has(prev.toString()) ? null : prev));
  }, []);

  const commitMembership = useCallback(
    async (added: AssignmentEntry[], removed: bigint[]): Promise<boolean> => {
      if (savingRef.current) return false;
      const currentIds = new Set(entries.map((e) => e.rackId.toString()));
      const newcomers = added.filter((a) => !currentIds.has(a.rackId.toString()));
      // Nothing to write. The caller may still have a placement change to
      // stage (e.g. re-placing a rack that's already a member).
      if (newcomers.length === 0 && removed.length === 0) return true;

      // Capacity guard — mirrors handleSave's and the server's
      // AssignRacksToBuilding cap. Skipped when the grid is unconfigured
      // (capacity 0): racks can join before a layout is set.
      const capacity = aislesNum * racksPerAisleNum;
      const nextCount = entries.length + newcomers.length - removed.length;
      if (capacity > 0 && nextCount > capacity) {
        setErrorMsg(
          `This building has ${capacity} rack positions (${aislesNum} aisles × ${racksPerAisleNum} per aisle), ` +
            `but the change would assign ${nextCount} racks. Remove some racks or increase the layout.`,
        );
        return false;
      }

      savingRef.current = true;
      setErrorMsg("");
      setIsSaving(true);
      try {
        // Removals first: they free their cells before any newcomer lands, and
        // the reverse order would trip the capacity check above on a swap.
        if (removed.length > 0) {
          await dispatchAssign(
            removed.map((rackId) => ({ rackId })),
            undefined,
          );
          // Persisted. Drop them from the working set and tell the host now, so
          // a failure in the newcomer write below still leaves both truthful —
          // the callers skip their own update when this returns false.
          forgetRacks(removed);
          onSaved?.(building);
        }
        if (newcomers.length > 0) {
          await dispatchAssign(
            newcomers.map((a) => ({ rackId: a.rackId })),
            building.id,
          );
        }
      } catch (err) {
        const detail = err instanceof Error ? err.message : "Failed to update racks";
        setErrorMsg(`Failed to update racks: ${detail}.`);
        return false;
      } finally {
        savingRef.current = false;
        setIsSaving(false);
      }

      // Removals were folded in as they landed; only the newcomers are left to
      // record, so freshly-committed membership doesn't read as pending dirt.
      setInitialPlacement((prev) => {
        const next = new Map(prev);
        for (const a of newcomers) next.set(a.rackId.toString(), "unplaced");
        return next;
      });
      // Rack counts changed on the host's building cache.
      onSaved?.(building);
      return true;
    },
    [entries, aislesNum, racksPerAisleNum, dispatchAssign, building, forgetRacks, onSaved],
  );

  // SearchRacksModal confirm — commit the rack into this building if it isn't
  // a member yet, then stage it at the cell that was selected when the popover
  // opened. A rack currently placed elsewhere goes behind the reparent confirm
  // (its miners move with it) before either happens.
  const handleSearchRackConfirm = useCallback(
    (rackId: bigint, label: string, reparent?: ReparentedRack) => {
      const targetKey = selectedCellKey;
      if (targetKey === null) {
        setShowSearchRacks(false);
        return;
      }
      const apply = async () => {
        const ok = await commitMembership([{ rackId, label }], []);
        if (!ok) return;
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
        promptReparent([reparent], () => void apply());
      } else {
        void apply();
      }
    },
    [selectedCellKey, promptReparent, commitMembership],
  );

  // Row-level "Remove rack" — an immediate unassign (the rack moves out of the
  // building; it is not deleted). The row drops only once the write lands.
  const handleRemoveRack = useCallback(
    async (rackId: bigint) => {
      const ok = await commitMembership([], [rackId]);
      if (!ok) return;
      setEntries((prev) => prev.filter((e) => e.rackId !== rackId));
      setSelectedRackId((prev) => (prev === rackId ? null : prev));
    },
    [commitMembership],
  );

  const currentRackIds = useMemo(() => entries.map((e) => e.rackId), [entries]);

  // ManageRacksModal Save — the picker owns building membership, so the delta is
  // written here rather than staged. `added` joins entries unplaced (placement
  // is still the operator's to set, on this modal's Save); `removed` drops only
  // those entries. Racks in neither list are untouched, so a seeded rack the
  // picker's listRacks response omitted (race / paging gap) is preserved.
  // Racks currently in another building arrive already confirmed — the picker
  // owns that warning, since it is the surface where they were chosen.
  const handleManageRacksConfirm = useCallback(
    (delta: RackSelectionDelta) => {
      void (async () => {
        const removedSet = new Set(delta.removed.map((id) => id.toString()));
        const kept = entries.filter((e) => !removedSet.has(e.rackId.toString()));
        const knownIds = new Set(kept.map((e) => e.rackId.toString()));
        const newcomers: AssignmentEntry[] = [];
        for (const a of delta.added) {
          if (knownIds.has(a.rackId.toString())) continue;
          newcomers.push({ rackId: a.rackId, label: a.label });
        }
        const ok = await commitMembership(newcomers, delta.removed);
        // Closes either way. This modal's error callout sits behind the picker,
        // and on a partial failure commitMembership has already folded in the
        // removals that landed — so the picker's staged selection is the stale
        // view of the two, not something worth keeping to retry from.
        setShowManageRacks(false);
        if (!ok) return;
        setEntries([...kept, ...newcomers]);
        setSelectedRackId(null);
        setSelectedCellKey(null);
      })();
    },
    [entries, commitMembership],
  );

  // Racks created from inside the picker. The create call placed them in this
  // building already, so there is no membership write to make — they join the
  // working set unplaced, exactly like a picker addition, and this modal's Save
  // is where the operator gives them grid positions.
  const handleRacksCreated = useCallback(
    (racks: CreatedRack[]) => {
      setEntries((prev) => {
        const known = new Set(prev.map((e) => e.rackId.toString()));
        const newcomers = racks
          .filter((r) => !known.has(r.rackId.toString()))
          .map((r) => ({ rackId: r.rackId, label: r.label }));
        return newcomers.length > 0 ? [...prev, ...newcomers] : prev;
      });
      // The create already placed them in the building, so the host's rack count
      // is stale from this moment — not from whenever Save happens. An operator
      // who dismisses without saving still made this change.
      onSaved?.(building);
    },
    [building, onSaved],
  );

  // Save owns rack placement only — membership commits in the pickers and
  // layout lives in BuildingSettingsModal. Dispatches placementDelta's buckets
  // as AssignRacksToBuilding calls against building.id.
  const handleSave = useCallback(async () => {
    if (savingRef.current) return;
    // Nothing to dispatch. Falling through would toast a save that wrote
    // nothing, so close instead — reviewing the layout and keeping it as-is is a
    // legitimate outcome, not an error.
    if (isPlacementDeltaEmpty(placementDelta)) {
      onDismiss();
      return;
    }

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
      const { unassign, inBuildingVacate, inBuildingPlace } = placementDelta;

      // Tracks whether any chunk has committed so the catch below can
      // distinguish "nothing landed" from "partial commit" — the operator
      // needs to know to refresh before retrying when chunks N..M ran
      // before chunk N+1 failed.
      let savedAtLeastOne = false;
      const onChunk = () => {
        savedAtLeastOne = true;
      };

      try {
        // Two-pass shape across chunks: vacate ALL cells (buildPlacementDelta
        // already folded each mover's old cell into inBuildingVacate) before
        // placing ANY rack at a new cell. The server's clear-then-place
        // ordering only applies within a single AssignRacksToBuilding tx, so
        // a >1000-rack save where chunk 2 still owns the cell chunk 1 is
        // trying to claim would trip uk_device_set_rack_building_position.
        //
        // Pass 1: all vacates. The unassign bucket is normally empty now that
        // removals commit in the pickers — it stays wired as a reconcile path
        // for any entry that leaves the list without a write.
        await dispatchAssign(unassign, undefined, onChunk);
        await dispatchAssign(inBuildingVacate, building.id, onChunk);

        // Pass 2: all places. By now every cell that will be reused
        // has been vacated, so no two writes collide on the partial
        // unique index — even across >1000-rack chunked saves.
        await dispatchAssign(inBuildingPlace, building.id, onChunk);
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

      pushToast({ message: `Rack positions saved`, status: STATUSES.success });
      onSaved?.(building);
      onDismiss();
    } finally {
      savingRef.current = false;
      setIsSaving(false);
    }
  }, [building, placementDelta, entries, aislesNum, racksPerAisleNum, dispatchAssign, onSaved, onDismiss]);

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
        // Dismissing abandons only staged placement — membership changes have
        // already committed (and already fired onSaved), so there's nothing
        // left for a dismiss to reconcile.
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
            // Dirty-gated: Save writes rack placement, so with no pending
            // change there's nothing to commit. isLoading stays in the gate
            // because the baseline snapshot isn't loaded yet — everything
            // would read clean for the wrong reason.
            // No dirty gate: the no-op case closes in handleSave, where it can
            // be told apart from a real save. The load guards stay — a delta
            // diffed against a placement that never arrived isn't a write.
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
        <BuildingRacksPicker
          siteId={siteId}
          buildingId={building.id}
          buildingName={building.name}
          currentRackIds={currentRackIds}
          onDismiss={() => setShowManageRacks(false)}
          onConfirm={handleManageRacksConfirm}
          onCreated={handleRacksCreated}
          saving={isSaving}
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
          // onConfirm clears this dialog and lets the membership commit run —
          // accepting the warning is what authorizes the reparent write.
          onCancel={() => setReparentConfirm(null)}
          onConfirm={reparentConfirm.onConfirm}
        />
      ) : null}
    </>
  );
};

export default ManageBuildingModal;
