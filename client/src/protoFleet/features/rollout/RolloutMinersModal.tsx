import { type ReactElement, useMemo, useState } from "react";

import RolloutColumnState from "./RolloutColumnState";
import {
  rolloutErrorCount,
  rolloutErrorImpactCount,
  type RolloutMetricDeltaIntent,
  rolloutMetricDeltaIntent,
  rolloutProcessLabel,
  rolloutStatusColumnLabel,
} from "./rolloutDisplayUtils";
import type {
  CurtailmentTelemetryPhase,
  RolloutEvent,
  RolloutMetricUnit,
  RolloutMinerRow,
  RolloutMinerTelemetryValue,
  RolloutProcessType,
} from "./rolloutTypes";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import Modal from "@/shared/components/Modal";
import SegmentedControl from "@/shared/components/SegmentedControl";

interface RolloutMinersModalProps {
  open: boolean;
  event: RolloutEvent;
  miners: RolloutMinerRow[];
  initialFilter?: RolloutMinerFilter;
  onDismiss: () => void;
}

export type RolloutMinerFilter = "all" | "errors";

type RolloutMinerColumn = "miner" | "rollout" | "hashrate" | "power" | "efficiency" | "temperature";

const rolloutMinerColumns: RolloutMinerColumn[] = [
  "miner",
  "rollout",
  "hashrate",
  "power",
  "efficiency",
  "temperature",
];

function rolloutMinerColTitles(event: RolloutEvent): ColTitles<RolloutMinerColumn> {
  return {
    miner: "Miner",
    rollout: rolloutStatusColumnLabel(event.processType),
    hashrate: "Hashrate",
    power: "Power",
    efficiency: "Efficiency",
    temperature: "Temp",
  };
}

function MinerCell({ miner }: { miner: RolloutMinerRow }): ReactElement {
  return (
    <span className="flex min-w-0 flex-col gap-1">
      <span className="text-emphasis-300 break-words text-text-primary" title={miner.name}>
        {miner.name}
      </span>
      <span className="text-200 break-words text-text-primary-50" title={`${miner.type} · ${miner.ipAddress}`}>
        {miner.type} · {miner.ipAddress}
      </span>
    </span>
  );
}

function RolloutCell({
  event,
  miner,
  messages,
}: {
  event: RolloutEvent;
  miner: RolloutMinerRow;
  messages: string[];
}): ReactElement {
  const errorDetail = messages.length > 0 ? messages.join("; ") : null;

  return (
    <span className="flex min-w-0 flex-col gap-1">
      <RolloutColumnState phase={miner.phase} processType={event.processType} />
      {errorDetail ? (
        <span className="text-200 break-words text-intent-critical-fill" title={errorDetail}>
          {errorDetail}
        </span>
      ) : null}
    </span>
  );
}

const deltaTextColor: Record<RolloutMetricDeltaIntent, string> = {
  positive: "text-intent-success-fill",
  negative: "text-intent-critical-fill",
  neutral: "text-text-primary-50",
};

function deltaIntent(
  unit: RolloutMetricUnit,
  delta: string,
  processType: RolloutProcessType,
  curtailmentTelemetryPhase?: CurtailmentTelemetryPhase,
): RolloutMetricDeltaIntent {
  const normalized = delta.trim().toLowerCase();
  if (normalized === "offline") {
    return "negative";
  }
  if (normalized.startsWith("+")) {
    return rolloutMetricDeltaIntent(unit, 1, processType, curtailmentTelemetryPhase);
  }
  if (normalized.startsWith("-") || normalized.startsWith("−")) {
    return rolloutMetricDeltaIntent(unit, -1, processType, curtailmentTelemetryPhase);
  }
  return "neutral";
}

function MetricCell({
  metric,
  unit,
  processType,
  curtailmentTelemetryPhase,
}: {
  metric: RolloutMinerTelemetryValue;
  unit: RolloutMetricUnit;
  processType: RolloutProcessType;
  curtailmentTelemetryPhase?: CurtailmentTelemetryPhase;
}): ReactElement {
  const deltaColor = metric.delta
    ? deltaTextColor[deltaIntent(unit, metric.delta, processType, curtailmentTelemetryPhase)]
    : "";
  const title = metric.delta ? `${metric.value} ${metric.delta}` : metric.value;

  return (
    <span className="flex min-w-0 flex-col gap-1 text-text-primary" title={title}>
      <span className="break-words">{metric.value}</span>
      {metric.delta ? <span className={`text-200 break-words ${deltaColor}`}>{metric.delta}</span> : null}
    </span>
  );
}

function createMinerErrorMessageMap(event: RolloutEvent): Map<string, string[]> {
  const messagesByMiner = new Map<string, string[]>();
  event.errors?.forEach((error) => {
    error.impactedMiners.forEach((minerName) => {
      const messages = messagesByMiner.get(minerName) ?? [];
      messages.push(error.message);
      messagesByMiner.set(minerName, messages);
    });
  });
  return messagesByMiner;
}

function hasMinerErrors(miner: RolloutMinerRow, messagesByMiner: Map<string, string[]>): boolean {
  return (messagesByMiner.get(miner.name)?.length ?? 0) > 0;
}

function createRolloutMinerColConfig(
  event: RolloutEvent,
  messagesByMiner: Map<string, string[]>,
): ColConfig<RolloutMinerRow, string, RolloutMinerColumn> {
  return {
    miner: {
      component: (miner) => <MinerCell miner={miner} />,
      width: "w-[220px]",
      allowWrap: true,
    },
    rollout: {
      component: (miner) => (
        <RolloutCell event={event} miner={miner} messages={messagesByMiner.get(miner.name) ?? []} />
      ),
      width: "w-[220px]",
      allowWrap: true,
    },
    hashrate: {
      component: (miner) => (
        <MetricCell
          metric={miner.hashrate}
          unit="hashrate"
          processType={event.processType}
          curtailmentTelemetryPhase={event.curtailmentTelemetryPhase}
        />
      ),
      width: "w-[128px]",
      allowWrap: true,
    },
    power: {
      component: (miner) => (
        <MetricCell
          metric={miner.power}
          unit="power"
          processType={event.processType}
          curtailmentTelemetryPhase={event.curtailmentTelemetryPhase}
        />
      ),
      width: "w-[112px]",
      allowWrap: true,
    },
    efficiency: {
      component: (miner) => (
        <MetricCell
          metric={miner.efficiency}
          unit="efficiency"
          processType={event.processType}
          curtailmentTelemetryPhase={event.curtailmentTelemetryPhase}
        />
      ),
      width: "w-[128px]",
      allowWrap: true,
    },
    temperature: {
      component: (miner) => (
        <MetricCell
          metric={miner.temperature}
          unit="temperature"
          processType={event.processType}
          curtailmentTelemetryPhase={event.curtailmentTelemetryPhase}
        />
      ),
      width: "w-[112px]",
      allowWrap: true,
    },
  };
}

function RolloutMinersModal({
  open,
  event,
  miners,
  initialFilter = "all",
  onDismiss,
}: RolloutMinersModalProps): ReactElement | null {
  const [filter, setFilter] = useState<RolloutMinerFilter>(initialFilter);
  const messagesByMiner = useMemo(() => createMinerErrorMessageMap(event), [event]);
  const errorCount = rolloutErrorCount(event.errors);
  const impactedMinerCount = rolloutErrorImpactCount(event.errors);
  const minersWithErrors = useMemo(
    () => miners.filter((miner) => hasMinerErrors(miner, messagesByMiner)),
    [messagesByMiner, miners],
  );
  const visibleMiners = filter === "errors" ? minersWithErrors : miners;

  if (!open) {
    return null;
  }

  const inScope = Math.max(event.totalTargets - event.excludedTargets, 0);
  const description =
    filter === "errors"
      ? `${visibleMiners.length.toLocaleString()} miners with errors, ${errorCount.toLocaleString()} ${errorCount === 1 ? "error" : "errors"}`
      : `${visibleMiners.length.toLocaleString()} of ${inScope.toLocaleString()} included miners shown`;

  return (
    <Modal
      open={open}
      onDismiss={onDismiss}
      title={`Miners in ${rolloutProcessLabel(event.processType).toLowerCase()}`}
      description={description}
      size="large"
      className="flex !h-[calc(100dvh-(--spacing(32)))] max-h-[calc(100dvh-(--spacing(32)))] flex-col !overflow-hidden"
      bodyClassName="flex flex-1 min-h-0 flex-col"
      divider={false}
      testId="rollout-miners-modal"
      buttons={[
        {
          text: "Done",
          variant: "primary",
          onClick: onDismiss,
          dismissModalOnClick: false,
        },
      ]}
    >
      <div className="flex h-full min-h-0 flex-col gap-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <SegmentedControl
            segments={[
              { key: "all", title: "All miners" },
              { key: "errors", title: "Errors" },
            ]}
            initialSegmentKey={initialFilter}
            onSelect={(key) => setFilter(key as RolloutMinerFilter)}
          />
          {errorCount > 0 ? (
            <div className="text-200 text-text-primary-70">
              {errorCount.toLocaleString()} {errorCount === 1 ? "error" : "errors"} affecting{" "}
              {impactedMinerCount.toLocaleString()} {impactedMinerCount === 1 ? "miner" : "miners"}
            </div>
          ) : null}
        </div>
        <List<RolloutMinerRow, string, RolloutMinerColumn>
          activeCols={rolloutMinerColumns}
          colTitles={rolloutMinerColTitles(event)}
          colConfig={createRolloutMinerColConfig(event, messagesByMiner)}
          items={visibleMiners}
          itemKey="id"
          total={visibleMiners.length}
          itemName={{ singular: "miner", plural: "miners" }}
          containerClassName="min-h-0"
          tableClassName="mb-0 w-full !table-fixed"
          applyColumnWidthsToCells
          stickyFirstColumn={false}
          emptyStateRow={
            <div className="py-10 text-center text-300 text-text-primary-70">
              {filter === "errors" ? "No miners with errors to show." : "No miners to show."}
            </div>
          }
        />
      </div>
    </Modal>
  );
}

export default RolloutMinersModal;
