import type { ReactElement } from "react";

import RolloutColumnState from "./RolloutColumnState";
import { rolloutProcessLabel, rolloutStatusColumnLabel } from "./rolloutDisplayUtils";
import type { RolloutEvent, RolloutMinerRow, RolloutMinerTelemetryValue } from "./rolloutTypes";
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
  "miner" | "rollout" | "type" | "ipAddress" | "hashrate" | "power" | "efficiency" | "temperature";

const rolloutMinerColumns: RolloutMinerColumn[] = [
  "miner",
  "rollout",
  "type",
  "ipAddress",
  "hashrate",
  "power",
  "efficiency",
  "temperature",
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
  };
}

function MinerCell({ miner }: { miner: RolloutMinerRow }): ReactElement {
  return (
    <span className="truncate text-emphasis-300 text-text-primary" title={miner.name}>
      {miner.name}
    </span>
  );
}

function RolloutCell({ event, miner }: { event: RolloutEvent; miner: RolloutMinerRow }): ReactElement {
  return <RolloutColumnState phase={miner.phase} processType={event.processType} />;
}

function TextCell({ value }: { value: string }): ReactElement {
  return (
    <span className="truncate text-text-primary" title={value}>
      {value}
    </span>
  );
}

function deltaTextColor(delta: string): string {
  if (delta.startsWith("+")) {
    return "text-intent-success-fill";
  }

  if (delta.startsWith("-")) {
    return "text-intent-critical-fill";
  }

  return "text-text-primary-50";
}

function MetricCell({ metric }: { metric: RolloutMinerTelemetryValue }): ReactElement {
  return (
    <span
      className="flex min-w-0 items-baseline gap-2 text-text-primary"
      title={metric.delta ? `${metric.value} ${metric.delta}` : metric.value}
    >
      <span className="min-w-0 truncate">{metric.value}</span>
      {metric.delta ? <span className={`shrink-0 ${deltaTextColor(metric.delta)}`}>{metric.delta}</span> : null}
    </span>
  );
}

function createRolloutMinerColConfig(event: RolloutEvent): ColConfig<RolloutMinerRow, string, RolloutMinerColumn> {
  return {
    miner: {
      component: (miner) => <MinerCell miner={miner} />,
      width: "w-40",
    },
    rollout: {
      component: (miner) => <RolloutCell event={event} miner={miner} />,
      width: "w-40",
    },
    type: {
      component: (miner) => <TextCell value={miner.type} />,
      width: "w-40",
    },
    ipAddress: {
      component: (miner) => <TextCell value={miner.ipAddress} />,
      width: "w-36",
    },
    hashrate: {
      component: (miner) => <MetricCell metric={miner.hashrate} />,
      width: "w-36",
    },
    power: {
      component: (miner) => <MetricCell metric={miner.power} />,
      width: "w-32",
    },
    efficiency: {
      component: (miner) => <MetricCell metric={miner.efficiency} />,
      width: "w-36",
    },
    temperature: {
      component: (miner) => <MetricCell metric={miner.temperature} />,
      width: "w-32",
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
          tableClassName="mb-0 inline-table w-max !min-w-fit !table-fixed"
          applyColumnWidthsToCells
          stickyFirstColumn={false}
          emptyStateRow={<div className="py-10 text-center text-300 text-text-primary-70">No miners to show.</div>}
        />
      </div>
    </Modal>
  );
}

export default RolloutMinersModal;
