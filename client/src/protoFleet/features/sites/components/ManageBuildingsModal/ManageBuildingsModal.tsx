import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { useSiteContainers } from "../../hooks/useSiteContainers";
import {
  buildBuildingPickerItem,
  buildContainerPickerItem,
  type BuildingPickerItem,
  containerPickerId,
} from "./buildingPickerItem";
import { computeBuildingSelectionDelta, computeSelectionDelta } from "./buildingSelectionDelta";
import { useBuildings } from "@/protoFleet/api/buildings";
import { useSites } from "@/protoFleet/api/sites";
import { Alert, ChevronDown } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
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
  // Hands off to the full building-create flow instead of picking an existing
  // building. Leaving this way abandons the pending selection: Save is what
  // commits it, so nothing was written. Omitted = no create affordance.
  //
  // Two callbacks rather than one with a variant argument, because a host that
  // hasn't wired the batch RPC can't offer the second: presence of each prop is
  // what decides whether its button renders.
  onCreateNewLaunch?: () => void;
  onCreateMultipleLaunch?: () => void;
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
  saving: parentSaving = false,
  onCreateNewLaunch,
  onCreateMultipleLaunch,
}: ManageBuildingsModalProps) => {
  const {
    containers,
    loadError: containerLoadError,
    retry: retryContainers,
    error: containerError,
    saving: containerSaving,
    assign,
  } = useSiteContainers(open);
  const saving = parentSaving || containerSaving;
  const committing = useRef(false);
  const seededContainers = useRef(false);
  const { listAllBuildings } = useBuildings();
  const { listSites } = useSites();
  const [buildingItems, setBuildingItems] = useState<BuildingPickerItem[] | undefined>(undefined);
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
        setBuildingItems(out);
      },
      onError: (msg) => {
        if (cancelled) return;
        setError(msg);
        setBuildingItems([]);
      },
    });
    return () => {
      cancelled = true;
    };
  }, [open, siteId, siteMap, listAllBuildings]);

  const currentContainerIds = useMemo(
    () =>
      containers
        ?.filter((container) => container.site?.id === siteId)
        .map((container) => containerPickerId(container.deviceIdentifier)),
    [containers, siteId],
  );
  useEffect(() => {
    if (!currentContainerIds || seededContainers.current) return;
    seededContainers.current = true;
    setSelectedItems((current) => [...current, ...currentContainerIds]);
  }, [currentContainerIds]);

  const containerItems = useMemo(
    () => containers?.map((container) => buildContainerPickerItem(container, siteId, siteMap)),
    [containers, siteId, siteMap],
  );
  const items = useMemo(
    () =>
      buildingItems && containerItems
        ? [...buildingItems, ...containerItems].sort((a, b) => a.label.localeCompare(b.label))
        : undefined,
    [buildingItems, containerItems],
  );
  const containerDelta = useMemo(() => {
    if (!containerItems || !currentContainerIds) return null;
    return computeSelectionDelta(containerItems, currentContainerIds, selectedItems);
  }, [containerItems, currentContainerIds, selectedItems]);

  const isRowDisabled = useCallback((item: BuildingPickerItem) => saving || item.disabled, [saving]);

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

  // The exact membership change Save would write — note it isn't a plain
  // set-difference: seeded ids the picker's response omitted, and rows that
  // became ineligible, are excluded on purpose (see
  // computeBuildingSelectionDelta), so a raw selection comparison would read as
  // a change when there's nothing to send.
  const buildingDelta = useMemo(() => {
    if (!items || !buildingItems) return null;
    return computeBuildingSelectionDelta(buildingItems, initialSelectedBuildingIds, selectedItems);
  }, [items, buildingItems, initialSelectedBuildingIds, selectedItems]);
  const handleConfirm = useCallback(async () => {
    if (!buildingDelta || !containerDelta || committing.current) return;
    committing.current = true;
    try {
      // Each successful batch updates the baseline, so a later failure can be
      // retried without replaying container changes that already succeeded.
      const toContainerIdentifiers = (ids: string[]) => ids.map((id) => id.slice("container:".length));
      if (containerDelta.added.length > 0) {
        const added = toContainerIdentifiers(containerDelta.added.map((item) => item.id));
        const assigned = await assign(added, siteId);
        if (!assigned) return;
      }
      if (containerDelta.removed.length > 0) {
        const removed = toContainerIdentifiers(containerDelta.removed);
        const unassigned = await assign(removed);
        if (!unassigned) return;
      }
      if (buildingDelta.added.length === 0 && buildingDelta.removed.length === 0) {
        onDismiss();
        return;
      }
      await onConfirm(buildingDelta);
    } finally {
      committing.current = false;
    }
  }, [buildingDelta, containerDelta, assign, siteId, onConfirm, onDismiss]);

  const handleSelectAll = useCallback(() => {
    if (!items || saving) return;
    // Select-all promotes the *eligible* set (excluding disabled rows).
    setSelectedItems(items.filter((b) => !b.disabled).map((b) => b.id));
  }, [items, saving]);

  const handleSelectNone = useCallback(() => {
    if (!saving) setSelectedItems([]);
  }, [saving]);

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
        // ButtonGroup sorts the primary button last, so these land to the left
        // of Save. Named for what they open rather than a generic "New
        // building" — each preselects its side of the create modal's Single /
        // Multiple toggle, so the label is the whole affordance.
        ...(onCreateNewLaunch
          ? [
              {
                text: "Create building",
                variant: variants.secondary,
                onClick: onCreateNewLaunch,
                disabled: saving,
                dismissModalOnClick: false,
                testId: "manage-buildings-modal-create-new",
              },
            ]
          : []),
        ...(onCreateMultipleLaunch
          ? [
              {
                text: "Create multiple buildings",
                variant: variants.secondary,
                onClick: onCreateMultipleLaunch,
                disabled: saving,
                dismissModalOnClick: false,
                testId: "manage-buildings-modal-create-multiple",
              },
            ]
          : []),
        {
          // "Save" because this is where membership is written
          // (AssignBuildingsToSite), not a step on the way to a later commit.
          text: saving ? "Saving…" : "Save",
          variant: "primary",
          onClick: handleConfirm,
          // Only the in-flight guard, plus not-yet-loaded: without the list
          // there's no delta to compute, so a click could only misread the
          // selection. The no-change case closes in handleConfirm instead.
          disabled: saving || buildingDelta === null,
          dismissModalOnClick: false,
          testId: "manage-buildings-modal-confirm",
        },
      ]}
    >
      <div className="flex h-full min-h-0 flex-col">
        {containerLoadError || containerError ? (
          <div role="alert">
            <Callout
              intent="danger"
              prefixIcon={<Alert />}
              title={containerLoadError ? "Couldn't load containers" : "Couldn't update buildings"}
              subtitle={containerLoadError ?? containerError}
              buttonText={containerLoadError ? "Retry" : undefined}
              buttonOnClick={retryContainers}
            />
          </div>
        ) : null}
        {error ? (
          <div className="py-6 text-300 text-intent-critical-fill" data-testid="manage-buildings-modal-error">
            {error}
          </div>
        ) : items === undefined ? (
          containerLoadError ? null : (
            <div className="flex flex-1 items-center justify-center py-12">
              <ProgressCircular indeterminate dataTestId="manage-buildings-modal-loading" />
            </div>
          )
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
