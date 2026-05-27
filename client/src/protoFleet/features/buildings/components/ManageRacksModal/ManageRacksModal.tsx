import { useCallback, useEffect, useMemo, useState } from "react";

import { type DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import { ChevronDown } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import Modal, { ModalSelectAllFooter } from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";

type RackPickerColumn = "name" | "building" | "status";

interface ManageRacksModalProps {
  open: boolean;
  // Parent building context drives the eligibility split.
  siteId: bigint;
  currentBuildingId: bigint;
  // Building id → display label lookup for the "Building" column. The
  // parent already fetches the site's building list for the manage modal,
  // so it threads the map down rather than re-fetching.
  buildingLabels?: Record<string, string>;
  // Rack IDs currently in the building's working set. The modal seeds its
  // selection with these so the operator sees the current state and can
  // add / remove in one flow.
  initialSelectedRackIds: bigint[];
  onDismiss: () => void;
  // Returns the final selection as (rackId, label) tuples so the parent
  // can render new entries in the left pane without a label lookup.
  onConfirm: (selections: { rackId: bigint; label: string }[]) => void;
}

interface RackPickerItem {
  id: string;
  label: string;
  buildingLabel: string;
  statusLabel: string;
  disabled: boolean;
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

const buildItem = (
  rack: DeviceSet,
  currentSiteId: bigint,
  currentBuildingId: bigint,
  buildingLabels: Record<string, string>,
): RackPickerItem | null => {
  if (rack.typeDetails.case !== "rackInfo") return null;
  const info = rack.typeDetails.value;
  const buildingId = info.buildingId;
  const siteId = info.siteId;
  const inOtherBuilding = buildingId !== undefined && buildingId !== 0n && buildingId !== currentBuildingId;
  const inThisBuilding = buildingId === currentBuildingId;
  // Racks under a *different* site are ineligible because moving them
  // across sites is a separate operator decision; manage-racks should
  // only add racks that already share this building's site or are
  // unassigned entirely.
  const inOtherSite = !inThisBuilding && siteId !== undefined && siteId !== 0n && siteId !== currentSiteId;
  // ineligible-but-visible: racks in another building or another site
  // render disabled so the operator sees why they can't be added.
  const disabled = inOtherBuilding || inOtherSite;
  const statusLabel = inOtherBuilding
    ? "In another building"
    : inOtherSite
      ? "In another site"
      : inThisBuilding
        ? "In this building"
        : "Unassigned";
  const buildingLabel =
    buildingId === undefined || buildingId === 0n ? "—" : (buildingLabels[buildingId.toString()] ?? "—");
  return { id: rack.id.toString(), label: rack.label, buildingLabel, statusLabel, disabled };
};

const ManageRacksModal = ({
  open,
  siteId,
  currentBuildingId,
  buildingLabels,
  initialSelectedRackIds,
  onDismiss,
  onConfirm,
}: ManageRacksModalProps) => {
  const { listRacks } = useDeviceSets();
  const [items, setItems] = useState<RackPickerItem[] | undefined>(undefined);
  const [error, setError] = useState<string | null>(null);
  const [selectedItems, setSelectedItems] = useState<string[]>(() => initialSelectedRackIds.map((id) => id.toString()));
  const [page, setPage] = useState(0);

  const buildingMap = useMemo(() => buildingLabels ?? {}, [buildingLabels]);

  // Fetch the full rack list and build picker items. Cross-site / cross-
  // building eligibility is computed per-row in buildItem so the operator
  // sees the full org-wide list with ineligible racks rendered disabled
  // (ineligible-but-visible — matches the SearchMinersModal pattern).
  // Conditional mount guarantees fresh state per open.
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    void listRacks({
      onSuccess: (racks) => {
        if (cancelled) return;
        const out: RackPickerItem[] = [];
        for (const rack of racks) {
          const item = buildItem(rack, siteId, currentBuildingId, buildingMap);
          if (item) out.push(item);
        }
        out.sort((a, b) => a.label.localeCompare(b.label));
        setItems(out);
      },
      onError: (msg) => {
        if (cancelled) return;
        setError(msg);
        setItems([]);
      },
    });
    return () => {
      cancelled = true;
    };
  }, [open, siteId, currentBuildingId, buildingMap, listRacks]);

  const isRowDisabled = useCallback((item: RackPickerItem) => item.disabled, []);

  // Client-side pagination. List doesn't paginate on its own — it consumes
  // a flat items array — so we slice here and feed a per-page view.
  const pageItems = useMemo(() => {
    if (!items) return [];
    const start = page * PAGE_SIZE;
    return items.slice(start, start + PAGE_SIZE);
  }, [items, page]);
  const totalItems = items?.length ?? 0;
  const totalPages = Math.max(1, Math.ceil(totalItems / PAGE_SIZE));
  const hasPreviousPage = page > 0;
  const hasNextPage = page < totalPages - 1;

  const handleConfirm = useCallback(() => {
    if (!items) return;
    // Resolve labels for every selected id by indexing into the full items
    // list (not just the current page, since selections persist across
    // pages via List's preserveOffPageSelection behavior).
    const selections: { rackId: bigint; label: string }[] = [];
    for (const id of selectedItems) {
      const item = items.find((r) => r.id === id);
      if (!item) continue;
      // Defensive: never confirm a disabled item even if it slipped into
      // the selection via initialSelectedRackIds. The server would reject
      // anyway, but failing earlier surfaces a clearer state.
      if (item.disabled) continue;
      selections.push({ rackId: BigInt(id), label: item.label });
    }
    onConfirm(selections);
  }, [items, selectedItems, onConfirm]);

  const handleSelectAll = useCallback(() => {
    if (!items) return;
    // Select-all promotes the *eligible* set (excluding disabled rows) to
    // the selection — matches MinerSelectionList's footer behavior.
    setSelectedItems(items.filter((r) => !r.disabled).map((r) => r.id));
  }, [items]);

  const handleSelectNone = useCallback(() => setSelectedItems([]), []);

  return (
    <Modal
      open={open}
      title="Select racks"
      size="large"
      className="flex !h-[calc(100vh-(--spacing(32)))] max-h-[calc(100vh-(--spacing(32)))] flex-col !overflow-hidden"
      bodyClassName="flex flex-1 min-h-0 flex-col overflow-hidden"
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
                colConfig={colConfig}
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
                onSelectAll={handleSelectAll}
                onSelectNone={handleSelectNone}
              />
            </div>
          </>
        )}
      </div>
    </Modal>
  );
};

export default ManageRacksModal;
