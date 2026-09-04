import { useMemo, useState } from "react";
import type { InventoryPartItem } from "../../types";
import AdjustPartModal from "./AdjustPartModal";
import CreatePartModal from "./CreatePartModal";
import DeletePartModal from "./DeletePartModal";
import ImportCsvModal from "./ImportCsvModal";
import { useInventory } from "@/protoFleet/features/maintenance/hooks/useInventory";
import { useMaintenanceOptions } from "@/protoFleet/features/maintenance/hooks/useMaintenanceOptions";
import { useHasPermission } from "@/protoFleet/store";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles, ListAction } from "@/shared/components/List/types";
import Select from "@/shared/components/Select";

type Columns = "name" | "type" | "site" | "onHand" | "allocated" | "available" | "reorderPoint";
const activeCols: Columns[] = ["name", "type", "site", "onHand", "allocated", "available", "reorderPoint"];
const colTitles: ColTitles<Columns> = {
  name: "Part Name",
  type: "Type",
  site: "Site",
  onHand: "On Hand",
  allocated: "Allocated",
  available: "Available",
  reorderPoint: "Reorder Pt",
};
const InventoryTab = () => {
  const canManage = useHasPermission("maintenance:manage");
  const inventory = useInventory();
  const options = useMaintenanceOptions();
  const [adjust, setAdjust] = useState<InventoryPartItem | null>(null);
  const [remove, setRemove] = useState<InventoryPartItem | null>(null);
  const [create, setCreate] = useState(false);
  const [importing, setImporting] = useState(false);
  const [siteId, setSiteId] = useState("");
  const [low, setLow] = useState(false);
  const apply = (site = siteId, lowOnly = low) =>
    inventory.setFilter({ siteIds: site ? [BigInt(site)] : [], lowStockOnly: lowOnly });
  const columns: ColConfig<InventoryPartItem, string, Columns> = useMemo(
    () => ({
      name: { component: (p) => <span className="font-medium">{p.name}</span>, width: "w-60" },
      type: { component: (p) => <span>{p.type}</span>, width: "w-28" },
      site: { component: (p) => <span>{p.siteName ?? "Unassigned"}</span>, width: "w-28" },
      onHand: { component: (p) => <span>{p.onHand}</span>, width: "w-20" },
      allocated: { component: (p) => <span>{p.allocated}</span>, width: "w-20" },
      available: {
        component: (p) => <span className={p.lowStock ? "font-medium text-text-critical" : ""}>{p.available}</span>,
        width: "w-20",
      },
      reorderPoint: { component: (p) => <span>{p.reorderPoint}</span>, width: "w-20" },
    }),
    [],
  );
  const actions: ListAction<InventoryPartItem>[] = useMemo(
    () =>
      canManage
        ? [
            { title: "Adjust", actionHandler: setAdjust },
            { title: "Delete", actionHandler: setRemove, variant: "destructive" as const, showDividerAfter: false },
          ]
        : [],
    [canManage],
  );
  const mutationControls = canManage ? (
    <div className="flex gap-2">
      <Button text="Add part" variant={variants.secondary} size={buttonSizes.compact} onClick={() => setCreate(true)} />
      <Button
        text="Import CSV"
        variant={variants.secondary}
        size={buttonSizes.compact}
        onClick={() => setImporting(true)}
      />
    </div>
  ) : undefined;
  if (inventory.loading && !inventory.data.length) return <div role="status">Loading inventory…</div>;
  if (inventory.error && !inventory.data.length) return <div role="alert">{inventory.error}</div>;
  return (
    <div className="flex flex-col gap-4">
      <div className="flex gap-4">
        <Insight label="Total on hand" value={inventory.insights?.totalOnHand ?? 0} />
        <Insight label="Allocated" value={inventory.insights?.totalAllocated ?? 0} />
        <Insight label="Low stock" value={inventory.insights?.lowStockCount ?? 0} />
        <Insight label="Sites" value={inventory.insights?.sitesCount ?? 0} />
      </div>
      <div className="flex items-end gap-3">
        <Select
          id="inventory-site"
          label="Site"
          value={siteId}
          options={[{ value: "", label: "All sites" }, ...options.sites.map((s) => ({ value: s.id, label: s.name }))]}
          onChange={(value) => {
            setSiteId(value);
            apply(value);
          }}
        />
        <label className="flex items-center gap-2">
          <Checkbox
            checked={low}
            onChange={(event) => {
              setLow(event.target.checked);
              apply(siteId, event.target.checked);
            }}
          />
          Low stock
        </label>
      </div>
      {inventory.data.length ? (
        <List
          items={inventory.data}
          itemKey="id"
          activeCols={activeCols}
          colTitles={colTitles}
          colConfig={columns}
          actions={actions}
          stickyFirstColumn={false}
          total={inventory.data.length}
          itemName={{ singular: "part", plural: "parts" }}
          headerControls={mutationControls}
        />
      ) : (
        <div className="flex items-center justify-between">
          <span>No inventory parts</span>
          {mutationControls}
        </div>
      )}
      {inventory.nextPageToken ? (
        <Button text="Load more" variant={variants.secondary} onClick={() => void inventory.loadMore()} />
      ) : null}
      {adjust ? <AdjustPartModal part={adjust} onDismiss={() => setAdjust(null)} onSubmit={inventory.adjust} /> : null}
      {remove ? (
        <DeletePartModal
          partName={remove.name}
          onDismiss={() => setRemove(null)}
          onDelete={() => inventory.remove(remove.id)}
        />
      ) : null}
      {create ? (
        <CreatePartModal sites={options.sites} onDismiss={() => setCreate(false)} onSubmit={inventory.create} />
      ) : null}
      {importing ? (
        <ImportCsvModal
          onDismiss={() => setImporting(false)}
          onPreview={inventory.previewCsv}
          onConfirm={inventory.applyCsv}
          onSuccess={() => setImporting(false)}
        />
      ) : null}
    </div>
  );
};
const Insight = ({ label, value }: { label: string; value: number }) => (
  <div className="flex flex-1 flex-col rounded-xl border border-border-5 p-4">
    <span>{label}</span>
    <strong>{value}</strong>
  </div>
);
export default InventoryTab;
