import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { type ReparentedRack } from "../ManageBuildingModal/RackReparentWarningDialog";
import { buildRackPickerItem, describeRackReassignment, type RackPickerItem } from "../rackPickerItem";
import { useBuildings } from "@/protoFleet/api/buildings";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import { type SiteFilterFields } from "@/protoFleet/components/PageHeader/SitePicker";
import { Alert, Info } from "@/shared/assets/icons";
import Button, { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Switch from "@/shared/components/Switch";
import { pushToast, STATUSES } from "@/shared/features/toaster";

type SearchRackColumn = "name" | "building" | "status";

interface SearchRacksModalProps {
  open: boolean;
  // Parent building context — same eligibility rules as ManageRacksModal:
  // racks in this building or unassigned (with matching site, or no site)
  // are eligible; racks in another building or another site are visible
  // but disabled.
  siteId: bigint;
  currentBuildingId: bigint;
  // Rack-fetch scope, derived from the building's own site (see
  // buildingRackScope) — NOT the header SitePicker. Governs which racks are
  // *fetched*: the building's site + site-unassigned. There is no "all sites"
  // fallback; the fetch is always scoped. Per-row eligibility is still
  // computed against `siteId`.
  scope: SiteFilterFields;
  // Broadened fetch scope used while "Show assigned racks" is ON, so
  // already-placed racks surface for reparenting. See assignedRackScope.
  assignedScope: SiteFilterFields;
  // Racks already in the building's working set. A rack in the working set
  // belongs to THIS building in the operator's draft — including a reparent
  // staged this session but not yet Saved, whose server row still reports its
  // old placement. Passed to buildRackPickerItem so it classifies as
  // "in this building" here too, matching ManageRacksModal — otherwise the same
  // rack would show as a spurious reparent row in this picker.
  assignedRackIds: bigint[];
  buildingName: string;
  onDismiss: () => void;
  // Returns a single chosen rack so the caller can add it to the working set
  // and assign it to the cell that was selected when the popover opened. When
  // the chosen rack is currently placed elsewhere, `reparent` carries the
  // reassignment info so the caller can gate the reparent confirm.
  onConfirm: (rackId: bigint, label: string, reparent?: ReparentedRack) => void;
}

const colTitles: ColTitles<SearchRackColumn> = {
  name: "Name",
  building: "Building",
  status: "Status",
};

const colConfig: ColConfig<RackPickerItem, string, SearchRackColumn> = {
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

const activeCols: SearchRackColumn[] = ["name", "building", "status"];

// Single-rack picker with a name filter — mirrors SearchMinersModal in the
// rack-management feature. Picked rack is added to the building's working
// set and assigned to whatever cell was selected when the popover opened.
const SearchRacksModal = ({
  open,
  siteId,
  currentBuildingId,
  scope,
  assignedScope,
  assignedRackIds,
  buildingName,
  onDismiss,
  onConfirm,
}: SearchRacksModalProps) => {
  const { listRacks } = useDeviceSets();
  const { listBuildingsBySite } = useBuildings();
  // See prop doc: seed working-set racks as in-this-building for classification.
  const seededRackIds = useMemo(() => new Set(assignedRackIds.map((id) => id.toString())), [assignedRackIds]);
  const [items, setItems] = useState<RackPickerItem[] | undefined>(undefined);
  const [error, setError] = useState<string | null>(null);
  const [selectedItems, setSelectedItems] = useState<string[]>([]);
  const [query, setQuery] = useState("");
  // "Show assigned racks" — default off (assignable set only). On surfaces
  // already-placed racks for reparenting, flagged and gated by a confirm.
  const [showAssigned, setShowAssigned] = useState(false);
  const [showAssignedInfo, setShowAssignedInfo] = useState(false);
  const [conflictInfoItem, setConflictInfoItem] = useState<RackPickerItem | null>(null);
  const activeScope = showAssigned ? assignedScope : scope;
  // Self-fetched building id → display label map for the Building
  // column. Mirrors ManageRacksModal so the two pickers stay aligned.
  const [buildingMap, setBuildingMap] = useState<Record<string, string>>({});
  // Mirror of the toggle for the fetch effect's async error handler, read via a
  // ref so the effect need not list `showAssigned` as a dep.
  const showAssignedRef = useRef(showAssigned);
  showAssignedRef.current = showAssigned;

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
          const item = buildRackPickerItem(rack, siteId, currentBuildingId, buildingMap, seededRackIds);
          if (item) out.push(item);
        }
        out.sort((a, b) => a.label.localeCompare(b.label));
        setItems(out);
      },
      onError: (msg) => {
        if (cancelled) return;
        // A failed *broadened* (toggle-on) fetch must not strand the operator:
        // the error branch below hides the Switch, so they could never toggle
        // back off. Keep the already-loaded scoped racks, revert the toggle and
        // clear the (now-hidden) selection, and surface the failure as a toast.
        // Only the initial scoped fetch shows the blocking error state.
        if (showAssignedRef.current) {
          pushToast({ message: `Couldn't load assigned racks: ${msg}`, status: STATUSES.error });
          setShowAssigned(false);
          setSelectedItems([]);
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
  }, [
    open,
    siteId,
    currentBuildingId,
    buildingMap,
    listRacks,
    activeScope.siteIds,
    activeScope.includeUnassigned,
    seededRackIds,
  ]);

  // Reparent rows are selectable only while the toggle is on; otherwise nothing
  // is disabled.
  const isRowDisabled = useCallback((item: RackPickerItem) => item.disabled && !showAssigned, [showAssigned]);

  // Turning the toggle OFF clears the selection and any open conflict dialog: a
  // reparent row selected while the toggle was on would otherwise stay selected
  // but hidden, leaving Assign enabled yet a silent no-op (handleConfirm blocks
  // a hidden ineligible pick). Clearing keeps the UI honest.
  const handleToggleShowAssigned = useCallback(
    (value: boolean | ((prev: boolean) => boolean)) => {
      const next = typeof value === "function" ? value(showAssigned) : value;
      setShowAssigned(next);
      if (!next) {
        setSelectedItems([]);
        setConflictInfoItem(null);
      }
    },
    [showAssigned],
  );

  // Client-side filter on the rack label. Case-insensitive substring match —
  // matches the SearchMinersModal feel without bringing in a fuzzy lib. Toggle
  // off also hides the ineligible (reparent) rows entirely.
  const filteredItems = useMemo(() => {
    if (!items) return [];
    const base = showAssigned ? items : items.filter((i) => !i.reassignment);
    const q = query.trim().toLowerCase();
    if (!q) return base;
    return base.filter((i) => i.label.toLowerCase().includes(q));
  }, [items, query, showAssigned]);

  // Name column shows a warning icon on reparent rows while the toggle is on.
  const listColConfig = useMemo<ColConfig<RackPickerItem, string, SearchRackColumn>>(() => {
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

  // The selected rack, only if it is currently visible. A rack hidden by the
  // search query (or the toggle) must not be assignable — mirrors the miner
  // side, which preserves the selection but blocks confirm when a filter hides
  // it (blockedByFilter). Drives both the Assign enablement and handleConfirm.
  const selectedVisibleItem = useMemo(
    () => (selectedItems.length === 0 ? undefined : filteredItems.find((r) => r.id === selectedItems[0])),
    [filteredItems, selectedItems],
  );

  const handleConfirm = useCallback(() => {
    if (!selectedVisibleItem) return;
    const { id, label, reassignment, minerCount } = selectedVisibleItem;
    const reparent: ReparentedRack | undefined = reassignment ? { rackId: BigInt(id), label, minerCount } : undefined;
    onConfirm(BigInt(id), label, reparent);
  }, [selectedVisibleItem, onConfirm]);

  return (
    <Modal
      open={open}
      title="Search racks"
      size="large"
      className="flex !h-[calc(100dvh-(--spacing(32)))] max-h-[calc(100dvh-(--spacing(32)))] flex-col !overflow-hidden"
      bodyClassName="flex flex-1 min-h-0 flex-col overflow-hidden"
      onDismiss={onDismiss}
      divider={false}
      testId="search-racks-modal"
      buttons={[
        {
          text: "Assign",
          variant: "primary",
          disabled: !selectedVisibleItem,
          onClick: handleConfirm,
          dismissModalOnClick: false,
          testId: "search-racks-modal-confirm",
        },
      ]}
    >
      <div className="flex h-full min-h-0 flex-col gap-4">
        <Input
          id="search-racks-query"
          label="Search by name"
          initValue={query}
          onChange={(value) => setQuery(value)}
          testId="search-racks-modal-query"
        />
        {error ? (
          <div className="py-6 text-300 text-intent-critical-fill" data-testid="search-racks-modal-error">
            {error}
          </div>
        ) : items === undefined ? (
          <div className="flex flex-1 items-center justify-center py-12">
            <ProgressCircular indeterminate />
          </div>
        ) : (
          <div className="min-h-0 flex-1 overflow-y-auto">
            <List<RackPickerItem, string, SearchRackColumn>
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
              items={filteredItems}
              itemKey="id"
              itemSelectable
              // Radio mode enforces single-select and renders no header
              // select-all, so bulk gestures can't batch in a reparent rack —
              // matches the miner-side SearchMinersModal. (A hidden selection is
              // handled separately via selectedVisibleItem, since selecting the
              // lone visible rack can promote List into "all" mode, which skips
              // its own filter cleanup.)
              selectionType="radio"
              customSelectedItems={selectedItems}
              customSetSelectedItems={setSelectedItems}
              isRowDisabled={isRowDisabled}
              itemName={{ singular: "rack", plural: "racks" }}
              hideTotal
              containerClassName="min-h-0"
              overflowContainer
              stickyBgColor="bg-surface-elevated-base"
            />
          </div>
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

export default SearchRacksModal;
