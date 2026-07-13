import { useCallback, useEffect, useMemo, useState } from "react";

import { buildRackPickerItem, type RackPickerItem } from "../rackPickerItem";
import { computeRackSelectionDelta } from "./rackSelectionDelta";
import { useBuildings } from "@/protoFleet/api/buildings";
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
  onConfirm: (delta: { added: { rackId: bigint; label: string }[]; removed: bigint[] }) => void;
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
  // Self-fetched building id → display label map for the Building
  // column. Falls back to "—" via the buildItem helper when an id is
  // missing (cross-site rack, or fetch in flight).
  const [buildingMap, setBuildingMap] = useState<Record<string, string>>({});

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
    onConfirm(computeRackSelectionDelta(items, initialSelectedRackIds, selectedItems));
  }, [items, selectedItems, initialSelectedRackIds, onConfirm]);

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
