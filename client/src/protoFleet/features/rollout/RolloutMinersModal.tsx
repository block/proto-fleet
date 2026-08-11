import type { ReactElement } from "react";

import RolloutColumnState from "./RolloutColumnState";
import {
  type RolloutMetricDeltaIntent,
  rolloutMetricDeltaIntent,
  rolloutProcessLabel,
  rolloutStatusColumnLabel,
} from "./rolloutDisplayUtils";
import type { RolloutEvent, RolloutMetricUnit, RolloutMinerRow, RolloutMinerTelemetryValue } from "./rolloutTypes";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import Modal from "@/shared/components/Modal";

interface RolloutMinersModalProps {
  open: boolean;
  event: RolloutEvent;
  miners: RolloutMinerRow[];
  onDismiss: () => void;
}

type RolloutMinerColumn =
  "miner" | "rollout" | "type" | "ipAddress" | "hashrate" | "power" | "efficiency" | "temperature" | "errors";

const rolloutMinerColumns: RolloutMinerColumn[] = [
  "miner",
  "rollout",
  "type",
  "ipAddress",
  "hashrate",
  "power",
  "efficiency",
  "temperature",
  "errors",
];

function rolloutMinerColTitles(event: RolloutEvent): ColTitles<RolloutMinerColumn> {
  return {
    miner: "Miner",
    rollout: rolloutStatusColumnLabel(event.processType),
    type: "Type",
    ipAddress: "IP address",
    hashrate: "Hashrate",
    power: "Power",
    efficiency: "Efficiency",
    temperature: "Temp",
    errors: "Errors",
  };
}

function MinerCell({ miner }: { miner: RolloutMinerRow }): ReactElement {
  return (
    <span className="text-emphasis-300 break-words text-text-primary" title={miner.name}>
      {miner.name}
    </span>
  );
}

function RolloutCell({ event, miner }: { event: RolloutEvent; miner: RolloutMinerRow }): ReactElement {
  return <RolloutColumnState phase={miner.phase} processType={event.processType} />;
}

function TextCell({ value }: { value: string }): ReactElement {
  return (
    <span className="break-words text-text-primary" title={value}>
      {value}
    </span>
  );
}

function ErrorsCell({ metric }: { metric: RolloutMinerTelemetryValue }): ReactElement {
  const hasErrors = metric.value !== "0";

  return (
    <span
      className={hasErrors ? "break-words text-intent-critical-fill" : "break-words text-text-primary"}
      title={metric.value}
    >
      {metric.value}
    </span>
  );
}

const deltaTextColor: Record<RolloutMetricDeltaIntent, string> = {
  positive: "text-intent-success-fill",
  negative: "text-intent-critical-fill",
  neutral: "text-text-primary-50",
};

function deltaIntent(unit: RolloutMetricUnit, delta: string): RolloutMetricDeltaIntent {
  const normalized = delta.trim().toLowerCase();
  if (normalized === "offline") {
    return "negative";
  }
  if (normalized.startsWith("+")) {
    return rolloutMetricDeltaIntent(unit, 1);
  }
  if (normalized.startsWith("-") || normalized.startsWith("−")) {
    return rolloutMetricDeltaIntent(unit, -1);
  }
  return "neutral";
}

function MetricCell({ metric, unit }: { metric: RolloutMinerTelemetryValue; unit: RolloutMetricUnit }): ReactElement {
  const deltaColor = metric.delta ? deltaTextColor[deltaIntent(unit, metric.delta)] : "";

  return (
    <span
      className="flex min-w-0 flex-wrap items-baseline gap-x-2 gap-y-1 text-text-primary"
      title={metric.delta ? `${metric.value} ${metric.delta}` : metric.value}
    >
      <span className="whitespace-nowrap">{metric.value}</span>
      {metric.delta ? <span className={`whitespace-nowrap ${deltaColor}`}>{metric.delta}</span> : null}
    </span>
  );
}

function createRolloutMinerColConfig(event: RolloutEvent): ColConfig<RolloutMinerRow, string, RolloutMinerColumn> {
  return {
    miner: {
      component: (miner) => <MinerCell miner={miner} />,
      width: "w-[132px]",
    },
    rollout: {
      component: (miner) => <RolloutCell event={event} miner={miner} />,
      width: "w-[150px]",
    },
    type: {
      component: (miner) => <TextCell value={miner.type} />,
      width: "w-[132px]",
    },
    ipAddress: {
      component: (miner) => <TextCell value={miner.ipAddress} />,
      width: "w-[116px]",
    },
    hashrate: {
      component: (miner) => <MetricCell metric={miner.hashrate} unit="hashrate" />,
      width: "w-[128px]",
    },
    power: {
      component: (miner) => <MetricCell metric={miner.power} unit="power" />,
      width: "w-[112px]",
    },
    efficiency: {
      component: (miner) => <MetricCell metric={miner.efficiency} unit="efficiency" />,
      width: "w-[128px]",
    },
    temperature: {
      component: (miner) => <MetricCell metric={miner.temperature} unit="temperature" />,
      width: "w-[112px]",
    },
    errors: {
      component: (miner) => <ErrorsCell metric={miner.errors} />,
      width: "w-[96px]",
    },
  };
}

function RolloutMinersModal({ open, event, miners, onDismiss }: RolloutMinersModalProps): ReactElement | null {
  if (!open) {
    return null;
  }

  const inScope = Math.max(event.totalTargets - event.excludedTargets, 0);
  const description = `${miners.length.toLocaleString()} of ${inScope.toLocaleString()} included miners shown`;

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
        <List<RolloutMinerRow, string, RolloutMinerColumn>
          activeCols={rolloutMinerColumns}
          colTitles={rolloutMinerColTitles(event)}
          colConfig={createRolloutMinerColConfig(event)}
          items={miners}
          itemKey="id"
          total={miners.length}
          itemName={{ singular: "miner", plural: "miners" }}
          containerClassName="min-h-0"
          tableClassName="mb-0 w-full !table-fixed"
          applyColumnWidthsToCells
          stickyFirstColumn={false}
          emptyStateRow={<div className="py-10 text-center text-300 text-text-primary-70">No miners to show.</div>}
        />
      </div>
    </Modal>
  );
}

export default RolloutMinersModal;
