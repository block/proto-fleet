import { useCallback, useEffect, useMemo, useState } from "react";

import { buildBuildingPickerItem, type BuildingPickerItem } from "./buildingPickerItem";
import { computeBuildingSelectionDelta } from "./buildingSelectionDelta";
import { useBuildings } from "@/protoFleet/api/buildings";
import { useSites } from "@/protoFleet/api/sites";
import { ChevronDown, Plus } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import Modal, { ModalSelectAllFooter } from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";

type BuildingPickerColumn = "name" | "site" | "status";

interface ManageBuildingsModalProps {
  open: boolean;
  // Parent site context drives the eligibility split.
  siteId: bigint;
  // Building IDs currently assigned to the site. The modal seeds its selection
  // with these so the operator sees current state and can add / remove in one
  // flow, and diffs against them to gate Save.
  initialSelectedBuildingIds: bigint[];
  onDismiss: () => void;
  // Save. Receives the delta against initialSelectedBuildingIds: `added` is the
  // newly-checked buildings (id + label so the caller can render without a
  // separate lookup); `removed` is the seeded ids the operator unchecked.
  // Untouched buildings are in neither list — the caller leaves them as-is.
  // This modal owns site membership, so the caller persists the delta here
  // rather than staging it for a later save.
  onConfirm: (delta: { added: { buildingId: bigint; label: string }[]; removed: bigint[] }) => Promise<void> | void;
  // In-flight signal from the caller's write, mirrored into the CTA.
  saving?: boolean;
  // Renders a "New building" button beside Save that hands off to the full
  // building-create flow instead of picking an existing building — mirroring
  // ParentPickerModal's createNewLaunch affordance ("New rack"). Leaving this
  // way abandons the pending selection: Save is what commits it, so nothing
  // was written. Omitted = no create affordance.
  onCreateNewLaunch?: () => void;
}

const PAGE_SIZE = 25;

const colTitles: ColTitles<BuildingPickerColumn> = {
  name: "Name",
  site: "Site",
  status: "Status",
};

const colConfig: ColConfig<BuildingPickerItem, string, BuildingPickerColumn> = {
  name: {
    component: (item) => <span>{item.label || "(unnamed building)"}</span>,
    width: "min-w-32",
  },
  site: {
    component: (item) => <span>{item.siteLabel}</span>,
    width: "min-w-32",
  },
  status: {
    component: (item) => <span>{item.statusLabel}</span>,
    width: "min-w-32",
  },
};

const activeCols: BuildingPickerColumn[] = ["name", "site", "status"];

const ManageBuildingsModal = ({
  open,
  siteId,
  initialSelectedBuildingIds,
  onDismiss,
  onConfirm,
  saving = false,
  onCreateNewLaunch,
}: ManageBuildingsModalProps) => {
  const { listAllBuildings } = useBuildings();
  const { listSites } = useSites();
  const [items, setItems] = useState<BuildingPickerItem[] | undefined>(undefined);
  const [error, setError] = useState<string | null>(null);
  const [selectedItems, setSelectedItems] = useState<string[]>(() =>
    initialSelectedBuildingIds.map((id) => id.toString()),
  );
  const [page, setPage] = useState(0);
  // Self-fetched site id → display label map for the Site column. Falls
  // back to "—" via buildBuildingPickerItem when an id is missing.
  const [siteMap, setSiteMap] = useState<Record<string, string>>({});

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    void listSites({
      onSuccess: (rows) => {
        if (cancelled) return;
        const out: Record<string, string> = {};
        for (const row of rows) {
          const s = row.site;
          if (s) out[s.id.toString()] = s.name;
        }
        setSiteMap(out);
      },
      // Silent on error — the Site column degrades to "—" but the picker
      // still functions.
      onError: () => {
        if (!cancelled) setSiteMap({});
      },
    });
    return () => {
      cancelled = true;
    };
  }, [open, listSites]);

  // Fetch the full building list and build picker items. Cross-site
  // eligibility is computed per-row in buildBuildingPickerItem so the
  // operator sees the org-wide list with ineligible buildings rendered
  // disabled. Conditional mount guarantees fresh state per open.
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    void listAllBuildings({
      onSuccess: (rows) => {
        if (cancelled) return;
        const out: BuildingPickerItem[] = [];
        for (const row of rows) {
          const item = buildBuildingPickerItem(row, siteId, siteMap);
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
  }, [open, siteId, siteMap, listAllBuildings]);

  const isRowDisabled = useCallback((item: BuildingPickerItem) => item.disabled, []);

  // Client-side pagination — List consumes a flat array, so we slice here.
  const pageItems = useMemo(() => {
    if (!items) return [];
    const start = page * PAGE_SIZE;
    return items.slice(start, start + PAGE_SIZE);
  }, [items, page]);
  const totalItems = items?.length ?? 0;
  const totalPages = Math.max(1, Math.ceil(totalItems / PAGE_SIZE));
  const hasPreviousPage = page > 0;
  const hasNextPage = page < totalPages - 1;

  // The exact membership change Save would write. Derived here so the CTA's
  // dirty gate and the write read the same delta — note it isn't a plain
  // set-difference: seeded ids the picker's response omitted, and rows that
  // became ineligible, are excluded on purpose (see computeBuildingSelectionDelta),
  // so a raw selection comparison would call the modal dirty when there's
  // nothing to send.
  const delta = useMemo(
    () => (items ? computeBuildingSelectionDelta(items, initialSelectedBuildingIds, selectedItems) : null),
    [items, initialSelectedBuildingIds, selectedItems],
  );
  const isDirty = !!delta && (delta.added.length > 0 || delta.removed.length > 0);

  const handleConfirm = useCallback(() => {
    if (!delta) return;
    void onConfirm(delta);
  }, [delta, onConfirm]);

  const handleSelectAll = useCallback(() => {
    if (!items) return;
    // Select-all promotes the *eligible* set (excluding disabled rows).
    setSelectedItems(items.filter((b) => !b.disabled).map((b) => b.id));
  }, [items]);

  const handleSelectNone = useCallback(() => setSelectedItems([]), []);

  return (
    <Modal
      open={open}
      title="Select buildings"
      size="large"
      className="flex !h-[calc(100dvh-(--spacing(32)))] max-h-[calc(100dvh-(--spacing(32)))] flex-col !overflow-hidden"
      bodyClassName="flex flex-1 min-h-0 flex-col"
      onDismiss={saving ? undefined : onDismiss}
      divider={false}
      testId="manage-buildings-modal"
      buttons={[
        // ButtonGroup sorts the primary button last, so this lands to the left
        // of Save.
        ...(onCreateNewLaunch
          ? [
              {
                text: "New building",
                variant: variants.secondary,
                prefixIcon: <Plus />,
                onClick: onCreateNewLaunch,
                disabled: saving,
                dismissModalOnClick: false,
                testId: "manage-buildings-modal-create-new",
              },
            ]
          : []),
        {
          // "Save" because this is where membership is written
          // (AssignBuildingsToSite), not a step on the way to a later commit.
          text: saving ? "Saving…" : "Save",
          variant: "primary",
          onClick: handleConfirm,
          // Dirty-gated, and blocked while `items` is undefined — the delta
          // can't be computed without the list, so every selection would read
          // clean for the wrong reason.
          disabled: saving || !isDirty,
          dismissModalOnClick: false,
          testId: "manage-buildings-modal-confirm",
        },
      ]}
    >
      <div className="flex h-full min-h-0 flex-col">
        {error ? (
          <div className="py-6 text-300 text-intent-critical-fill" data-testid="manage-buildings-modal-error">
            {error}
          </div>
        ) : items === undefined ? (
          <div className="flex flex-1 items-center justify-center py-12">
            <ProgressCircular indeterminate />
          </div>
        ) : (
          <>
            <div className="min-h-0 flex-1 overflow-y-auto">
              <List<BuildingPickerItem, string, BuildingPickerColumn>
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
                itemName={{ singular: "building", plural: "buildings" }}
                hideTotal
                containerClassName="min-h-0"
                tableClassName="mb-0"
                overflowContainer
                stickyBgColor="bg-surface-elevated-base"
                footerContent={
                  totalItems > PAGE_SIZE ? (
                    <div className="flex flex-col items-center gap-4 py-6">
                      <span className="text-300 text-text-primary">
                        Showing {page * PAGE_SIZE + 1}–{page * PAGE_SIZE + pageItems.length} of {totalItems} buildings
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
                label={`${selectedItems.length} ${selectedItems.length === 1 ? "building" : "buildings"} selected`}
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

export default ManageBuildingsModal;
