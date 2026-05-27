import { useCallback, useEffect, useMemo, useState } from "react";

import { useBuildings } from "@/protoFleet/api/buildings";
import { type DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";

type SearchRackColumn = "name" | "building" | "status";

interface SearchRacksModalProps {
  open: boolean;
  // Parent building context — same eligibility rules as ManageRacksModal:
  // racks in this building or unassigned (with matching site, or no site)
  // are eligible; racks in another building or another site are visible
  // but disabled.
  siteId: bigint;
  currentBuildingId: bigint;
  onDismiss: () => void;
  // Returns a single chosen rack so the caller can add it to the working
  // set and assign it to the cell that was selected when the popover
  // opened.
  onConfirm: (rackId: bigint, label: string) => void;
}

interface SearchRackItem {
  id: string;
  label: string;
  buildingLabel: string;
  statusLabel: string;
  disabled: boolean;
}

const colTitles: ColTitles<SearchRackColumn> = {
  name: "Name",
  building: "Building",
  status: "Status",
};

const colConfig: ColConfig<SearchRackItem, string, SearchRackColumn> = {
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

const buildItem = (
  rack: DeviceSet,
  currentSiteId: bigint,
  currentBuildingId: bigint,
  buildingLabels: Record<string, string>,
): SearchRackItem | null => {
  if (rack.typeDetails.case !== "rackInfo") return null;
  const info = rack.typeDetails.value;
  const buildingId = info.buildingId;
  const siteId = info.siteId;
  const inOtherBuilding = buildingId !== undefined && buildingId !== 0n && buildingId !== currentBuildingId;
  const inThisBuilding = buildingId === currentBuildingId;
  const inOtherSite = !inThisBuilding && siteId !== undefined && siteId !== 0n && siteId !== currentSiteId;
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

// Single-rack picker with a name filter — mirrors SearchMinersModal in the
// rack-management feature. Picked rack is added to the building's working
// set and assigned to whatever cell was selected when the popover opened.
const SearchRacksModal = ({ open, siteId, currentBuildingId, onDismiss, onConfirm }: SearchRacksModalProps) => {
  const { listRacks } = useDeviceSets();
  const { listBuildingsBySite } = useBuildings();
  const [items, setItems] = useState<SearchRackItem[] | undefined>(undefined);
  const [error, setError] = useState<string | null>(null);
  const [selectedItems, setSelectedItems] = useState<string[]>([]);
  const [query, setQuery] = useState("");
  // Self-fetched building id → display label map for the Building
  // column. Mirrors ManageRacksModal so the two pickers stay aligned.
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
      onSuccess: (racks) => {
        if (cancelled) return;
        const out: SearchRackItem[] = [];
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

  const isRowDisabled = useCallback((item: SearchRackItem) => item.disabled, []);

  // Client-side filter on the rack label. Case-insensitive substring match —
  // matches the SearchMinersModal feel without bringing in a fuzzy lib.
  const filteredItems = useMemo(() => {
    if (!items) return [];
    const q = query.trim().toLowerCase();
    if (!q) return items;
    return items.filter((i) => i.label.toLowerCase().includes(q));
  }, [items, query]);

  const handleConfirm = useCallback(() => {
    if (!items || selectedItems.length === 0) return;
    const id = selectedItems[0];
    const item = items.find((r) => r.id === id);
    if (!item || item.disabled) return;
    onConfirm(BigInt(id), item.label);
  }, [items, selectedItems, onConfirm]);

  return (
    <Modal
      open={open}
      title="Search racks"
      size="large"
      className="flex !h-[calc(100vh-(--spacing(32)))] max-h-[calc(100vh-(--spacing(32)))] flex-col !overflow-hidden"
      bodyClassName="flex flex-1 min-h-0 flex-col overflow-hidden"
      onDismiss={onDismiss}
      divider={false}
      testId="search-racks-modal"
      buttons={[
        {
          text: "Assign",
          variant: "primary",
          disabled: selectedItems.length === 0,
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
            <List<SearchRackItem, string, SearchRackColumn>
              activeCols={activeCols}
              colTitles={colTitles}
              colConfig={colConfig}
              items={filteredItems}
              itemKey="id"
              itemSelectable
              selectionType="checkbox"
              customSelectedItems={selectedItems}
              customSetSelectedItems={(ids) => {
                // Single-select: only retain the last id the user toggled
                // on. List doesn't ship a singleSelect prop, so we enforce
                // it here.
                const next = Array.isArray(ids) ? ids : [];
                if (next.length <= 1) {
                  setSelectedItems(next);
                  return;
                }
                const newId = next.find((id) => !selectedItems.includes(id));
                setSelectedItems(newId ? [newId] : [next[next.length - 1]]);
              }}
              isRowDisabled={isRowDisabled}
              itemName={{ singular: "rack", plural: "racks" }}
              hideTotal
              containerClassName="min-h-0"
              overflowContainer
              stickyBgColor="bg-surface-elevated-base"
            />
          </div>
        )}
      </div>
    </Modal>
  );
};

export default SearchRacksModal;
