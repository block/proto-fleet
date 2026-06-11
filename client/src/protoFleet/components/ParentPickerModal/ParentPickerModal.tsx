import { type ChangeEvent, useCallback, useEffect, useRef, useState } from "react";

import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { useSites } from "@/protoFleet/api/sites";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import { Alert, ArrowLeftCompact, ArrowRight } from "@/shared/assets/icons";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Radio from "@/shared/components/Radio";

const INACTIVE_PLACEHOLDER = "—";
const PAGE_SIZE = 50;

export type PickerKind = "site" | "building" | "rack" | "group";

type PickerItem = {
  id: string;
  label: string;
  hint: string;
};

interface ParentPickerModalProps {
  kind: PickerKind;
  show: boolean;
  selectionMode: "single" | "multi";
  // Pre-selects this row in single-select; Save disabled while it's
  // the only selection (no-op re-parent). Ignored in multi-select.
  currentParentId?: bigint;
  sourceLabel: string;
  description?: string;
  createNewLabel?: string;
  onCreateNew?: (name: string) => Promise<void>;
  onDismiss: () => void;
  onConfirm: (targetIds: bigint[]) => void | Promise<void>;
}

// ListSites + ListBuildings RPCs return everything; only racks + groups
// support pageSize/pageToken.
const IS_PAGINATED: Record<PickerKind, boolean> = {
  site: false,
  building: false,
  rack: true,
  group: true,
};

const ParentPickerModal = ({
  kind,
  show,
  selectionMode,
  currentParentId,
  sourceLabel,
  description,
  createNewLabel,
  onCreateNew,
  onDismiss,
  onConfirm,
}: ParentPickerModalProps) => {
  const { listSites } = useSites();
  const { listAllBuildings } = useBuildings();
  const { listRacks, listGroups } = useDeviceSets();

  const [pages, setPages] = useState<PickerItem[][]>([]);
  // pageTokens[N] fetches page N; index 0 is always "".
  const [pageTokens, setPageTokens] = useState<string[]>([""]);
  const [currentPage, setCurrentPage] = useState(0);
  const [totalCount, setTotalCount] = useState(0);
  const [pageLoading, setPageLoading] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [saving, setSaving] = useState(false);
  const [createNewChecked, setCreateNewChecked] = useState(false);
  const [newName, setNewName] = useState("");

  const currentParentKey =
    selectionMode === "single" && currentParentId !== undefined ? currentParentId.toString() : null;

  // Name lookups loaded once per open, reused across paginated fetches.
  const buildingsRef = useRef(new Map<string, string>()).current;
  const sitesRef = useRef(new Map<string, string>()).current;

  const fetchSitesPage = useCallback(async (): Promise<PickerItem[]> => {
    return await new Promise<PickerItem[]>((resolve, reject) => {
      void listSites({
        onSuccess: (sites: SiteWithCounts[]) => {
          const rows = sites
            .filter((s) => !!s.site)
            .map<PickerItem>((s) => ({
              id: s.site!.id.toString(),
              label: s.site!.name,
              hint: `${s.deviceCount.toString()} miners`,
            }))
            .sort((a, b) => a.label.localeCompare(b.label));
          resolve(rows);
        },
        onError: (msg) => reject(new Error(msg)),
      });
    });
  }, [listSites]);

  const fetchBuildingsPage = useCallback(async (): Promise<PickerItem[]> => {
    const [sites, buildings] = await Promise.all([
      new Promise<SiteWithCounts[]>((resolve, reject) => {
        void listSites({ onSuccess: resolve, onError: (msg) => reject(new Error(msg)) });
      }),
      new Promise<BuildingWithCounts[]>((resolve, reject) => {
        void listAllBuildings({ onSuccess: resolve, onError: (msg) => reject(new Error(msg)) });
      }),
    ]);
    sitesRef.clear();
    for (const s of sites) if (s.site) sitesRef.set(s.site.id.toString(), s.site.name);
    return buildings
      .filter((b) => !!b.building)
      .map<PickerItem>((b) => {
        const sid = b.building!.siteId?.toString();
        return {
          id: b.building!.id.toString(),
          label: b.building!.name,
          hint: sid ? (sitesRef.get(sid) ?? INACTIVE_PLACEHOLDER) : INACTIVE_PLACEHOLDER,
        };
      })
      .sort((a, b) => a.label.localeCompare(b.label));
  }, [listSites, listAllBuildings, sitesRef]);

  const ensureBuildingNames = useCallback(async () => {
    if (buildingsRef.size > 0) return;
    const buildings = await new Promise<BuildingWithCounts[]>((resolve, reject) => {
      void listAllBuildings({ onSuccess: resolve, onError: (msg) => reject(new Error(msg)) });
    });
    for (const b of buildings) {
      if (b.building) buildingsRef.set(b.building.id.toString(), b.building.name);
    }
  }, [listAllBuildings, buildingsRef]);

  const fetchRackPage = useCallback(
    async (token: string) => {
      await ensureBuildingNames();
      return await new Promise<{ items: PickerItem[]; nextPageToken: string; totalCount: number }>(
        (resolve, reject) => {
          void listRacks({
            pageSize: PAGE_SIZE,
            pageToken: token,
            onSuccess: (deviceSets, nextPageToken, total) => {
              const items: PickerItem[] = deviceSets.map((rack) => {
                const rackInfo = rack.typeDetails.case === "rackInfo" ? rack.typeDetails.value : undefined;
                const bid = rackInfo?.buildingId?.toString();
                return {
                  id: rack.id.toString(),
                  label: rack.label || INACTIVE_PLACEHOLDER,
                  hint: bid ? (buildingsRef.get(bid) ?? INACTIVE_PLACEHOLDER) : INACTIVE_PLACEHOLDER,
                };
              });
              resolve({ items, nextPageToken, totalCount: total });
            },
            onError: (msg) => reject(new Error(msg)),
          });
        },
      );
    },
    [listRacks, ensureBuildingNames, buildingsRef],
  );

  const fetchGroupPage = useCallback(
    async (token: string) => {
      return await new Promise<{ items: PickerItem[]; nextPageToken: string; totalCount: number }>(
        (resolve, reject) => {
          void listGroups({
            pageSize: PAGE_SIZE,
            pageToken: token,
            onSuccess: (deviceSets, nextPageToken, total) => {
              const items: PickerItem[] = deviceSets.map((set) => ({
                id: set.id.toString(),
                label: set.label,
                hint: `${set.deviceCount} miners`,
              }));
              resolve({ items, nextPageToken, totalCount: total });
            },
            onError: (msg) => reject(new Error(msg)),
          });
        },
      );
    },
    [listGroups],
  );

  useEffect(() => {
    if (!show) return;
    let cancelled = false;
    queueMicrotask(() => {
      setPages([]);
      setPageTokens([""]);
      setCurrentPage(0);
      setTotalCount(0);
      setLoadError(null);
      setSelectedIds(currentParentKey ? new Set([currentParentKey]) : new Set());
      setCreateNewChecked(false);
      setNewName("");
      setSaving(false);
      setPageLoading(true);
      buildingsRef.clear();
      sitesRef.clear();
    });
    void (async () => {
      try {
        if (kind === "site") {
          const rows = await fetchSitesPage();
          if (cancelled) return;
          setPages([rows]);
          setTotalCount(rows.length);
        } else if (kind === "building") {
          const rows = await fetchBuildingsPage();
          if (cancelled) return;
          setPages([rows]);
          setTotalCount(rows.length);
        } else if (kind === "rack") {
          const { items, nextPageToken, totalCount } = await fetchRackPage("");
          if (cancelled) return;
          setPages([items]);
          setPageTokens(nextPageToken ? ["", nextPageToken] : [""]);
          setTotalCount(totalCount);
        } else {
          const { items, nextPageToken, totalCount } = await fetchGroupPage("");
          if (cancelled) return;
          setPages([items]);
          setPageTokens(nextPageToken ? ["", nextPageToken] : [""]);
          setTotalCount(totalCount);
        }
      } catch (err) {
        if (!cancelled) setLoadError(err instanceof Error ? err.message : String(err));
      } finally {
        if (!cancelled) setPageLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [
    show,
    kind,
    currentParentKey,
    fetchSitesPage,
    fetchBuildingsPage,
    fetchRackPage,
    fetchGroupPage,
    buildingsRef,
    sitesRef,
  ]);

  const loadNextPage = useCallback(async () => {
    if (!IS_PAGINATED[kind]) return;
    const nextIndex = currentPage + 1;
    if (pages[nextIndex]) {
      setCurrentPage(nextIndex);
      return;
    }
    const token = pageTokens[nextIndex];
    if (!token) return;
    setPageLoading(true);
    try {
      const fetcher = kind === "rack" ? fetchRackPage : fetchGroupPage;
      const { items, nextPageToken, totalCount: total } = await fetcher(token);
      setPages((prev) => {
        const next = [...prev];
        next[nextIndex] = items;
        return next;
      });
      setPageTokens((prev) => {
        if (!nextPageToken) return prev;
        const next = [...prev];
        next[nextIndex + 1] = nextPageToken;
        return next;
      });
      setTotalCount(total);
      setCurrentPage(nextIndex);
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : String(err));
    } finally {
      setPageLoading(false);
    }
  }, [kind, currentPage, pages, pageTokens, fetchRackPage, fetchGroupPage]);

  const goPrevPage = useCallback(() => {
    setCurrentPage((p) => Math.max(0, p - 1));
  }, []);

  const visibleItems = pages[currentPage] ?? [];
  const hasNextPage = IS_PAGINATED[kind] && (pages[currentPage + 1] !== undefined || !!pageTokens[currentPage + 1]);
  const hasPrevPage = currentPage > 0;
  const showPaginationFooter = IS_PAGINATED[kind] && totalCount > PAGE_SIZE;

  const handleToggle = useCallback(
    (id: string) => {
      setSelectedIds((prev) => {
        if (selectionMode === "single") {
          if (prev.has(id) && prev.size === 1) return new Set();
          return new Set([id]);
        }
        const next = new Set(prev);
        if (next.has(id)) {
          next.delete(id);
        } else {
          next.add(id);
        }
        return next;
      });
    },
    [selectionMode],
  );

  const handleCreateNewToggle = useCallback((e: ChangeEvent<HTMLInputElement>) => {
    setCreateNewChecked(e.target.checked);
    if (!e.target.checked) setNewName("");
  }, []);

  const hasCreateNew = !!createNewLabel && !!onCreateNew;
  const trimmedNewName = newName.trim();
  const wantsCreateNew = hasCreateNew && createNewChecked && trimmedNewName.length > 0;
  const isUnchangedSingleSelection =
    currentParentKey !== null && selectedIds.size === 1 && selectedIds.has(currentParentKey);
  const canSave = (selectedIds.size > 0 && !isUnchangedSingleSelection) || wantsCreateNew;

  const handleSave = useCallback(async () => {
    if (!canSave) return;
    setSaving(true);
    try {
      const tasks: Promise<unknown>[] = [];
      if (selectedIds.size > 0) {
        const ids = Array.from(selectedIds).map((id) => BigInt(id));
        tasks.push(Promise.resolve(onConfirm(ids)));
      }
      if (wantsCreateNew && onCreateNew) {
        tasks.push(onCreateNew(trimmedNewName));
      }
      await Promise.all(tasks);
      onDismiss();
    } catch {
      // Caller surfaces the error via toast; keep the picker open so
      // the operator can retry or change selection.
    } finally {
      setSaving(false);
    }
  }, [canSave, selectedIds, wantsCreateNew, onConfirm, onCreateNew, trimmedNewName, onDismiss]);

  const titleByKind: Record<PickerKind, string> = {
    site: `Add ${sourceLabel} to a site`,
    building: `Add ${sourceLabel} to a building`,
    rack: `Add ${sourceLabel} to a rack`,
    group: `Add ${sourceLabel} to group`,
  };
  const hintHeaderByKind: Record<PickerKind, string> = {
    site: "Miners",
    building: "Site",
    rack: "Building",
    group: "Miners",
  };
  const hintHeader = hintHeaderByKind[kind];
  const isInitialLoad = pages.length === 0 && pageLoading;
  const hasAnyItems = totalCount > 0;
  const title = !hasAnyItems && hasCreateNew ? (createNewLabel ?? titleByKind[kind]) : titleByKind[kind];

  if (!show) return null;

  return (
    <Modal
      open={show}
      onDismiss={onDismiss}
      title={title}
      description={description}
      divider={false}
      buttons={[
        {
          text: "Save",
          variant: "primary",
          onClick: handleSave,
          disabled: !canSave || saving,
          loading: saving,
          dismissModalOnClick: false,
        },
      ]}
    >
      {loadError ? <Callout className="mb-4" intent="danger" prefixIcon={<Alert />} title={loadError} /> : null}
      {isInitialLoad ? (
        <div className="flex justify-center py-20">
          <ProgressCircular indeterminate />
        </div>
      ) : (
        <div>
          {hasCreateNew && hasAnyItems ? (
            <label className="mb-6 flex items-center gap-6">
              <Checkbox checked={createNewChecked} onChange={handleCreateNewToggle} />
              <div className="flex-1">
                <Input
                  id="parent-picker-new-name"
                  label={createNewLabel}
                  initValue={newName}
                  // Typing arms the checkbox so Save enables without a second click.
                  onChange={(value) => {
                    setNewName(value);
                    if (!createNewChecked && value.length > 0) setCreateNewChecked(true);
                  }}
                />
              </div>
            </label>
          ) : null}
          {hasCreateNew && !hasAnyItems ? (
            <div className="mb-4">
              <Input
                id="parent-picker-new-name"
                label={createNewLabel}
                initValue={newName}
                onChange={(value) => {
                  setNewName(value);
                  if (!createNewChecked) setCreateNewChecked(true);
                }}
                autoFocus
              />
            </div>
          ) : null}
          {hasAnyItems ? (
            <div className="flex items-center gap-6 border-b border-border-5 pb-2 text-emphasis-300 text-text-primary">
              <div className="w-[18px] shrink-0" aria-hidden />
              <span className="w-1/2 truncate">Name</span>
              <span className="w-1/2 truncate">{hintHeader}</span>
            </div>
          ) : null}
          {pageLoading && pages.length > 0 ? (
            <div className="flex justify-center py-8">
              <ProgressCircular indeterminate />
            </div>
          ) : (
            visibleItems.map((item) => (
              <label
                key={item.id}
                className="flex cursor-pointer items-center gap-6 border-b border-border-5 py-3 text-300"
              >
                {selectionMode === "single" ? (
                  <Radio selected={selectedIds.has(item.id)} onChange={() => handleToggle(item.id)} />
                ) : (
                  <Checkbox checked={selectedIds.has(item.id)} onChange={() => handleToggle(item.id)} />
                )}
                <span className="w-1/2 truncate text-emphasis-300">{item.label}</span>
                <span className="w-1/2 truncate">{item.hint}</span>
              </label>
            ))
          )}
          {showPaginationFooter ? (
            <div className="mt-3 flex items-center justify-between">
              <span className="text-300 text-text-primary-70">
                Page {currentPage + 1} of {Math.max(1, Math.ceil(totalCount / PAGE_SIZE))} · {totalCount} total
              </span>
              <div className="flex items-center gap-2">
                <Button
                  variant={variants.secondary}
                  size={buttonSizes.compact}
                  prefixIcon={<ArrowLeftCompact />}
                  ariaLabel="Previous page"
                  disabled={!hasPrevPage || pageLoading}
                  onClick={goPrevPage}
                />
                <Button
                  variant={variants.secondary}
                  size={buttonSizes.compact}
                  prefixIcon={<ArrowRight />}
                  ariaLabel="Next page"
                  disabled={!hasNextPage || pageLoading}
                  onClick={() => {
                    void loadNextPage();
                  }}
                />
              </div>
            </div>
          ) : null}
        </div>
      )}
    </Modal>
  );
};

export default ParentPickerModal;
