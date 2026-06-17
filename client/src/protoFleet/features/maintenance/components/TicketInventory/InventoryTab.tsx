import { useCallback, useMemo, useState } from "react";

import AdjustPartModal from "./AdjustPartModal";
import ImportCsvModal from "./ImportCsvModal";
import { mockInventoryParts } from "../../mockData";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles, ListAction } from "@/shared/components/List/types";
import type { ButtonFilterItem, DropdownFilterItem, FilterItem } from "@/shared/components/List/Filters/types";

type InventoryColumns = "name" | "type" | "site" | "onHand" | "allocated" | "available" | "reorderPoint";

interface PartItem {
  id: string;
  name: string;
  type: string;
  siteName: string;
  onHand: number;
  allocated: number;
  reorderPoint: number;
  binLocation: string;
}

const activeCols: InventoryColumns[] = ["name", "type", "site", "onHand", "allocated", "available", "reorderPoint"];

const colTitles: ColTitles<InventoryColumns> = {
  name: "Part Name",
  type: "Type",
  site: "Site",
  onHand: "On Hand",
  allocated: "Allocated",
  available: "Available",
  reorderPoint: "Reorder Pt",
};

const InventoryTab = () => {
  const [parts] = useState<PartItem[]>(mockInventoryParts);
  const [adjustPart, setAdjustPart] = useState<PartItem | null>(null);
  const [showImport, setShowImport] = useState(false);

  const colConfig: ColConfig<PartItem, string, InventoryColumns> = useMemo(
    () => ({
      name: {
        component: (part) => <span className="text-emphasis-300 font-medium">{part.name}</span>,
        width: "w-60",
      },
      type: {
        component: (part) => <span className="text-300">{part.type}</span>,
        width: "w-28",
      },
      site: {
        component: (part) => <span className="text-300">{part.siteName}</span>,
        width: "w-28",
      },
      onHand: {
        component: (part) => <span className="text-300">{part.onHand}</span>,
        width: "w-20",
      },
      allocated: {
        component: (part) => <span className="text-300">{part.allocated}</span>,
        width: "w-20",
      },
      available: {
        component: (part) => {
          const available = part.onHand - part.allocated;
          const isLow = available <= part.reorderPoint;
          return (
            <span className={`text-300 ${isLow ? "text-text-critical font-medium" : ""}`}>
              {available}
            </span>
          );
        },
        width: "w-20",
      },
      reorderPoint: {
        component: (part) => <span className="text-300">{part.reorderPoint}</span>,
        width: "w-20",
      },
    }),
    [],
  );

  const actions: ListAction<PartItem>[] = useMemo(
    () => [
      {
        title: "Adjust",
        actionHandler: (part) => setAdjustPart(part),
      },
    ],
    [],
  );

  const filters: FilterItem[] = useMemo(
    () => [
      {
        type: "button",
        title: "Low stock",
        value: "low_stock",
        count: 0,
      } satisfies ButtonFilterItem,
      {
        type: "dropdown",
        title: "Site",
        value: "site",
        options: [],
        defaultOptionIds: [],
      } satisfies DropdownFilterItem,
      {
        type: "dropdown",
        title: "Type",
        value: "type",
        options: [],
        defaultOptionIds: [],
      } satisfies DropdownFilterItem,
    ],
    [],
  );

  return (
    <div className="flex flex-col gap-4">
      <div className="flex gap-4">
        <InsightCard label="Total on hand" value={String(parts.reduce((s, p) => s + p.onHand, 0))} />
        <InsightCard label="Allocated" value={String(parts.reduce((s, p) => s + p.allocated, 0))} />
        <InsightCard label="Low stock" value={String(parts.filter((p) => p.onHand - p.allocated <= p.reorderPoint).length)} />
        <InsightCard label="Sites" value={String(new Set(parts.map((p) => p.siteName)).size)} />
      </div>

      <List
        items={parts}
        itemKey="id"
        activeCols={activeCols}
        colTitles={colTitles}
        colConfig={colConfig}
        actions={actions}
        filters={filters}
        stickyFirstColumn={false}
        headerControls={
          <div className="flex gap-2">
            <Button
              text="Import CSV"
              variant={variants.secondary}
              size={buttonSizes.compact}
              onClick={() => setShowImport(true)}
            />
            <Button text="Export CSV" variant={variants.secondary} size={buttonSizes.compact} />
          </div>
        }
      />

      {adjustPart && (
        <AdjustPartModal
          part={adjustPart}
          onDismiss={() => setAdjustPart(null)}
          onSuccess={() => setAdjustPart(null)}
        />
      )}

      {showImport && (
        <ImportCsvModal
          onDismiss={() => setShowImport(false)}
          onSuccess={() => setShowImport(false)}
        />
      )}
    </div>
  );
};

const InsightCard = ({ label, value }: { label: string; value: string }) => (
  <div className="flex flex-1 flex-col gap-1 rounded-xl border border-border-5 p-4">
    <span className="text-200 text-text-primary-70">{label}</span>
    <span className="text-emphasis-400 font-medium">{value}</span>
  </div>
);

export default InventoryTab;
