import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { buildRackPickerItem, describeRackReassignment, type RackPickerItem } from "../rackPickerItem";
import { computeRackSelectionDelta, type RackSelectionDelta } from "./rackSelectionDelta";
import { useBuildings } from "@/protoFleet/api/buildings";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import { type SiteFilterFields } from "@/protoFleet/components/PageHeader/SitePicker";
import { Alert, ChevronDown, Info } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import List from "@/shared/components/List";
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
  // Rack-fetch scope, derived from the building's own site (see
  // buildingRackScope) — NOT the header SitePicker. Governs which racks are
  // *fetched*: the building's site + site-unassigned. There is no "all sites"
  // fallback; the fetch is always scoped. Per-row eligibility is still
  // computed against `siteId`.
  scope: SiteFilterFields;
  // Broadened fetch scope used while "Show assigned racks" is ON, so
  // already-placed (ineligible) racks surface for reparenting. All-sites header
  // → global fetch (cross-site racks); scoped header → same as `scope` (the
  // site scope already covers other-building same-site racks — the toggle just
  // stops hiding them). See assignedRackScope.
  assignedScope: SiteFilterFields;
  buildingName: string;
  // Rack IDs currently in the building's working set. The modal seeds its
  // selection with these so the operator sees the current state and can
  // add / remove in one flow.
  initialSelectedRackIds: bigint[];
  onDismiss: () => void;
  // Returns the delta against initialSelectedRackIds: `added` is the
  // newly-checked racks (rackId + label so the caller can render
  // without a separate lookup); `removed` is the seeded ids the
  // operator unchecked. Racks the operator did not touch are not in
  // either list — the caller leaves them as-is. Delta-shape avoids
  // accidentally unassigning a seeded rack that didn't make it into
  // the listRacks response (race, paging gap, soft-delete window).
  // `delta.reassigned` reports the added racks that are being reparented so the
  // host can gate the reparent confirm before committing.
  onConfirm: (delta: RackSelectionDelta) => void;
}

const PAGE_SIZE = 25;

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
  buildingName,
  initialSelectedRackIds,
  onDismiss,
  onConfirm,
}: ManageRacksModalProps) => {
  const { listRacks } = useDeviceSets();
  const { listBuildingsBySite } = useBuildings();
  const [items, setItems] = useState<RackPickerItem[] | undefined>(undefined);
  const [error, setError] = useState<string | null>(null);
  const [selectedItems, setSelectedItems] = useState<string[]>(() => initialSelectedRackIds.map((id) => id.toString()));
  const [page, setPage] = useState(0);
  // "Show assigned racks" — default off, so the list starts with only the
  // assignable set (this building's site + unassigned, eligible rows). Turning
  // it on surfaces racks already placed in another building/site; they become
  // selectable (reparenting moves them, behind a confirm) and are flagged with
  // a warning icon.
  const [showAssigned, setShowAssigned] = useState(false);
  const [showAssignedInfo, setShowAssignedInfo] = useState(false);
  // The reassignment row whose conflict dialog is open, or null.
  const [conflictInfoItem, setConflictInfoItem] = useState<RackPickerItem | null>(null);
  // Active fetch scope: broadened to `assignedScope` while the toggle is on so
  // ineligible racks are actually fetched (cross-site when all-sites).
  const activeScope = showAssigned ? assignedScope : scope;
  // Self-fetched building id → display label map for the Building
  // column. Falls back to "—" via the buildItem helper when an id is
  // missing (cross-site rack, or fetch in flight).
  const [buildingMap, setBuildingMap] = useState<Record<string, string>>({});

  // Ids of the ineligible (reassignment) racks currently loaded. Used to drop
  // them from the selection when the toggle goes off — either explicitly, or as
  // part of recovering from a failed broadened fetch below.
  const reassignmentIdSet = useMemo(
    () => new Set((items ?? []).filter((r) => r.reassignment).map((r) => r.id)),
    [items],
  );
  // Mirrors of the toggle + reassignment set for the fetch effect's async error
  // handler. Read via refs so the effect need not list them as deps —
  // `reassignmentIdSet` gets a fresh identity on every `items` change, which in
  // the deps array would trigger an endless refetch loop.
  const showAssignedRef = useRef(showAssigned);
  showAssignedRef.current = showAssigned;
  const reassignmentIdSetRef = useRef(reassignmentIdSet);
  reassignmentIdSetRef.current = reassignmentIdSet;

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
      // Silent on error — the Building column degrades to "—" but the
      // picker still functions.
      onError: () => {
        if (!cancelled) setBuildingMap({});
      },
    });
    return () => {
      cancelled = true;
    };
  }, [open, siteId, listBuildingsBySite]);

  // Fetch the full rack list and build picker items. Cross-site / cross-
  // building eligibility is computed per-row in buildItem so the operator
  // sees the full org-wide list with ineligible racks rendered disabled
  // (ineligible-but-visible — matches the SearchMinersModal pattern).
  // Conditional mount guarantees fresh state per open.
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    void listRacks({
      siteIds: activeScope.siteIds,
      includeUnassigned: activeScope.includeUnassigned,
      onSuccess: (racks) => {
        if (cancelled) return;
        const out: RackPickerItem[] = [];
        for (const rack of racks) {
          const item = buildRackPickerItem(rack, siteId, currentBuildingId, buildingMap);
          if (item) out.push(item);
        }
        out.sort((a, b) => a.label.localeCompare(b.label));
        setItems(out);
      },
      onError: (msg) => {
        if (cancelled) return;
        // A failed *broadened* (toggle-on) fetch must not strand the operator:
        // the full-modal error branch below hides the Switch, so they could
        // never toggle back off. Instead keep the already-loaded eligible racks,
        // revert the toggle, and surface the failure as a toast — the picker
        // stays usable with the scoped set. Only the initial scoped fetch (which
        // has nothing to fall back to) shows the blocking error state.
        if (showAssignedRef.current) {
          pushToast({ message: `Couldn't load assigned racks: ${msg}`, status: STATUSES.error });
          const ineligible = reassignmentIdSetRef.current;
          setShowAssigned(false);
          setPage(0);
          setSelectedItems((sel) => sel.filter((id) => !ineligible.has(id)));
          setConflictInfoItem(null);
          return;
        }
        setError(msg);
        setItems([]);
      },
    });
    return () => {
      cancelled = true;
    };
  }, [open, siteId, currentBuildingId, buildingMap, listRacks, activeScope.siteIds, activeScope.includeUnassigned]);

  // Toggle-off hides the ineligible (reassignment) rows entirely — the picker
  // shows only the assignable set. Toggle-on keeps them, selectable and flagged.
  const visibleItems = useMemo(() => {
    if (!items) return undefined;
    return showAssigned ? items : items.filter((r) => !r.reassignment);
  }, [items, showAssigned]);

  // With the toggle on, reassignment rows are intentionally selectable (behind
  // the reparent confirm at commit); nothing else is ever disabled.
  const isRowDisabled = useCallback((item: RackPickerItem) => item.disabled && !showAssigned, [showAssigned]);

  // Flip the toggle and reset to the first page in one go — the visible set
  // changes shape, so an out-of-range page would otherwise show empty. Turning
  // the toggle OFF also drops any selected reassignment racks (they are now
  // hidden, so leaving them selected would silently reparent them on Continue)
  // and closes any open conflict dialog. A reparent accepted earlier in the
  // session isn't a reassignment row anymore — it was committed into this
  // building on confirm — so nothing seeded is at risk here. Matches Switch's
  // setChecked signature (accepts a value or updater).
  const handleToggleShowAssigned = useCallback(
    (value: boolean | ((prev: boolean) => boolean)) => {
      const next = typeof value === "function" ? value(showAssigned) : value;
      setShowAssigned(next);
      setPage(0);
      if (!next) {
        setSelectedItems((sel) => sel.filter((id) => !reassignmentIdSet.has(id)));
        setConflictInfoItem(null);
      }
    },
    [showAssigned, reassignmentIdSet],
  );

  // Name column renders a warning icon on reassignment rows while the toggle is
  // on; tapping it opens the per-row conflict dialog. Other columns unchanged.
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

  // Client-side pagination. List doesn't paginate on its own — it consumes
  // a flat items array — so we slice here and feed a per-page view.
  const pageItems = useMemo(() => {
    if (!visibleItems) return [];
    const start = page * PAGE_SIZE;
    return visibleItems.slice(start, start + PAGE_SIZE);
  }, [visibleItems, page]);
  const totalItems = visibleItems?.length ?? 0;
  const totalPages = Math.max(1, Math.ceil(totalItems / PAGE_SIZE));
  const hasPreviousPage = page > 0;
  const hasNextPage = page < totalPages - 1;

  const handleConfirm = useCallback(() => {
    if (!items) return;
    onConfirm(computeRackSelectionDelta(items, initialSelectedRackIds, selectedItems));
  }, [items, selectedItems, initialSelectedRackIds, onConfirm]);

  const handleSelectAll = useCallback(() => {
    if (!items) return;
    // Footer "Select all" selects every eligible rack across all pages. It is
    // only offered while the "Show assigned racks" toggle is off (see the footer
    // wiring below), so no reassignment rows are on screen; the `!r.reassignment`
    // filter still guards the same-site reassignment rows the fetch loads but
    // keeps hidden. Bulk-selecting reparent rows stays possible only via the
    // in-table header checkbox, which routes them through the reparent confirm.
    setSelectedItems(items.filter((r) => !r.reassignment).map((r) => r.id));
  }, [items]);

  const handleSelectNone = useCallback(() => setSelectedItems([]), []);

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
        ) : items === undefined ? (
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
                items={pageItems}
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
                  totalItems > PAGE_SIZE ? (
                    <div className="flex flex-col items-center gap-4 py-6">
                      <span className="text-300 text-text-primary">
                        Showing {page * PAGE_SIZE + 1}–{page * PAGE_SIZE + pageItems.length} of {totalItems} racks
                      </span>
                      <div className="flex gap-3">
                        <Button
                          variant={variants.secondary}
                          size={sizes.compact}
                          ariaLabel="Previous page"
                          prefixIcon={<ChevronDown className="rotate-90" />}
                          onClick={() => setPage((p) => Math.max(0, p - 1))}
                          disabled={!hasPreviousPage}
                        />
                        <Button
                          variant={variants.secondary}
                          size={sizes.compact}
                          ariaLabel="Next page"
                          prefixIcon={<ChevronDown className="rotate-270" />}
                          onClick={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
                          disabled={!hasNextPage}
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
                // Hide "Select all" while ineligible (reassignment) racks are on
                // screen — a bulk select-all can't sweep them into a reparent.
                // Matches MinerSelectionList, which drops select-all when its
                // "Show assigned" toggle is on. The in-table header checkbox
                // still selects the whole page (reparent rows included), gated by
                // the reparent confirm on Continue.
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
