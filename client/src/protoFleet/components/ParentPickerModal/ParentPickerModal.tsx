import { type ChangeEvent, useCallback, useEffect, useMemo, useState } from "react";

import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { useSites } from "@/protoFleet/api/sites";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import { Alert } from "@/shared/assets/icons";
import Callout from "@/shared/components/Callout";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Radio from "@/shared/components/Radio";

const INACTIVE_PLACEHOLDER = "—";

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
  // Hidden from candidates. Used to drop the row's current parent from
  // re-parent flows. Ignored in multi-select.
  excludeId?: bigint;
  sourceLabel: string;
  description?: string;
  // When set, an inline create-new row appears. `onCreateNew` fires on
  // Save alongside any `onConfirm` for existing selections.
  createNewLabel?: string;
  onCreateNew?: (name: string) => Promise<void>;
  onDismiss: () => void;
  onConfirm: (targetIds: bigint[]) => void | Promise<void>;
}

const ParentPickerModal = ({
  kind,
  show,
  selectionMode,
  excludeId,
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

  const [items, setItems] = useState<PickerItem[] | undefined>(undefined);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [saving, setSaving] = useState(false);
  const [createNewChecked, setCreateNewChecked] = useState(false);
  const [newName, setNewName] = useState("");

  useEffect(() => {
    if (!show) return;
    queueMicrotask(() => {
      setItems(undefined);
      setLoadError(null);
      setSelectedIds(new Set());
      setCreateNewChecked(false);
      setNewName("");
      setSaving(false);
    });

    // Multi-select keeps the current parent visible so the operator
    // sees the existing membership state.
    const exclude = selectionMode === "single" && excludeId !== undefined ? excludeId.toString() : null;
    const toRows = (rows: PickerItem[]) => rows.filter((row) => row.id !== exclude);

    if (kind === "site") {
      void listSites({
        onSuccess: (sites: SiteWithCounts[]) => {
          const rows: PickerItem[] = sites
            .filter((s) => !!s.site)
            .map((s) => ({
              id: s.site!.id.toString(),
              label: s.site!.name,
              hint: `${s.deviceCount.toString()} miners`,
            }));
          setItems(toRows(rows));
        },
        onError: (msg) => setLoadError(msg),
      });
      return;
    }

    if (kind === "group") {
      void listGroups({
        onSuccess: (deviceSets) => {
          const rows: PickerItem[] = deviceSets.map((set) => ({
            id: set.id.toString(),
            label: set.label,
            hint: `${set.deviceCount} miners`,
          }));
          setItems(toRows(rows));
        },
        onError: (msg) => setLoadError(msg),
      });
      return;
    }

    if (kind === "building") {
      const sitesPromise = new Promise<SiteWithCounts[]>((resolve, reject) => {
        void listSites({ onSuccess: resolve, onError: (msg) => reject(new Error(msg)) });
      });
      const buildingsPromise = new Promise<BuildingWithCounts[]>((resolve, reject) => {
        void listAllBuildings({ onSuccess: resolve, onError: (msg) => reject(new Error(msg)) });
      });
      Promise.all([sitesPromise, buildingsPromise])
        .then(([sites, buildings]) => {
          const siteName = new Map<string, string>();
          for (const s of sites) {
            if (s.site) siteName.set(s.site.id.toString(), s.site.name);
          }
          const rows: PickerItem[] = buildings
            .filter((b) => !!b.building)
            .map((b) => {
              const sid = b.building!.siteId?.toString();
              return {
                id: b.building!.id.toString(),
                label: b.building!.name,
                hint: sid ? (siteName.get(sid) ?? INACTIVE_PLACEHOLDER) : INACTIVE_PLACEHOLDER,
              };
            });
          setItems(toRows(rows));
        })
        .catch((err: Error) => setLoadError(err.message));
      return;
    }

    const buildingsPromise = new Promise<BuildingWithCounts[]>((resolve, reject) => {
      void listAllBuildings({ onSuccess: resolve, onError: (msg) => reject(new Error(msg)) });
    });
    const racksPromise = new Promise<DeviceSet[]>((resolve, reject) => {
      void listRacks({
        onSuccess: (deviceSets) => resolve(deviceSets),
        onError: (msg) => reject(new Error(msg)),
      });
    });
    Promise.all([buildingsPromise, racksPromise])
      .then(([buildings, racks]) => {
        const buildingName = new Map<string, string>();
        for (const b of buildings) {
          if (b.building) buildingName.set(b.building.id.toString(), b.building.name);
        }
        const rows: PickerItem[] = racks.map((rack) => {
          const rackInfo = rack.typeDetails.case === "rackInfo" ? rack.typeDetails.value : undefined;
          const bid = rackInfo?.buildingId?.toString();
          return {
            id: rack.id.toString(),
            label: rack.label || INACTIVE_PLACEHOLDER,
            hint: bid ? (buildingName.get(bid) ?? INACTIVE_PLACEHOLDER) : INACTIVE_PLACEHOLDER,
          };
        });
        setItems(toRows(rows));
      })
      .catch((err: Error) => setLoadError(err.message));
  }, [show, kind, selectionMode, excludeId, listSites, listAllBuildings, listRacks, listGroups]);

  const sortedItems = useMemo(() => {
    if (!items) return [];
    return [...items].sort((a, b) => a.label.localeCompare(b.label));
  }, [items]);

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
  const canSave = selectedIds.size > 0 || wantsCreateNew;

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
  const hasItems = sortedItems.length > 0;
  const title = !hasItems && hasCreateNew ? (createNewLabel ?? titleByKind[kind]) : titleByKind[kind];

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
      {items === undefined && !loadError ? (
        <div className="flex justify-center py-20">
          <ProgressCircular indeterminate />
        </div>
      ) : (
        <div>
          {hasCreateNew && hasItems ? (
            <label className="mb-6 flex items-center gap-6">
              <Checkbox checked={createNewChecked} onChange={handleCreateNewToggle} />
              <div className="flex-1">
                <Input
                  id="parent-picker-new-name"
                  label={createNewLabel}
                  initValue={newName}
                  onChange={(value) => setNewName(value)}
                  disabled={!createNewChecked}
                />
              </div>
            </label>
          ) : null}
          {hasCreateNew && !hasItems ? (
            <div className="mb-4">
              <Input
                id="parent-picker-new-name"
                label={createNewLabel}
                initValue={newName}
                onChange={(value) => {
                  setNewName(value);
                  // Empty list + create-new is the only path — auto-arm
                  // so the Save button enables on first keystroke.
                  if (!createNewChecked) setCreateNewChecked(true);
                }}
                autoFocus
              />
            </div>
          ) : null}
          {hasItems ? (
            <div className="flex items-center gap-6 border-b border-border-5 pb-2 text-emphasis-300 text-text-primary">
              {/* Spacer aligns Name column over the row's checkbox/radio. */}
              <div className="w-[18px] shrink-0" aria-hidden />
              <span className="w-1/2 truncate">Name</span>
              <span className="w-1/2 truncate">{hintHeader}</span>
            </div>
          ) : null}
          {sortedItems.map((item) => (
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
          ))}
        </div>
      )}
    </Modal>
  );
};

export default ParentPickerModal;
