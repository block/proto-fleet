import { useMemo, useState } from "react";
import clsx from "clsx";
import { timestampMs } from "@bufbuild/protobuf/wkt";

import { StatusCell } from "./channelStatus";
import { channelUpdateStatus, modelFirmwareLabel, modelUpdateStatus } from "./rolloutStatus";
import {
  type Rollout,
  type RolloutLane,
  type RolloutLaneModelGroup,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { ChevronDown } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";

interface ChannelsTableProps {
  lanes: RolloutLane[];
  rollouts: Rollout[];
  onCreate: () => void;
  onManage: (lane: RolloutLane) => void;
}

interface ChannelRow {
  id: string;
  kind: "channel";
  lane: RolloutLane;
}

interface ModelRow {
  id: string;
  kind: "model";
  lane: RolloutLane;
  group: RolloutLaneModelGroup;
}

type ChannelTableRow = ChannelRow | ModelRow;
type ChannelColumn = "name" | "miners" | "firmware" | "status" | "actions";

const channelColumns: ChannelColumn[] = ["name", "miners", "firmware", "status", "actions"];

const channelColTitles: ColTitles<ChannelColumn> = {
  name: "Release channel",
  miners: "Miners",
  firmware: "Firmware",
  status: "Update status",
  actions: "",
};

const laneModelKey = (laneId: bigint, model: string) => `${laneId.toString()}:${model}`;

// Newest completed rollout per (lane, model), for "Updated <date>" cells.
function lastCompletedByLaneModel(rollouts: Rollout[]): Map<string, Rollout> {
  const latest = new Map<string, Rollout>();
  for (const rollout of rollouts) {
    if (rollout.status !== RolloutStatus.COMPLETED || !rollout.finishedAt) continue;
    const key = laneModelKey(rollout.laneId, rollout.model);
    const current = latest.get(key);
    if (!current?.finishedAt || timestampMs(rollout.finishedAt) > timestampMs(current.finishedAt)) {
      latest.set(key, rollout);
    }
  }
  return latest;
}

// The release channels overview on the shared List: one disclosure row per
// channel with aggregate counts, and per-model rows carrying firmware targets
// and update state, per the firmware release channels design.
const ChannelsTable = ({ lanes, rollouts, onCreate, onManage }: ChannelsTableProps) => {
  const [expandedLaneIds, setExpandedLaneIds] = useState(() => new Set<string>());

  const activeByLaneModel = useMemo(
    () =>
      new Map(
        rollouts
          .filter((rollout) => rollout.status === RolloutStatus.ACTIVE)
          .map((rollout) => [laneModelKey(rollout.laneId, rollout.model), rollout]),
      ),
    [rollouts],
  );
  const lastCompleted = useMemo(() => lastCompletedByLaneModel(rollouts), [rollouts]);

  const rows = useMemo<ChannelTableRow[]>(
    () =>
      lanes.flatMap((lane) => {
        const laneKey = lane.id.toString();
        return [
          { id: laneKey, kind: "channel" as const, lane },
          ...(expandedLaneIds.has(laneKey)
            ? lane.modelGroups.map((group) => ({
                id: `${laneKey}:${group.model}`,
                kind: "model" as const,
                lane,
                group,
              }))
            : []),
        ];
      }),
    [lanes, expandedLaneIds],
  );

  const toggleLane = (laneKey: string) => {
    setExpandedLaneIds((current) => {
      const next = new Set(current);
      if (next.has(laneKey)) next.delete(laneKey);
      else next.add(laneKey);
      return next;
    });
  };

  const laneActiveRollouts = (lane: RolloutLane): Rollout[] =>
    lane.modelGroups
      .map((group) => activeByLaneModel.get(laneModelKey(lane.id, group.model)))
      .filter((rollout): rollout is Rollout => rollout !== undefined);

  const colConfig: ColConfig<ChannelTableRow, string, ChannelColumn> = {
    name: {
      component: (row) => {
        if (row.kind === "channel") {
          const laneKey = row.lane.id.toString();
          const isExpanded = expandedLaneIds.has(laneKey);
          return (
            <button
              type="button"
              aria-expanded={isExpanded}
              aria-label={`${isExpanded ? "Collapse" : "Expand"} ${row.lane.name} models`}
              data-testid={`channel-toggle-${row.lane.name}`}
              className="flex min-w-0 cursor-pointer items-center gap-2 text-emphasis-300 text-text-primary"
              onClick={() => toggleLane(laneKey)}
            >
              <ChevronDown width="w-3" className={clsx("shrink-0 transition-transform", !isExpanded && "-rotate-90")} />
              <span className="truncate" data-testid={`channel-row-${row.lane.name}`}>
                {row.lane.name}
              </span>
            </button>
          );
        }
        return (
          <span
            className="ml-5 block min-w-0 truncate border-l border-border-5 pl-5 text-300 text-text-primary"
            data-testid={`model-row-${row.lane.name}-${row.group.model}`}
          >
            {row.group.model || "Unknown model"}
          </span>
        );
      },
      width: "w-72",
    },
    miners: {
      component: (row) =>
        row.kind === "channel" ? (
          <span data-testid={`channel-miners-${row.lane.name}`}>
            {row.lane.modelGroups.reduce((sum, group) => sum + group.miners.length, 0).toLocaleString()}
          </span>
        ) : (
          row.group.miners.length.toLocaleString()
        ),
      width: "w-32",
    },
    firmware: {
      component: (row) =>
        row.kind === "channel"
          ? `${row.lane.modelGroups.length} ${row.lane.modelGroups.length === 1 ? "model" : "models"}`
          : modelFirmwareLabel(row.group),
      width: "w-64",
    },
    status: {
      component: (row) =>
        row.kind === "channel" ? (
          <StatusCell
            status={channelUpdateStatus(laneActiveRollouts(row.lane))}
            emphasized
            testId={`channel-status-${row.lane.name}`}
          />
        ) : (
          <StatusCell
            status={modelUpdateStatus(
              row.group,
              activeByLaneModel.get(laneModelKey(row.lane.id, row.group.model)),
              lastCompleted.get(laneModelKey(row.lane.id, row.group.model)),
            )}
            testId={`model-status-${row.lane.name}-${row.group.model}`}
          />
        ),
      width: "w-64",
    },
    actions: {
      component: (row) =>
        row.kind === "channel" ? (
          <div className="flex justify-end">
            <Button
              ariaLabel={`Manage ${row.lane.name}`}
              text="Manage"
              variant={variants.secondary}
              size={sizes.compact}
              onClick={() => onManage(row.lane)}
              testId={`manage-channel-${row.lane.name}`}
            />
          </div>
        ) : null,
      width: "w-32",
    },
  };

  return (
    <div className="flex flex-col gap-6" data-testid="channels-table">
      <div>
        <Button
          variant={variants.primary}
          size={sizes.compact}
          text="Create release channel"
          onClick={onCreate}
          className="phone:w-full"
        />
      </div>
      <List<ChannelTableRow, string, ChannelColumn>
        activeCols={channelColumns}
        colTitles={channelColTitles}
        colConfig={colConfig}
        items={rows}
        itemKey="id"
        total={lanes.length}
        itemName={{ singular: "release channel", plural: "release channels" }}
        applyColumnWidthsToCells
        stickyFirstColumn={false}
      />
      <div className="text-300 text-text-primary-70">
        Expand a release channel to inspect each model's last or current update.
      </div>
    </div>
  );
};

export default ChannelsTable;
