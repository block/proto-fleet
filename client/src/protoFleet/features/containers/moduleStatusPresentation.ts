import { statuses } from "@/shared/components/StatusCircle";
import type { StatusCircleStatus } from "@/shared/components/StatusCircle/constants";
import type { MinerStatus } from "@/shared/hooks/useStatusSummary";

type StatusPresentation = { label: MinerStatus | "Unknown"; indicator: StatusCircleStatus };

const unknown: StatusPresentation = { label: "Unknown", indicator: statuses.inactive };
const moduleStatuses: ReadonlyMap<string, StatusPresentation> = new Map([
  ["hashing", { label: "Hashing", indicator: statuses.normal }],
  ["offline", { label: "Offline", indicator: statuses.inactive }],
  ["sleeping", { label: "Sleeping", indicator: statuses.sleeping }],
  ["attention", { label: "Needs attention", indicator: statuses.error }],
  ["isolated", { label: "Needs attention", indicator: statuses.error }],
]);

// Fleet presents isolation as attention while retaining the collected state.
export function getModuleStatusPresentation(state: string): StatusPresentation {
  return moduleStatuses.get(state) ?? unknown;
}
