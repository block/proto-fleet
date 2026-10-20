import { getModuleStatusPresentation } from "./moduleStatusPresentation";
import StatusCircle from "@/shared/components/StatusCircle";

// Controller states map to the same display statuses as miners. Missing health
// evidence stays Unknown rather than taking the miner renderer's hashing default.
export function ModuleStatus({ state, indicatorOnly = false }: { state: string; indicatorOnly?: boolean }) {
  const status = getModuleStatusPresentation(state);
  return (
    <span className="flex items-center gap-2" title={indicatorOnly ? status.label : undefined}>
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
