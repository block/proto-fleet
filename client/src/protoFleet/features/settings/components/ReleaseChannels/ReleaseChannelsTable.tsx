import { useMemo, useState } from "react";
import clsx from "clsx";
import { timestampMs } from "@bufbuild/protobuf/wkt";

import { StatusCell } from "./channelStatus";
import { channelUpdateStatus, isActive, modelFirmwareLabel, modelUpdateStatus } from "./rolloutStatus";
import type {
  ReleaseChannel,
  ReleaseChannelModelGroup,
  Rollout,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { RolloutStatus } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { ChevronDown } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";

interface ReleaseChannelsTableProps {
  channels: ReleaseChannel[];
  rollouts: Rollout[];
  onCreate: () => void;
  onManage: (channel: ReleaseChannel) => void;
}

interface ChannelRow {
  id: string;
  kind: "channel";
  channel: ReleaseChannel;
}

interface ModelRow {
  id: string;
  kind: "model";
  channel: ReleaseChannel;
  group: ReleaseChannelModelGroup;
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

const channelModelKey = (channelId: bigint, model: string) => `${channelId.toString()}:${model}`;

// Newest finished (completed or completed with failures) rollout per
// (channel, model), for "Updated <date>" and "N failed" cells.
function lastFinishedByChannelModel(rollouts: Rollout[]): Map<string, Rollout> {
  const latest = new Map<string, Rollout>();
  for (const rollout of rollouts) {
    const finished =
      rollout.status === RolloutStatus.COMPLETED || rollout.status === RolloutStatus.COMPLETED_WITH_FAILURES;
    if (!finished || !rollout.finishedAt) continue;
    const key = channelModelKey(rollout.channelId, rollout.model);
    const current = latest.get(key);
    if (!current?.finishedAt || timestampMs(rollout.finishedAt) > timestampMs(current.finishedAt)) {
      latest.set(key, rollout);
    }
  }
  return latest;
}

// The release channels overview on the shared List: one disclosure row per
// channel with aggregate counts, and per-model rows carrying firmware targets
// and update state.
const ReleaseChannelsTable = ({ channels, rollouts, onCreate, onManage }: ReleaseChannelsTableProps) => {
  const [expandedChannelIds, setExpandedChannelIds] = useState(() => new Set<string>());

  const activeByChannelModel = useMemo(
    () =>
      new Map(rollouts.filter(isActive).map((rollout) => [channelModelKey(rollout.channelId, rollout.model), rollout])),
    [rollouts],
  );
  const lastFinished = useMemo(() => lastFinishedByChannelModel(rollouts), [rollouts]);

  const rows = useMemo<ChannelTableRow[]>(
    () =>
      channels.flatMap((channel) => {
        const channelKey = channel.id.toString();
        return [
          { id: channelKey, kind: "channel" as const, channel },
          ...(expandedChannelIds.has(channelKey)
            ? channel.modelGroups.map((group) => ({
                id: `${channelKey}:${group.model}`,
                kind: "model" as const,
                channel,
                group,
              }))
            : []),
        ];
      }),
    [channels, expandedChannelIds],
  );

  const toggleChannel = (channelKey: string) => {
    setExpandedChannelIds((current) => {
      const next = new Set(current);
      if (next.has(channelKey)) next.delete(channelKey);
      else next.add(channelKey);
      return next;
    });
  };

  const channelActiveRollouts = (channel: ReleaseChannel): Rollout[] =>
    channel.modelGroups
      .map((group) => activeByChannelModel.get(channelModelKey(channel.id, group.model)))
      .filter((rollout): rollout is Rollout => rollout !== undefined);

  const colConfig: ColConfig<ChannelTableRow, string, ChannelColumn> = {
    name: {
      component: (row) => {
        if (row.kind === "channel") {
          const channelKey = row.channel.id.toString();
          const isExpanded = expandedChannelIds.has(channelKey);
          return (
            <button
              type="button"
              aria-expanded={isExpanded}
              aria-label={`${isExpanded ? "Collapse" : "Expand"} ${row.channel.name} models`}
              data-testid={`channel-toggle-${row.channel.name}`}
              className="flex min-w-0 cursor-pointer items-center gap-2 text-emphasis-300 text-text-primary"
              onClick={() => toggleChannel(channelKey)}
            >
              <ChevronDown width="w-3" className={clsx("shrink-0 transition-transform", !isExpanded && "-rotate-90")} />
              <span className="truncate" data-testid={`channel-row-${row.channel.name}`}>
                {row.channel.name}
              </span>
            </button>
          );
        }
        return (
          <span
            className="ml-5 block min-w-0 truncate border-l border-border-5 pl-5 text-300 text-text-primary"
            data-testid={`model-row-${row.channel.name}-${row.group.model}`}
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
          <span data-testid={`channel-miners-${row.channel.name}`}>{row.channel.minerCount.toLocaleString()}</span>
        ) : (
          row.group.minerCount.toLocaleString()
        ),
      width: "w-32",
    },
    firmware: {
      component: (row) =>
        row.kind === "channel"
          ? `${row.channel.modelGroups.length} ${row.channel.modelGroups.length === 1 ? "model" : "models"}`
          : modelFirmwareLabel(row.group),
      width: "w-64",
    },
    status: {
      component: (row) =>
        row.kind === "channel" ? (
          <StatusCell
            status={channelUpdateStatus(channelActiveRollouts(row.channel))}
            emphasized
            testId={`channel-status-${row.channel.name}`}
          />
        ) : (
          <StatusCell
            status={modelUpdateStatus(
              row.group,
              activeByChannelModel.get(channelModelKey(row.channel.id, row.group.model)),
              lastFinished.get(channelModelKey(row.channel.id, row.group.model)),
            )}
            testId={`model-status-${row.channel.name}-${row.group.model}`}
          />
        ),
      width: "w-64",
    },
    actions: {
      component: (row) =>
        row.kind === "channel" ? (
          <div className="flex justify-end">
            <Button
              ariaLabel={`Manage ${row.channel.name}`}
              text="Manage"
              variant={variants.secondary}
              size={sizes.compact}
              onClick={() => onManage(row.channel)}
              testId={`manage-channel-${row.channel.name}`}
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
          testId="create-release-channel"
        />
      </div>
      <List<ChannelTableRow, string, ChannelColumn>
        activeCols={channelColumns}
        colTitles={channelColTitles}
        colConfig={colConfig}
        items={rows}
        itemKey="id"
        total={channels.length}
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

export default ReleaseChannelsTable;
