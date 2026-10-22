import { type ReactNode, useEffect, useState } from "react";
import { create } from "@bufbuild/protobuf";
import MiningActionsMenu from "../MiningActionsMenu";
import { isCurrentObservation } from "../moduleDetail";
import { ModuleIssues } from "../ModuleIssues";
import { getModuleStatusPresentation } from "../moduleStatusPresentation";
import { tankMetricCoverage } from "../tankMetricCoverage";
import ModuleHashboardCard from "./ModuleHashboardCard";
import ModulePerformance from "./ModulePerformance";
import ModulePsuCard from "./ModulePsuCard";
import {
  HashboardSlotSchema,
  type InventoryItem,
} from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { useModuleHashboards } from "@/protoFleet/api/useModuleHashboards";
import { useTemperatureUnit } from "@/protoFleet/store";
import Button from "@/shared/components/Button";
import Stats from "@/shared/components/Stats";
import {
  convertCtoF,
  formatEfficiencyOrDash,
  formatHashrateOrDash,
  formatPowerKwOrDash,
  formatTempRange,
} from "@/shared/utils/telemetryFormat";

export default function ModuleDetails({
  module,
  unavailable,
  enabled,
  refresh,
  header,
}: {
  module: InventoryItem;
  unavailable: boolean;
  enabled: boolean;
  refresh: () => void;
  header: ReactNode;
}) {
  const unit = useTemperatureUnit();
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 5000);
    return () => clearInterval(timer);
  }, []);
  const boards = useModuleHashboards(enabled ? [module.ref!.id] : [], String(module.ref!.id));
  const result = enabled ? boards.modules[String(module.ref!.id)] : undefined;
  const stale = unavailable || !isCurrentObservation(module.observedAt, now);
  const status = getModuleStatusPresentation(module.state);
  const statusUnavailable = unavailable || status.label === "Unknown";
  const kpis = [
    {
      label: "Hashrate",
      value: formatHashrateOrDash(stale ? null : (module.hashrateThs ?? null)),
      text: !stale ? tankMetricCoverage(module, "hashrate") : undefined,
    },
    {
      label: "Equipment power",
      value: formatPowerKwOrDash(stale ? null : module.powerKw),
      text: !stale ? tankMetricCoverage(module, "power") : undefined,
    },
    {
      label: "Efficiency",
      value: formatEfficiencyOrDash(stale ? null : (module.efficiencyJth ?? null)),
      text: !stale ? tankMetricCoverage(module, "efficiency") : undefined,
    },
    {
      label: "Temperature",
      value:
        !stale && module.minTemperatureC !== undefined && module.maxTemperatureC !== undefined
          ? module.minTemperatureC === module.maxTemperatureC
            ? `${(unit === "F" ? convertCtoF(module.minTemperatureC) : module.minTemperatureC).toFixed(1)} °${unit}`
            : formatTempRange(module.minTemperatureC, module.maxTemperatureC, unit)
          : "—",
      text: !stale ? tankMetricCoverage(module, "temperature") : undefined,
    },
  ];
  return (
    <>
      <div className="flex flex-col items-start justify-between gap-4 tablet:flex-row">
        <div className="w-full min-w-0 tablet:flex-1">{header}</div>
        <div className="flex shrink-0 flex-wrap items-center gap-3">
          <ModuleIssues
            item={statusUnavailable ? { ...module, moduleIssues: undefined } : module}
            statusKnown={!statusUnavailable}
            statusCircle={statusUnavailable ? "inactive" : status.indicator}
            status={statusUnavailable ? "Status unknown" : status.label}
          />
          <MiningActionsMenu
            unavailable={statusUnavailable}
            presentation="header"
            kind="module"
            items={[module]}
            onRefresh={() => {
              refresh();
              boards.retry();
            }}
          />
        </div>
      </div>
      {stale ? (
        <p role="status" className="text-200 text-text-primary-70">
          Current readings are unavailable or stale.
        </p>
      ) : null}
      <Stats
        stats={kpis.map((stat) => ({ ...stat, preformatted: true, valueClassName: "block whitespace-normal" }))}
        size="large"
        grid="grid-cols-4 phone:grid-cols-2 phone:gap-y-6"
        gap="gap-x-10 phone:gap-6"
        padding=""
      />
      <section aria-label="Hashboards" className="flex flex-col gap-4">
        <h2 className="text-heading-200">Hashboards</h2>
        {enabled && !result ? <p role="status">Loading hashboards…</p> : null}
        {result?.error ? (
          <div>
            <p role="alert">Could not load hashboards: {result.error}</p>
            <Button variant="secondary" text="Retry hashboards" onClick={boards.retry} />
          </div>
        ) : null}
        <div className="grid grid-cols-1 gap-4 laptop:grid-cols-3">
          {[1, 2, 3].map((position) => (
            <ModuleHashboardCard
              key={position}
              slot={
                !unavailable
                  ? (result?.data?.slots.find((slot) => slot.position === position) ??
                    create(HashboardSlotSchema, { position, occupancy: "unknown" }))
                  : create(HashboardSlotSchema, { position, occupancy: "unknown" })
              }
              now={now}
            />
          ))}
        </div>
      </section>
      <section aria-label="Other hardware" className="flex flex-col gap-4">
        <h2 className="text-heading-200">Other hardware</h2>
        <ModulePsuCard module={module} slots={result?.data?.slots ?? []} unavailable={unavailable} now={now} />
      </section>
      <ModulePerformance moduleId={module.ref!.id} enabled={enabled} />
    </>
  );
}
