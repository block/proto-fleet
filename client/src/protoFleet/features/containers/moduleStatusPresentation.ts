import { statuses } from "@/shared/components/StatusCircle";
import type { StatusCircleStatus } from "@/shared/components/StatusCircle/constants";
import type { MinerStatus } from "@/shared/hooks/useStatusSummary";

type StatusPresentation = {
  label: MinerStatus | "Unknown" | "Updating firmware";
  indicator: StatusCircleStatus;
  title?: string;
};

const unknown: StatusPresentation = { label: "Unknown", indicator: statuses.inactive };
const moduleStatuses: ReadonlyMap<string, StatusPresentation> = new Map([
  ["active", { label: "Hashing", indicator: statuses.normal }],
  ["offline", { label: "Offline", indicator: statuses.inactive }],
  ["inactive", { label: "Sleeping", indicator: statuses.sleeping }],
  ["error", { label: "Needs attention", indicator: statuses.error }],
  ["updating", { label: "Updating firmware", indicator: statuses.pending }],
  ["needs_mining_pool", { label: "Needs attention", indicator: statuses.error }],
  ["reboot_required", { label: "Needs attention", indicator: statuses.error }],
  ["maintenance", { label: "Sleeping", indicator: statuses.sleeping, title: "Module is in maintenance" }],
]);

export function getModuleStatusPresentation(state: string): StatusPresentation {
  return moduleStatuses.get(state) ?? unknown;
}
