import { moduleStatusLabels } from "./moduleListFilters";
import type { ModuleColumn, ModuleListItem } from "./moduleListModel";
import ModuleTemperature from "./ModuleTemperature";
import type { ModuleStatus } from "@/protoOS/features/tanks/tankView";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import StatusCircle from "@/shared/components/StatusCircle";
import type { StatusCircleStatus } from "@/shared/components/StatusCircle/constants";
import {
  convertWtoKW,
  formatEfficiencyOrDash,
  formatHashrateOrDash,
  formatPowerKwOrDash,
} from "@/shared/utils/telemetryFormat";

const statusColors: Record<ModuleStatus, StatusCircleStatus> = {
  healthy: "normal",
  attention: "error",
  offline: "inactive",
  sleeping: "sleeping",
  isolated: "inactive",
  unknown: "pending",
};
export const moduleColTitles: ColTitles<ModuleColumn> = {
  name: "Name",
  position: "Position",
  status: "Status",
  issues: "Issues",
  hashrate: "Hashrate",
  temperature: "Temperature",
  power: "Power",
  efficiency: "Efficiency",
};
export const moduleColConfig: ColConfig<ModuleListItem, string, ModuleColumn> = {
  name: { width: "w-[200px]", component: (row) => <span className="text-emphasis-300">{row.name}</span> },
  position: { width: "w-[120px]" },
  status: {
    width: "w-[200px]",
    component: (row) => (
      <span className="flex items-center gap-2">
        <StatusCircle status={statusColors[row.status]} variant="simple" width="w-2" />
        {moduleStatusLabels[row.status]}
      </span>
    ),
  },
  issues: {
    width: "w-[170px]",
    component: (row) =>
      row.issueCount === null
        ? "Unknown"
        : row.issueCount === 0
          ? "—"
          : `${row.issueCount}${row.issuesComplete ? "" : "+"} ${row.issueCount === 1 ? "issue" : "issues"}`,
  },
  hashrate: {
    width: "w-[150px]",
    component: (row) => <span className="tabular-nums">{formatHashrateOrDash(row.hashrate)}</span>,
  },
  temperature: {
    width: "w-[150px]",
    component: (row) => <ModuleTemperature value={row.temperature} />,
  },
  power: {
    width: "w-[150px]",
    component: (row) => (
      <span className="tabular-nums">{formatPowerKwOrDash(row.power === null ? null : convertWtoKW(row.power))}</span>
    ),
  },
  efficiency: {
    width: "w-[150px]",
    component: (row) => <span className="tabular-nums">{formatEfficiencyOrDash(row.efficiency)}</span>,
  },
};
