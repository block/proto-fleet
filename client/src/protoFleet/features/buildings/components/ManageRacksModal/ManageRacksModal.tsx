import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { buildRackPickerItem, describeRackReassignment, type RackPickerItem } from "../rackPickerItem";
import { computeRackSelectionDelta, type RackSelectionDelta } from "./rackSelectionDelta";
import { useBuildings } from "@/protoFleet/api/buildings";
import { useSites } from "@/protoFleet/api/sites";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import { type SiteFilterFields } from "@/protoFleet/components/PageHeader/SitePicker";
import { useHasPermission } from "@/protoFleet/store";
import { Alert, ChevronDown, Info, Plus } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import List from "@/shared/components/List";
import type { ActiveFilters, FilterItem, NestedFilterChildItem } from "@/shared/components/List/Filters/types";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import Modal, { ModalSelectAllFooter } from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Switch from "@/shared/components/Switch";
import { pushToast, STATUSES } from "@/shared/features/toaster";

type RackPickerColumn = "name" | "building" | "status";

interface ManageRacksModalProps {
  open: boolean;
  // Parent building context drives the eligibility split.
  siteId: bigint;
  currentBuildingId: bigint;
  // Assignable-only fetch scope, derived from the building's own site (see
  // buildingRackScope) — NOT the header SitePicker. Used while "Show assigned
  // racks" is OFF: the building's site + site-unassigned. Per-row eligibility is
  // still computed against `siteId`.
  scope: SiteFilterFields;
  // Broadened fetch scope used while "Show assigned racks" is ON, so
  // already-placed (ineligible) racks surface for reparenting. All-sites header
  // → global fetch (cross-site racks); scoped header → same as `scope`. See
  // assignedRackScope.
  assignedScope: SiteFilterFields;
  // True when the header SitePicker is on "all sites". Gates the Site facet
  // (offered only when the fetch can span sites), mirroring the miner picker's
  // `showSiteFilter: !scope`.
  allSites: boolean;
  buildingName: string;
  // Rack IDs currently in the building's working set. The modal seeds its
  // selection with these so the operator sees the current state and can
  // add / remove in one flow.
  initialSelectedRackIds: bigint[];
  onDismiss: () => void;
  // Returns the delta against initialSelectedRackIds. `delta.reassigned` reports
  // the added racks that are being reparented so the host can gate the reparent
  // confirm before committing. Computed against the items-by-id accumulator
  // (every rack seen across pages / select-all), NOT just the current page.
  onConfirm: (delta: RackSelectionDelta) => void;
}

const PAGE_SIZE = 50;

const colTitles: ColTitles<RackPickerColumn> = {
  name: "Name",
  building: "Building",
  status: "Status",
};

const colConfig: ColConfig<RackPickerItem, string, RackPickerColumn> = {
  name: {
    component: (item) => <span>{item.label || "(unnamed rack)"}</span>,
    width: "min-w-32",
  },
  building: {
    component: (item) => <span>{item.buildingLabel}</span>,
    width: "min-w-32",
  },
  status: {
    component: (item) => <span>{item.statusLabel}</span>,
    width: "min-w-32",
  },
};

const activeCols: RackPickerColumn[] = ["name", "building", "status"];

const ManageRacksModal = ({
  open,
  siteId,
  currentBuildingId,
  scope,
  assignedScope,
  allSites,
  buildingName,
  initialSelectedRackIds,
  onDismiss,
  onConfirm,
}: ManageRacksModalProps) => {
  const { listRacks } = useDeviceSets();
  const { listBuildingsBySite, listBuildings } = useBuildings();
  const { listSites } = useSites();
  const canReadSiteCatalog = useHasPermission("site:read");

  const [pageItems, setPageItems] = useState<RackPickerItem[] | undefined>(undefined);
  const [error, setError] = useState<string | null>(null);
  const [selectedItems, setSelectedItems] = useState<string[]>(() => initialSelectedRackIds.map((id) => id.toString()));
  // "Show assigned racks" — default off, so the list starts with only the
  // assignable set (this building's site + unassigned, eligible rows). Turning
  // it on broadens the fetch and surfaces racks already placed elsewhere; they
  // become selectable (reparenting moves them, behind a confirm) and are flagged
  // with a warning icon.
  const [showAssigned, setShowAssigned] = useState(false);
  const [showAssignedInfo, setShowAssignedInfo] = useState(false);
  // The reassignment row whose conflict dialog is open, or null.
  const [conflictInfoItem, setConflictInfoItem] = useState<RackPickerItem | null>(null);
  // User facet selections. Building narrows within the fetched set; Site
  // (offered only under all-sites) narrows the site scope.
  const [facetBuildingIds, setFacetBuildingIds] = useState<bigint[]>([]);
  const [facetSiteIds, setFacetSiteIds] = useState<bigint[]>([]);
  const [availableBuildings, setAvailableBuildings] = useState<{ id: string; label: string }[]>([]);
  const [availableSites, setAvailableSites] = useState<{ id: string; label: string }[]>([]);
  // Self-fetched building id → display label map for the Building column. Falls
  // back to "—" via buildRackPickerItem when an id is missing.
  const [buildingMap, setBuildingMap] = useState<Record<string, string>>({});

  // --- Pagination state ---
  // Server-side pagination. `pageTokensRef[i]` is the page token to fetch page
  // `i` (index 0 → ""); grown as the operator pages forward so Previous can
  // return without re-deriving tokens. Kept in a ref so mutating it never
  // retriggers the fetch effect.
  const [pageIndex, setPageIndex] = useState(0);
  const [nextPageToken, setNextPageToken] = useState("");
  const [totalCount, setTotalCount] = useState(0);
  const pageTokensRef = useRef<string[]>([""]);
  // Every rack seen this session (across pages + select-all), keyed by id.
  // computeRackSelectionDelta needs the FULL set, not just the current page —
  // server pagination only hands us one page at a time. Reset when the request
  // shape changes (the set it describes changed).
  const accumulatorRef = useRef<Map<string, RackPickerItem>>(new Map());

  // Racks already in the working set. Passed to buildRackPickerItem so a seeded
  // rack — including a reparent staged earlier this session but not yet Saved —
  // classifies as "in this building" instead of a reassignment row derived from
  // its stale server placement.
  const seededRackIds = useMemo(
    () => new Set(initialSelectedRackIds.map((id) => id.toString())),
    [initialSelectedRackIds],
  );

  // The effective server request. Everything that narrows the result set lives
  // here so pagination stays correct — there is no client-side filtering of a
  // fetched page (that would empty pages after the fact and break page counts /
  // select-all). Toggle OFF pins the fetch to the assignable set (this building
  // + no-building racks, within the site scope); toggle ON broadens to
  // assignedScope and lets facets narrow freely. Mirrors MinerSelectionList's
  // derived filter.
  const request = useMemo(() => {
    const base = showAssigned ? assignedScope : scope;
    let siteIds = base.siteIds;
    let includeUnassigned = base.includeUnassigned;
    // Site facet (offered only under all-sites) narrows the site scope.
    if (facetSiteIds.length > 0) {
      siteIds = facetSiteIds;
      includeUnassigned = false;
    }
    let buildingIds: bigint[];
    let includeNoBuilding: boolean;
    if (!showAssigned) {
      // Assignable-only: return only this building's racks + no-building racks.
      // A Building facet on another building yields an empty intersection,
      // surfaced as placementFacetConflict below rather than a broadened fetch.
      if (facetBuildingIds.length > 0) {
        buildingIds = facetBuildingIds.filter((id) => id === currentBuildingId);
        includeNoBuilding = false;
      } else {
        buildingIds = [currentBuildingId];
        includeNoBuilding = true;
      }
    } else {
      buildingIds = facetBuildingIds;
      includeNoBuilding = false;
    }
    return { siteIds, includeUnassigned, buildingIds, includeNoBuilding };
  }, [showAssigned, scope, assignedScope, facetSiteIds, facetBuildingIds, currentBuildingId]);

  // Assignable-only + a Building facet that excludes this building = provably no
  // assignable matches. Show empty rather than fetch (an empty building_ids with
  // include_no_building=false drops the building predicate and would leak the
  // whole site). Self-correcting once the facet is cleared. Mirrors
  // MinerSelectionList's placementFacetConflict.
  const placementFacetConflict =
    !showAssigned && facetBuildingIds.length > 0 && !facetBuildingIds.includes(currentBuildingId);

  const requestKey = useMemo(
    () =>
      JSON.stringify({
        s: request.siteIds.map(String),
        u: request.includeUnassigned,
        b: request.buildingIds.map(String),
        nb: request.includeNoBuilding,
        conflict: placementFacetConflict,
      }),
    [request, placementFacetConflict],
  );

  // Live mirrors read by the fetch effect / callbacks so they aren't listed as
  // effect deps (which would churn identity every render).
  const requestRef = useRef(request);
  requestRef.current = request;
  const showAssignedRef = useRef(showAssigned);
  showAssignedRef.current = showAssigned;
  const buildingMapRef = useRef(buildingMap);
  buildingMapRef.current = buildingMap;

  // Building-label lookup for the Building column (this building's site).
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    void listBuildingsBySite({
      siteId,
      onSuccess: (rows) => {
        if (cancelled) return;
        const out: Record<string, string> = {};
        for (const row of rows) {
          const b = row.building;
          if (b) out[b.id.toString()] = b.name;
        }
        setBuildingMap(out);
      },
      onError: () => {
        if (!cancelled) setBuildingMap({});
      },
    });
    return () => {
      cancelled = true;
    };
  }, [open, siteId, listBuildingsBySite]);

  // Facet dropdown options. Building options scope to the active site(s) (the
  // building's site, or org-wide under all-sites). Site options only fetched
  // (and only offered) under all-sites, gated by site:read like the miner picker.
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    const optionSiteIds = allSites ? [] : scope.siteIds;
    void listBuildings({
      siteIds: optionSiteIds,
      onSuccess: (rows) => {
        if (cancelled) return;
        setAvailableBuildings(
          rows
            .filter((b) => b.building !== undefined)
            .map((b) => ({ id: String(b.building!.id), label: b.building!.name })),
        );
      },
      onError: () => {
        if (!cancelled) setAvailableBuildings([]);
      },
    });
    if (allSites && canReadSiteCatalog) {
      void listSites({
        onSuccess: (rows) => {
          if (cancelled) return;
          setAvailableSites(
            rows.filter((s) => s.site !== undefined).map((s) => ({ id: String(s.site!.id), label: s.site!.name })),
          );
        },
        onError: () => {
          if (!cancelled) setAvailableSites([]);
        },
      });
    }
    return () => {
      cancelled = true;
    };
  }, [open, allSites, scope.siteIds, canReadSiteCatalog, listBuildings, listSites]);

  // Reset pagination whenever the request shape changes — the described set is
  // different, so tokens and the accumulator no longer apply. Done during render
  // (React's "adjust state when a prop changes" pattern) rather than in an effect
  // so the reset lands before the fetch effect reads pageIndex, and to avoid the
  // cascading extra render a setState-in-effect would cause.
  const prevRequestKeyRef = useRef(requestKey);
  if (prevRequestKeyRef.current !== requestKey) {
    prevRequestKeyRef.current = requestKey;
    pageTokensRef.current = [""];
    accumulatorRef.current = new Map();
    setPageIndex(0);
    setError(null);
  }

  // Fetch the current page. Builds picker items and folds them into the
  // accumulator. Ineligible racks are excluded server-side while the toggle is
  // off, so no client-side filtering happens here.
  useEffect(() => {
    if (!open) return;
    // A placement-facet conflict has no assignable matches — skip the fetch and
    // let the derived display values below render the empty state.
    if (placementFacetConflict) return;
    // After a reset, pageTokensRef holds only index 0; a stale pageIndex > 0
    // reads undefined and waits for the reset's setPageIndex(0) to land.
    const token = pageTokensRef.current[pageIndex];
    if (token === undefined) return;
    let cancelled = false;
    const req = requestRef.current;
    void listRacks({
      siteIds: req.siteIds,
      includeUnassigned: req.includeUnassigned,
      buildingIds: req.buildingIds,
      includeNoBuilding: req.includeNoBuilding,
      pageSize: PAGE_SIZE,
      pageToken: token,
      onSuccess: (racks, next, total) => {
        if (cancelled) return;
        const out: RackPickerItem[] = [];
        for (const rack of racks) {
          const item = buildRackPickerItem(rack, siteId, currentBuildingId, buildingMapRef.current, seededRackIds);
          if (item) {
            out.push(item);
            accumulatorRef.current.set(item.id, item);
          }
        }
        out.sort((a, b) => a.label.localeCompare(b.label));
        pageTokensRef.current[pageIndex + 1] = next;
        setPageItems(out);
        setNextPageToken(next);
        setTotalCount(total);
        setError(null);
      },
      onError: (msg) => {
        if (cancelled) return;
        // A failed *broadened* (toggle-on) fetch must not strand the operator
        // behind the blocking error state (which hides the Switch). Revert the
        // toggle — that changes the request and refetches the scoped set — drop
        // any reparent picks, and surface the failure as a toast.
        if (showAssignedRef.current) {
          pushToast({ message: `Couldn't load assigned racks: ${msg}`, status: STATUSES.error });
          const acc = accumulatorRef.current;
          setSelectedItems((sel) => sel.filter((id) => !acc.get(id)?.reassignment));
          setConflictInfoItem(null);
          setShowAssigned(false);
          return;
        }
        setError(msg);
        setPageItems([]);
      },
    });
    return () => {
      cancelled = true;
    };
  }, [
    open,
    requestKey,
    pageIndex,
    buildingMap,
    siteId,
    currentBuildingId,
    seededRackIds,
    placementFacetConflict,
    listRacks,
  ]);

  const goToNextPage = useCallback(() => {
    if (!nextPageToken) return;
    pageTokensRef.current[pageIndex + 1] = nextPageToken;
    setPageIndex((i) => i + 1);
  }, [nextPageToken, pageIndex]);

  const goToPrevPage = useCallback(() => setPageIndex((i) => Math.max(0, i - 1)), []);

  // With the toggle on, reassignment rows are intentionally selectable (behind
  // the reparent confirm at commit); nothing else is ever disabled.
  const isRowDisabled = useCallback((item: RackPickerItem) => item.disabled && !showAssigned, [showAssigned]);

  // Flip the toggle. Turning it OFF drops any selected reassignment racks (now
  // excluded from the fetch, so leaving them selected would silently reparent
  // them on Continue) and closes the conflict dialog. Seeded racks classify
  // in-this-building, so nothing seeded is at risk. The request change resets
  // pagination via the effect above. Matches Switch's setChecked signature.
  const handleToggleShowAssigned = useCallback((value: boolean | ((prev: boolean) => boolean)) => {
    const next = typeof value === "function" ? value(showAssignedRef.current) : value;
    if (!next) {
      const acc = accumulatorRef.current;
      setSelectedItems((sel) => sel.filter((id) => !acc.get(id)?.reassignment));
      setConflictInfoItem(null);
    }
    setShowAssigned(next);
  }, []);

  const handleServerFilter = useCallback(async (active: ActiveFilters) => {
    const buildingFilters = active.dropdownFilters.building;
    setFacetBuildingIds(buildingFilters && buildingFilters.length > 0 ? buildingFilters.map((id) => BigInt(id)) : []);
    const siteFilters = active.dropdownFilters.site;
    setFacetSiteIds(siteFilters && siteFilters.length > 0 ? siteFilters.map((id) => BigInt(id)) : []);
  }, []);

  // "Add filter" popover: Building always, Site only under all-sites (site:read).
  const filters = useMemo((): FilterItem[] => {
    const children: NestedFilterChildItem[] = [
      {
        type: "dropdown",
        title: "Building",
        value: "building",
        options: availableBuildings,
        defaultOptionIds: [],
      },
    ];
    if (allSites && canReadSiteCatalog) {
      children.push({
        type: "dropdown",
        title: "Site",
        value: "site",
        options: availableSites,
        defaultOptionIds: [],
      });
    }
    return [
      {
        type: "nestedFilterDropdown",
        title: "Add filter",
        value: "filters-meta",
        prefixIcon: <Plus width="w-3" />,
        children,
      },
    ];
  }, [availableBuildings, availableSites, allSites, canReadSiteCatalog]);

  // Name column renders a warning icon on reassignment rows while the toggle is
  // on; tapping it opens the per-row conflict dialog.
  const listColConfig = useMemo<ColConfig<RackPickerItem, string, RackPickerColumn>>(() => {
    if (!showAssigned) return colConfig;
    return {
      ...colConfig,
      name: {
        width: "min-w-32",
        component: (item: RackPickerItem) => (
          <div className="flex items-center justify-between gap-2">
            <span>{item.label || "(unnamed rack)"}</span>
            {item.reassignment ? (
              <Button
                variant={variants.textOnly}
                textOnlyUnderlineOnHover={false}
                ariaLabel="Reparent conflict — view details"
                prefixIcon={<Alert className="text-text-emphasis" />}
                onClick={(e) => {
                  e.stopPropagation();
                  setConflictInfoItem(item);
                }}
              />
            ) : null}
          </div>
        ),
      },
    };
  }, [showAssigned]);

  const handleConfirm = useCallback(() => {
    if (pageItems === undefined) return;
    onConfirm(computeRackSelectionDelta([...accumulatorRef.current.values()], initialSelectedRackIds, selectedItems));
  }, [pageItems, selectedItems, initialSelectedRackIds, onConfirm]);

  // Footer "Select all" (offered only with the toggle off — see below) selects
  // every ELIGIBLE rack across all pages, not just the visible page. Server
  // pagination hands us one page at a time, so we fetch the full assignable set
  // (the toggle-off request, unpaginated) and select those ids, folding them
  // into the accumulator so Continue can build the delta. Mirrors the miner
  // picker's fetchAllSelectableMinerIds.
  const handleSelectAll = useCallback(() => {
    void listRacks({
      siteIds: scope.siteIds,
      includeUnassigned: scope.includeUnassigned,
      buildingIds: [currentBuildingId],
      includeNoBuilding: true,
      onSuccess: (racks) => {
        const ids: string[] = [];
        for (const rack of racks) {
          const item = buildRackPickerItem(rack, siteId, currentBuildingId, buildingMapRef.current, seededRackIds);
          if (item) {
            accumulatorRef.current.set(item.id, item);
            ids.push(item.id);
          }
        }
        setSelectedItems(ids);
      },
      onError: (msg) => {
        pushToast({ message: `Couldn't select all racks: ${msg}`, status: STATUSES.error });
      },
    });
  }, [listRacks, scope.siteIds, scope.includeUnassigned, currentBuildingId, siteId, seededRackIds]);

  const handleSelectNone = useCallback(() => setSelectedItems([]), []);

  // A placement-facet conflict is provably empty; present it as such even though
  // the last successful fetch may still be held in pageItems/totalCount.
  const displayItems = placementFacetConflict ? [] : pageItems;
  const displayTotal = placementFacetConflict ? 0 : totalCount;
  const displayNextToken = placementFacetConflict ? "" : nextPageToken;

  const pageStart = pageIndex * PAGE_SIZE;
  const showFooterPagination = displayTotal > PAGE_SIZE;

  return (
    <Modal
      open={open}
      title="Select racks"
      size="large"
      className="flex !h-[calc(100dvh-(--spacing(32)))] max-h-[calc(100dvh-(--spacing(32)))] flex-col !overflow-hidden"
      bodyClassName="flex flex-1 min-h-0 flex-col"
      onDismiss={onDismiss}
      divider={false}
      testId="manage-racks-modal"
      buttons={[
        {
          text: "Continue",
          variant: "primary",
          onClick: handleConfirm,
          dismissModalOnClick: false,
          testId: "manage-racks-modal-confirm",
        },
      ]}
    >
      <div className="flex h-full min-h-0 flex-col">
        {error ? (
          <div className="py-6 text-300 text-intent-critical-fill" data-testid="manage-racks-modal-error">
            {error}
          </div>
        ) : displayItems === undefined ? (
          <div className="flex flex-1 items-center justify-center py-12">
            <ProgressCircular indeterminate />
          </div>
        ) : (
          <>
            <div className="min-h-0 flex-1 overflow-y-auto">
              <List<RackPickerItem, string, RackPickerColumn>
                activeCols={activeCols}
                colTitles={colTitles}
                colConfig={listColConfig}
                filters={filters}
                onServerFilter={handleServerFilter}
                headerControls={
                  <div className="flex items-center gap-1 px-1">
                    <Button
                      variant={variants.textOnly}
                      textOnlyUnderlineOnHover={false}
                      ariaLabel="About “Show assigned racks”"
                      prefixIcon={<Info className="text-text-primary-70" />}
                      onClick={() => setShowAssignedInfo(true)}
                    />
                    <Switch
                      label="Show assigned racks"
                      ariaLabel="Show assigned racks"
                      checked={showAssigned}
                      setChecked={handleToggleShowAssigned}
                    />
                  </div>
                }
                items={displayItems}
                itemKey="id"
                itemSelectable
                selectionType="checkbox"
                customSelectedItems={selectedItems}
                customSetSelectedItems={setSelectedItems}
                preserveOffPageSelection
                isRowDisabled={isRowDisabled}
                itemName={{ singular: "rack", plural: "racks" }}
                hideTotal
                containerClassName="min-h-0"
                tableClassName="mb-0"
                overflowContainer
                stickyBgColor="bg-surface-elevated-base"
                footerContent={
                  showFooterPagination ? (
                    <div className="flex flex-col items-center gap-4 py-6">
                      <span className="text-300 text-text-primary">
                        Showing {pageStart + 1}–{pageStart + displayItems.length} of {displayTotal} racks
                      </span>
                      <div className="flex gap-3">
                        <Button
                          variant={variants.secondary}
                          size={sizes.compact}
                          ariaLabel="Previous page"
                          prefixIcon={<ChevronDown className="rotate-90" />}
                          onClick={goToPrevPage}
                          disabled={pageIndex === 0}
                        />
                        <Button
                          variant={variants.secondary}
                          size={sizes.compact}
                          ariaLabel="Next page"
                          prefixIcon={<ChevronDown className="rotate-270" />}
                          onClick={goToNextPage}
                          disabled={!displayNextToken}
                        />
                      </div>
                    </div>
                  ) : null
                }
              />
            </div>
            <div className="shrink-0">
              <ModalSelectAllFooter
                label={`${selectedItems.length} ${selectedItems.length === 1 ? "rack" : "racks"} selected`}
                // Hide "Select all" while ineligible (reassignment) racks are in
                // the fetch — a bulk select-all can't sweep them into a reparent.
                // Matches MinerSelectionList. The in-table header checkbox still
                // selects the whole page (reparent rows included), gated by the
                // reparent confirm on Continue.
                onSelectAll={showAssigned ? undefined : handleSelectAll}
                onSelectNone={handleSelectNone}
              />
            </div>
          </>
        )}
        {showAssignedInfo ? (
          <Dialog
            icon={<Info />}
            title="Show assigned racks"
            subtitle="Shows or hides racks that are already placed in another building or site. Assigning one of these racks to this building moves the rack — and every miner in it — out of its current placement."
            onDismiss={() => setShowAssignedInfo(false)}
            buttons={[{ text: "Got it", variant: variants.primary, onClick: () => setShowAssignedInfo(false) }]}
          />
        ) : null}
        {conflictInfoItem ? (
          <Dialog
            icon={<Alert className="text-text-emphasis" />}
            title="Reparent conflict"
            subtitle={describeRackReassignment(conflictInfoItem, buildingName)}
            onDismiss={() => setConflictInfoItem(null)}
            buttons={[{ text: "Got it", variant: variants.primary, onClick: () => setConflictInfoItem(null) }]}
          />
        ) : null}
      </div>
    </Modal>
  );
};

export default ManageRacksModal;
