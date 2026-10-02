import StatusCircle, { statuses } from "@/shared/components/StatusCircle";
import type { StatusCircleStatus } from "@/shared/components/StatusCircle/constants";
import type { MinerStatus } from "@/shared/hooks/useStatusSummary";

type StatusPresentation = { label: MinerStatus | "Unknown"; indicator: StatusCircleStatus; title?: string };

const unknown: StatusPresentation = { label: "Unknown", indicator: statuses.inactive };
const moduleStatuses: ReadonlyMap<string, StatusPresentation> = new Map([
  ["hashing", { label: "Hashing", indicator: statuses.normal }],
  ["offline", { label: "Offline", indicator: statuses.inactive }],
  ["sleeping", { label: "Sleeping", indicator: statuses.sleeping }],
  ["attention", { label: "Needs attention", indicator: statuses.error }],
  ["isolated", { label: "Needs attention", indicator: statuses.error, title: "Module is isolated" }],
]);

// Controller states map to the same display statuses as miners. Missing health
// evidence stays Unknown rather than taking the miner renderer's hashing default.
export function ModuleStatus({ state, indicatorOnly = false }: { state: string; indicatorOnly?: boolean }) {
  const status = moduleStatuses.get(state) ?? unknown;
  return (
    <span className="flex items-center gap-2" title={status.title ?? (indicatorOnly ? status.label : undefined)}>
      <StatusCircle
        status={status.indicator}
        variant="simple"
        width="w-[6px]"
        removeMargin={indicatorOnly}
        testId="module-status-indicator"
      />
      {indicatorOnly ? null : status.label}
    </span>
  );
}
