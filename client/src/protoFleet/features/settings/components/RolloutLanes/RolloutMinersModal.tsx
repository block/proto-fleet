import { type ReactElement, useMemo, useState } from "react";
import clsx from "clsx";

import {
  attentionDevices,
  type DeltaIntent,
  deviceNeedsAttention,
  metricDisplay,
  type MetricKind,
  scopeDevices,
} from "./rolloutStatus";
import { type Rollout, type RolloutDevice, RolloutDeviceState } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { useTemperatureUnit } from "@/protoFleet/store";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import SegmentedControl from "@/shared/components/SegmentedControl";
import StatusCircle, { statuses } from "@/shared/components/StatusCircle";

export type RolloutMinerFilter = "all" | "attention";

type MinerColumn = "miner" | "firmware" | "hashrate" | "power" | "efficiency" | "temperature";

const minerColumns: MinerColumn[] = ["miner", "firmware", "hashrate", "power", "efficiency", "temperature"];

const minerColTitles: ColTitles<MinerColumn> = {
  miner: "Miner",
  firmware: "Firmware",
  hashrate: "Hashrate",
  power: "Power",
  efficiency: "Efficiency",
  temperature: "Temp",
};

const deltaTextColor: Record<DeltaIntent, string> = {
  positive: "text-intent-success-fill",
  negative: "text-intent-critical-fill",
  neutral: "text-text-primary-50",
};

interface MinerRow {
  id: string;
  device: RolloutDevice;
  name: string;
  attention: boolean;
}

function MinerCell({ row, model }: { row: MinerRow; model: string }): ReactElement {
  const detail = [model, row.device.ipAddress].filter(Boolean).join(" · ");
  return (
    <span className="flex min-w-0 flex-col gap-1">
      <span className="text-emphasis-300 break-words text-text-primary" title={row.name}>
        {row.name}
      </span>
      {detail ? (
        <span className="text-200 break-words text-text-primary-50" title={detail}>
          {detail}
        </span>
      ) : null}
    </span>
  );
}

// Per-miner state for the Firmware column: the shared StatusCircle dot, an
// inline spinner while in flight, and the same wording the fleet table uses.
function FirmwareCell({ row, targetVersion }: { row: MinerRow; targetVersion: string }): ReactElement {
  const { device } = row;
  const dot = (status: keyof typeof statuses) => (
    <StatusCircle status={status} variant="simple" width="w-[6px]" testId="rollout-column-status" />
  );
  let state: ReactElement;
  if (row.attention) {
    state = (
      <span className="flex items-center gap-2 text-text-primary">
        {dot(statuses.error)}
        Needs attention
      </span>
    );
  } else if (device.state === RolloutDeviceState.UPDATED) {
    state = (
      <span className="flex items-center gap-2 text-text-primary">
        {dot(statuses.normal)}
        {targetVersion}
      </span>
    );
  } else if (device.state === RolloutDeviceState.VERIFYING) {
    state = (
      <span className="flex items-center gap-2 text-text-primary">
        {dot(statuses.warning)}
        <ProgressCircular size={14} indeterminate />
        Verifying
      </span>
    );
  } else if (device.state === RolloutDeviceState.UPDATING) {
    state = (
      <span className="flex items-center gap-2 text-text-primary">
        {dot(statuses.error)}
        <ProgressCircular size={14} indeterminate />
        Updating firmware
      </span>
    );
  } else {
    state = (
      <span className="flex items-center gap-2 text-text-primary-70">
        {dot(statuses.inactive)}
        {device.firmwareVersion ? `Queued (${device.firmwareVersion})` : "Queued"}
      </span>
    );
  }

  const reasons: string[] = [];
  if (device.openErrors > device.baselineOpenErrors) {
    const added = device.openErrors - device.baselineOpenErrors;
    reasons.push(added === 1 ? "1 new error" : `${added} new errors`);
  }
  if (row.attention && !device.online)
    reasons.push(device.status ? `Device ${device.status.toLowerCase()}` : "Offline");
  else if (row.attention && device.baselineHashing && !device.hashing) reasons.push("Not hashing");

  return (
    <span className="flex min-w-0 flex-col gap-1">
      {state}
      {reasons.length > 0 ? (
        <span className="text-200 break-words text-intent-critical-fill">{reasons.join("; ")}</span>
      ) : null}
    </span>
  );
}

function MetricCell({ device, kind }: { device: RolloutDevice; kind: MetricKind }): ReactElement {
  const temperatureUnit = useTemperatureUnit();
  const metric =
    kind === "hashrate"
      ? device.hashRateHs
      : kind === "power"
        ? device.powerW
        : kind === "efficiency"
          ? device.efficiencyJh
          : device.tempC;
  const display = metricDisplay(kind, metric, temperatureUnit);
  return (
    <span className="flex min-w-0 flex-col gap-1 text-text-primary" title={`${display.value} ${display.delta ?? ""}`}>
      <span className="break-words">{display.value}</span>
      {display.delta ? (
        <span className={clsx("text-200 break-words", deltaTextColor[display.deltaIntent])}>{display.delta}</span>
      ) : null}
    </span>
  );
}

interface RolloutMinersModalProps {
  rollout: Rollout;
  // deviceIdentifier -> display name.
  minerNames: Record<string, string>;
  initialFilter?: RolloutMinerFilter;
  onClose: () => void;
}

// Miner drill-down for an update: every targeted miner (or just those that
// need attention) with its update state and telemetry against baseline.
const RolloutMinersModal = ({ rollout, minerNames, initialFilter = "all", onClose }: RolloutMinersModalProps) => {
  const [filter, setFilter] = useState<RolloutMinerFilter>(initialFilter);

  const rows = useMemo<MinerRow[]>(
    () =>
      rollout.devices.map((device) => ({
        id: device.deviceIdentifier,
        device,
        name: minerNames[device.deviceIdentifier] ?? device.deviceIdentifier,
        attention: deviceNeedsAttention(device, rollout.firmwareVersion),
      })),
    [rollout, minerNames],
  );
  const visibleRows = filter === "attention" ? rows.filter((row) => row.attention) : rows;
  const attentionCount = attentionDevices(rollout).length;
  const summary =
    filter === "attention"
      ? `${attentionCount.toLocaleString()} ${attentionCount === 1 ? "miner needs" : "miners need"} attention`
      : `${rows.length.toLocaleString()} miners in this update, ${scopeDevices(rollout).length.toLocaleString()} in the current scope`;

  const colConfig: ColConfig<MinerRow, string, MinerColumn> = {
    miner: { component: (row) => <MinerCell row={row} model={rollout.model} />, width: "w-[220px]", allowWrap: true },
    firmware: {
      component: (row) => <FirmwareCell row={row} targetVersion={rollout.firmwareVersion} />,
      width: "w-[220px]",
      allowWrap: true,
    },
    hashrate: {
      component: (row) => <MetricCell device={row.device} kind="hashrate" />,
      width: "w-[128px]",
      allowWrap: true,
    },
    power: { component: (row) => <MetricCell device={row.device} kind="power" />, width: "w-[112px]", allowWrap: true },
    efficiency: {
      component: (row) => <MetricCell device={row.device} kind="efficiency" />,
      width: "w-[128px]",
      allowWrap: true,
    },
    temperature: {
      component: (row) => <MetricCell device={row.device} kind="temperature" />,
      width: "w-[112px]",
      allowWrap: true,
    },
  };

  return (
    <Modal
      open
      onDismiss={onClose}
      title="Miners in firmware update"
      size="large"
      className="flex !h-[calc(100dvh-(--spacing(32)))] max-h-[calc(100dvh-(--spacing(32)))] flex-col !overflow-hidden"
      bodyClassName="flex flex-1 min-h-0 flex-col"
      divider={false}
      testId="rollout-miners-modal"
      buttons={[{ text: "Done", variant: "primary", onClick: onClose, dismissModalOnClick: false }]}
    >
      <div className="flex min-h-0 flex-1 flex-col gap-4">
        <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
          <SegmentedControl
            segments={[
              { key: "all", title: "All miners" },
              { key: "attention", title: "Needs attention" },
            ]}
            initialSegmentKey={initialFilter}
            onSelect={(key) => setFilter(key as RolloutMinerFilter)}
          />
          <div className="text-200 text-text-primary-70">{summary}</div>
        </div>
        <List<MinerRow, string, MinerColumn>
          activeCols={minerColumns}
          colTitles={minerColTitles}
          colConfig={colConfig}
          items={visibleRows}
          itemKey="id"
          total={visibleRows.length}
          itemName={{ singular: "miner", plural: "miners" }}
          containerClassName="min-h-0"
          tableClassName="mb-0 w-full !table-fixed"
          applyColumnWidthsToCells
          stickyFirstColumn={false}
          emptyStateRow={
            <div className="py-10 text-center text-300 text-text-primary-70">
              {filter === "attention" ? "No miners need attention." : "No miners to show."}
            </div>
          }
        />
      </div>
    </Modal>
  );
};

export default RolloutMinersModal;
