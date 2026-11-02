import { type ReactElement, useMemo } from "react";
import clsx from "clsx";

import { type CurtailmentHealth, type CurtailmentSource } from "@/protoFleet/api/curtailmentTypes";

import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import Switch from "@/shared/components/Switch";
import "./CurtailmentSettingsPage.css";

const curtailmentSourceCols = {
  name: "name",
  lastSignalValue: "lastSignalValue",
  lastSignalUpdate: "lastSignalUpdate",
  health: "health",
  enabled: "enabled",
} as const;

type CurtailmentSourceColumn = (typeof curtailmentSourceCols)[keyof typeof curtailmentSourceCols];

const activeCurtailmentSourceCols: CurtailmentSourceColumn[] = [
  curtailmentSourceCols.name,
  curtailmentSourceCols.lastSignalValue,
  curtailmentSourceCols.lastSignalUpdate,
  curtailmentSourceCols.health,
  curtailmentSourceCols.enabled,
];

const curtailmentSourceColTitles: ColTitles<CurtailmentSourceColumn> = {
  name: "Name",
  lastSignalValue: "Last signal",
  lastSignalUpdate: "Updated",
  health: "Connection",
  enabled: "",
};

const curtailmentSourceColumnAriaLabels: Partial<Record<CurtailmentSourceColumn, string>> = {
  enabled: "Enabled",
};

const curtailmentSourceColumnsExemptFromDisabledStyling = new Set<CurtailmentSourceColumn>([
  curtailmentSourceCols.enabled,
]);

const curtailmentSourcesTableClassName = [
  "mb-2 w-full",
  "phone:table-fixed",
  "phone:[&_thead_th:last-child]:w-14",
  "phone:[&_thead_th:last-child>div]:w-14",
  "phone:[&_tbody_td[data-testid=enabled]:last-child>div:first-child]:box-border",
  "phone:[&_tbody_td[data-testid=enabled]:last-child>div:first-child]:flex",
  "phone:[&_tbody_td[data-testid=enabled]:last-child>div:first-child]:justify-end",
  "phone:[&_tbody_td[data-testid=enabled]:last-child>div:first-child]:w-14",
].join(" ");

const sourceHealthDotClassName: Record<CurtailmentHealth, string> = {
  connected: "bg-intent-success-fill",
  waitingForSignal: "bg-intent-warning-fill",
  noSignal: "bg-intent-critical-fill",
  offline: "bg-intent-critical-fill",
};

const sourceHealthLabel: Record<CurtailmentHealth, string> = {
  connected: "Connected",
  waitingForSignal: "Waiting for signal",
  noSignal: "No signal",
  offline: "Offline",
};

function formatSourceHealth(health: CurtailmentSource["health"]): string {
  return sourceHealthLabel[health];
}

type CurtailmentSourceColConfigOptions = {
  onToggle: (sourceId: string) => void;
  updatingSourceIds: ReadonlySet<string>;
  canManageSources: boolean;
};

function createCurtailmentSourceColConfig({
  onToggle,
  updatingSourceIds,
  canManageSources,
}: CurtailmentSourceColConfigOptions): ColConfig<CurtailmentSource, string, CurtailmentSourceColumn> {
  return {
    [curtailmentSourceCols.name]: {
      component: (source) => (
        <span className="block max-w-full truncate text-emphasis-300 text-text-primary">{source.name}</span>
      ),
      width: "w-[23.5%] phone:w-auto",
    },
    [curtailmentSourceCols.lastSignalValue]: {
      component: (source) => <span className="truncate text-text-primary">{source.lastTarget}</span>,
      width: "w-[23.5%] phone:w-auto",
    },
    [curtailmentSourceCols.lastSignalUpdate]: {
      component: (source) => <span className="truncate text-text-primary">{source.lastSeen}</span>,
      width: "w-[23.5%] phone:hidden",
    },
    [curtailmentSourceCols.health]: {
      allowWrap: true,
      component: (source) => (
        <div>
          <div className="inline-flex items-center gap-1.5">
            <span
              className={clsx(
                "curtailment-source-health-dot h-2 w-2 shrink-0 rounded-full",
                sourceHealthDotClassName[source.health],
                source.health === "connected" && "curtailment-source-health-dot--connected",
              )}
            />
            <span className="truncate text-text-primary">{formatSourceHealth(source.health)}</span>
          </div>
          {source.lastRuntimeError ? (
            <div className="text-200 wrap-anywhere whitespace-normal text-text-critical">{source.lastRuntimeError}</div>
          ) : null}
        </div>
      ),
      width: "w-[23.5%] phone:w-auto",
    },
    [curtailmentSourceCols.enabled]: {
      component: (source) => (
        <div className="flex justify-end" data-interactive>
          <Switch
            checked={source.enabled}
            setChecked={() => onToggle(source.id)}
            disabled={!canManageSources || updatingSourceIds.has(source.id)}
          />
        </div>
      ),
      width: "w-[6%] phone:w-14",
    },
  };
}

export function CurtailmentSourcesTable({
  sources,
  onToggle,
  updatingSourceIds,
  canManageSources,
  noDataElement,
  onEdit,
}: CurtailmentSourceColConfigOptions & {
  sources: CurtailmentSource[];
  noDataElement: ReactElement;
  onEdit: (source: CurtailmentSource) => void;
}): ReactElement {
  const sourceColConfig = useMemo(
    () => createCurtailmentSourceColConfig({ onToggle, updatingSourceIds, canManageSources }),
    [onToggle, updatingSourceIds, canManageSources],
  );
  return (
    <List<CurtailmentSource, string, CurtailmentSourceColumn>
      activeCols={activeCurtailmentSourceCols}
      colTitles={curtailmentSourceColTitles}
      columnHeaderAriaLabels={curtailmentSourceColumnAriaLabels}
      colConfig={sourceColConfig}
      items={sources}
      itemKey="id"
      total={sources.length}
      hideTotal
      itemName={{ singular: "source", plural: "sources" }}
      stickyFirstColumn={false}
      isRowDisabled={(source) => !source.enabled}
      columnsExemptFromDisabledStyling={curtailmentSourceColumnsExemptFromDisabledStyling}
      tableClassName={curtailmentSourcesTableClassName}
      noDataElement={noDataElement}
      applyColumnWidthsToCells
      onRowClick={canManageSources ? onEdit : undefined}
    />
  );
}
