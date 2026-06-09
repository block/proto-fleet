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
  // Right-column hint that varies per kind: group/site = "<N> miners",
  // building = parent site name, rack = parent building name.
  hint: string;
};

interface ParentPickerModalProps {
  kind: PickerKind;
  show: boolean;
  // "single" enforces at-most-one selection (re-parent flows where a
  // child can only have one parent); "multi" allows N (add-to-group
  // where a miner can belong to multiple groups).
  selectionMode: "single" | "multi";
  // Hide one id from the candidate list. Use to exclude the current
  // parent (operator clicking "Add to building" shouldn't see the
  // building the rack is already in). Ignored in multi-select.
  excludeId?: bigint;
  // Display string for the source — "Rack 17" / "12 miners". Surfaces
  // in the title + description so the operator knows what they're
  // assigning.
  sourceLabel: string;
  // Optional info line rendered under the title. Used to surface
  // cascade impact ("N miners will move with this rack") so the
  // operator sees what gets carried along with the re-parent.
  description?: string;
  // Optional inline "Create new" row. When provided, the picker renders
  // a checkbox + name input at the top; on Save it calls `onCreateNew`
  // (in addition to onConfirm for any selected existing rows). Group
  // re-parent uses this today.
  createNewLabel?: string;
  onCreateNew?: (name: string) => Promise<void>;
  onDismiss: () => void;
  // Picker collects selections + delegates dispatch to the host.
  // Single-select kinds always pass a 1-element array; multi-select
  // passes 0+.
  onConfirm: (targetIds: bigint[]) => void | Promise<void>;
}

// Picker for "Add <miners|rack|building> to <site|building|rack|group>"
// flows. Replaces the standalone AddToGroupModal + the prior
// List-component-based ParentPickerModal — same checkbox-row layout
// across all four kinds, with selection mode + optional "Create new"
// gated by props.
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

    // excludeId only applies in single-select (re-parent). In
    // multi-select the operator may want to see the current parent in
    // the list so they can leave it checked.
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
      // Buildings need parent-site labels for the hint column. Load
      // sites in parallel and join client-side.
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

    // kind === "rack"
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
          // Toggle off when clicking the already-selected row; otherwise
          // replace the selection so at most one is held.
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
                  // No existing items + create-new is the only path —
                  // implicitly enable the create branch as the operator
                  // types so canSave flips on without an extra click.
                  if (!createNewChecked) setCreateNewChecked(true);
                }}
                autoFocus
              />
            </div>
          ) : null}
          {hasItems ? (
            <div className="flex items-center gap-6 border-b border-border-5 pb-2 text-emphasis-300 text-text-primary">
              {/* Spacer matches the row checkbox column so the label
                  column lines up with the checkbox-less header text. */}
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
