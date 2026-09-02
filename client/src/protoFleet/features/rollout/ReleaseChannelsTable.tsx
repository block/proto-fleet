import { type ReactElement, useMemo, useState } from "react";
import clsx from "clsx";

import type { ReleaseChannel, ReleaseChannelModelCohort, ReleaseChannelUpdateTone } from "./releaseChannelTypes";
import { ChevronDown } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";

interface ReleaseChannelsTableProps {
  channels: ReleaseChannel[];
  onCreate: () => void;
  onManage: (channel: ReleaseChannel) => void;
  initiallyExpandedChannelIds?: string[];
}

interface ChannelTableRow {
  id: string;
  kind: "channel";
  channel: ReleaseChannel;
}

interface ModelTableRow {
  id: string;
  kind: "model";
  channelId: string;
  cohort: ReleaseChannelModelCohort;
}

type ReleaseChannelTableRow = ChannelTableRow | ModelTableRow;
type ReleaseChannelColumn = "name" | "miners" | "firmware" | "status" | "actions";

const releaseChannelColumns: ReleaseChannelColumn[] = ["name", "miners", "firmware", "status", "actions"];

const releaseChannelColTitles: ColTitles<ReleaseChannelColumn> = {
  name: "Release channel",
  miners: "Miners",
  firmware: "Firmware",
  status: "Update status",
  actions: "",
};

const statusDotClasses: Record<ReleaseChannelUpdateTone, string> = {
  attention: "bg-intent-critical-fill",
  active: "bg-core-primary-fill",
  completed: "bg-intent-success-fill",
  none: "bg-core-primary-20",
};

function UpdateStatus({
  label,
  tone,
  emphasized = false,
}: {
  label: string;
  tone: ReleaseChannelUpdateTone;
  emphasized?: boolean;
}): ReactElement {
  return (
    <span className={clsx("flex items-center gap-2 text-text-primary", emphasized && "text-emphasis-300")}>
      <span className={clsx("size-2 shrink-0 rounded-full", statusDotClasses[tone])} />
      {label}
    </span>
  );
}

function firmwareLabel(cohort: ReleaseChannelModelCohort): string {
  return cohort.previousVersion ? `${cohort.previousVersion} → ${cohort.currentVersion}` : cohort.currentVersion;
}

/** Release channels tab body with model-level disclosure rows. */
function ReleaseChannelsTable({
  channels,
  onCreate,
  onManage,
  initiallyExpandedChannelIds = [],
}: ReleaseChannelsTableProps): ReactElement {
  const [expandedChannelIds, setExpandedChannelIds] = useState(() => new Set(initiallyExpandedChannelIds));
  const rows = useMemo<ReleaseChannelTableRow[]>(
    () =>
      channels.flatMap((channel) => [
        { id: channel.id, kind: "channel" as const, channel },
        ...(expandedChannelIds.has(channel.id)
          ? channel.modelCohorts.map((cohort) => ({
              id: cohort.id,
              kind: "model" as const,
              channelId: channel.id,
              cohort,
            }))
          : []),
      ]),
    [channels, expandedChannelIds],
  );

  const toggleChannel = (channelId: string): void => {
    setExpandedChannelIds((current) => {
      const next = new Set(current);
      if (next.has(channelId)) next.delete(channelId);
      else next.add(channelId);
      return next;
    });
  };

  const releaseChannelColConfig: ColConfig<ReleaseChannelTableRow, string, ReleaseChannelColumn> = {
    name: {
      component: (row) =>
        row.kind === "channel" ? (
          <button
            type="button"
            aria-expanded={expandedChannelIds.has(row.channel.id)}
            aria-label={`${expandedChannelIds.has(row.channel.id) ? "Collapse" : "Expand"} ${row.channel.name} models`}
            className="flex min-w-0 cursor-pointer items-center gap-2 text-emphasis-300 text-text-primary"
            onClick={() => toggleChannel(row.channel.id)}
          >
            <ChevronDown
              width="w-3"
              className={clsx("shrink-0 transition-transform", !expandedChannelIds.has(row.channel.id) && "-rotate-90")}
            />
            <span className="truncate">{row.channel.name}</span>
          </button>
        ) : (
          <span className="ml-5 block min-w-0 truncate border-l border-border-5 pl-5 text-300 text-text-primary">
            {row.cohort.model}
          </span>
        ),
      width: "w-72",
    },
    miners: {
      component: (row) => (row.kind === "channel" ? row.channel.minerCount : row.cohort.minerCount).toLocaleString(),
      width: "w-32",
    },
    firmware: {
      component: (row) =>
        row.kind === "channel"
          ? `${row.channel.modelCohorts.length} ${row.channel.modelCohorts.length === 1 ? "model" : "models"}`
          : firmwareLabel(row.cohort),
      width: "w-64",
    },
    status: {
      component: (row) => (
        <UpdateStatus
          label={row.kind === "channel" ? row.channel.updateStatus : row.cohort.updateStatus}
          tone={row.kind === "channel" ? row.channel.updateTone : row.cohort.updateTone}
          emphasized={row.kind === "channel"}
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
            />
          </div>
        ) : null,
      width: "w-32",
    },
  };

  return (
    <div className="flex flex-col gap-6">
      <div>
        <Button variant={variants.primary} size={sizes.compact} text="Create release channel" onClick={onCreate} />
      </div>
      <List<ReleaseChannelTableRow, string, ReleaseChannelColumn>
        activeCols={releaseChannelColumns}
        colTitles={releaseChannelColTitles}
        colConfig={releaseChannelColConfig}
        items={rows}
        itemKey="id"
        total={channels.length}
        itemName={{ singular: "release channel", plural: "release channels" }}
        applyColumnWidthsToCells
        stickyFirstColumn={false}
      />
      <div className="text-300 text-text-primary-70">
        Expand a release channel to inspect each model cohort's last or current update.
      </div>
    </div>
  );
}

export default ReleaseChannelsTable;
