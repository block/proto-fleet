import { useMemo, useState } from "react";
import type { InventoryPartItem } from "../../types";
import ListPagination from "../ListPagination";
import AdjustPartModal from "./AdjustPartModal";
import CreatePartModal from "./CreatePartModal";
import DeletePartModal from "./DeletePartModal";
import ImportCsvModal from "./ImportCsvModal";
import { useInventory } from "@/protoFleet/features/maintenance/hooks/useInventory";
import { useMaintenanceOptions } from "@/protoFleet/features/maintenance/hooks/useMaintenanceOptions";
import { useHasPermission } from "@/protoFleet/store";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import DropdownFilter from "@/shared/components/List/Filters/DropdownFilter";
import type { ColConfig, ColTitles, ListAction } from "@/shared/components/List/types";

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
  const [siteIds, setSiteIds] = useState<string[]>([]);
  const [types, setTypes] = useState<string[]>([]);
  const [lowStockOnly, setLowStockOnly] = useState(false);

  const applyFilters = (sites = siteIds, partTypes = types, lowStock = lowStockOnly) => {
    inventory.setFilter({
      siteIds: sites.map(BigInt),
      types: partTypes,
      lowStockOnly: lowStock,
    });
  };

  const columns: ColConfig<InventoryPartItem, string, Columns> = useMemo(
    () => ({
      name: { component: (part) => <span className="font-medium">{part.name}</span>, width: "w-60" },
      type: { component: (part) => <span>{part.type}</span>, width: "w-28" },
      site: { component: (part) => <span>{part.siteName ?? "Unassigned"}</span>, width: "w-28" },
      onHand: { component: (part) => <span>{part.onHand}</span>, width: "w-20" },
      allocated: { component: (part) => <span>{part.allocated}</span>, width: "w-20" },
      available: {
        component: (part) => (
          <span className={part.lowStock ? "font-medium text-text-critical" : ""}>{part.available}</span>
        ),
        width: "w-20",
      },
      reorderPoint: { component: (part) => <span>{part.reorderPoint}</span>, width: "w-20" },
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

  if (inventory.loading && !inventory.data.length) return <div role="status">Loading inventory…</div>;
  if (inventory.error && !inventory.data.length) return <div role="alert">{inventory.error}</div>;

  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-2 gap-x-8 gap-y-5 py-2 laptop:grid-cols-4">
        <Insight label="Total on hand" value={inventory.insights?.totalOnHand ?? 0} />
        <Insight label="Allocated to repairs" value={inventory.insights?.totalAllocated ?? 0} />
        <Insight
          label="Low stock items"
          value={inventory.insights?.lowStockCount ?? 0}
          actionLabel="Show low stock items"
          active={lowStockOnly}
          onActivate={() => {
            setLowStockOnly(true);
            applyFilters(siteIds, types, true);
          }}
        />
        <Insight label="Sites" value={inventory.insights?.sitesCount ?? 0} />
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <Button
          variant={lowStockOnly ? variants.accent : variants.ghost}
          size={buttonSizes.compact}
          onClick={() => {
            const next = !lowStockOnly;
            setLowStockOnly(next);
            applyFilters(siteIds, types, next);
          }}
        >
          Low stock
        </Button>
        <DropdownFilter
          title="Site"
          pluralTitle="sites"
          options={options.sites.map((site) => ({ id: site.id, label: site.name }))}
          selectedOptions={siteIds}
          showSelectAll={false}
          onSelect={(selected) => {
            setSiteIds(selected);
            applyFilters(selected);
          }}
        />
        <DropdownFilter
          title="Type"
          options={(inventory.insights?.partTypes ?? []).map((type) => ({ id: type, label: type }))}
          selectedOptions={types}
          showSelectAll={false}
          onSelect={(selected) => {
            setTypes(selected);
            applyFilters(siteIds, selected);
          }}
        />
        {canManage ? (
          <div className="ml-auto flex gap-2 phone:ml-0 phone:w-full">
            <Button
              text="Add part"
              variant={variants.secondary}
              size={buttonSizes.compact}
              onClick={() => setCreate(true)}
            />
            <Button
              text="Import CSV"
              variant={variants.secondary}
              size={buttonSizes.compact}
              onClick={() => setImporting(true)}
            />
          </div>
        ) : null}
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
          total={inventory.total}
          hideTotal
          itemName={{ singular: "part", plural: "parts" }}
        />
      ) : (
        <span>No inventory parts</span>
      )}

      <ListPagination
        currentPage={inventory.currentPage}
        pageSize={50}
        visibleCount={inventory.data.length}
        total={inventory.total}
        itemName="parts"
        hasNextPage={!!inventory.nextPageToken}
        loading={inventory.loading}
        onPrevious={() => void inventory.previousPage()}
        onNext={() => void inventory.nextPage()}
      />

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

interface InsightProps {
  label: string;
  value: number;
  actionLabel?: string;
  active?: boolean;
  onActivate?: () => void;
}

const Insight = ({ label, value, actionLabel, active = false, onActivate }: InsightProps) => {
  const content = (
    <>
      <span className="text-300 text-text-primary-50">{label}</span>
      <strong className="text-heading-400 font-medium">{value}</strong>
    </>
  );
  return onActivate ? (
    <button
      type="button"
      className="flex flex-col items-start text-left"
      aria-label={actionLabel}
      aria-pressed={active}
      onClick={onActivate}
    >
      {content}
    </button>
  ) : (
    <div className="flex flex-col">{content}</div>
  );
};

export default InventoryTab;
