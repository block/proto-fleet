import type { ReactNode } from "react";
import { InventoryCell } from "./InventoryCell";
import { tankMetricCoverage } from "./tankMetricCoverage";
import type { InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

export default function TankInventoryCard({
  item,
  onClick,
  actions,
}: {
  item: InventoryItem;
  onClick: () => void;
  actions?: ReactNode;
}) {
  const hashrateCoverage = Number.isFinite(item.hashrateThs) ? tankMetricCoverage(item, "hashrate") : undefined;
  const powerCoverage = Number.isFinite(item.powerKw) ? tankMetricCoverage(item, "power") : undefined;
  const summary = [`${item.assetCount} ${item.assetCount === 1n ? "module" : "modules"}`];
  if (hashrateCoverage && hashrateCoverage === powerCoverage) {
    if (item.coverage) {
      summary.push(hashrateCoverage);
    } else {
      summary[0] = hashrateCoverage;
    }
  } else {
    if (hashrateCoverage) summary.push(`Hashrate: ${hashrateCoverage}`);
    if (powerCoverage) summary.push(`Power: ${powerCoverage}`);
  }

  return (
    <div className="relative h-full rounded-2xl bg-surface-overlay" data-testid="tank-inventory-card">
      <button
        type="button"
        className="flex h-full w-full cursor-pointer flex-col rounded-2xl p-6 text-left transition-opacity hover:opacity-80 focus-visible:outline-2 focus-visible:outline-core-accent-fill"
        onClick={onClick}
      >
        <div className="w-full pr-8">
          <InventoryCell item={item} column="name" />
        </div>
        <div className="py-3 text-200 text-text-primary-70" data-testid="tank-inventory-coverage">
          {summary.map((line) => (
            <p key={line}>{line}</p>
          ))}
        </div>
        <div className="w-full">
          <InventoryCell item={item} column="health" />
        </div>
        <div className="mt-auto grid w-full grid-cols-2 gap-3 pt-4">
          <InventoryCell item={item} column="hashrate" showCoverage={false} />
          <InventoryCell item={item} column="power" showCoverage={false} />
        </div>
      </button>
      {actions ? <div className="absolute top-3 right-3">{actions}</div> : null}
    </div>
  );
}
