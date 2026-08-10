import type { ReactElement } from "react";

import type { ReleaseChannel } from "./releaseChannelTypes";
import Button, { sizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";

interface ReleaseChannelsTableProps {
  channels: ReleaseChannel[];
  onCreate: () => void;
  onManage: (channel: ReleaseChannel) => void;
}

type ReleaseChannelColumn = "name" | "miners" | "releases" | "lastUpdated" | "actions";

const releaseChannelColumns: ReleaseChannelColumn[] = ["name", "miners", "releases", "lastUpdated", "actions"];

const releaseChannelColTitles: ColTitles<ReleaseChannelColumn> = {
  name: "Name",
  miners: "Miners",
  releases: "Releases",
  lastUpdated: "Last updated",
  actions: "",
};

/** Release channels tab body. */
function ReleaseChannelsTable({ channels, onCreate, onManage }: ReleaseChannelsTableProps): ReactElement {
  const releaseChannelColConfig: ColConfig<ReleaseChannel, string, ReleaseChannelColumn> = {
    name: {
      component: (channel) => <span className="text-emphasis-300 text-text-primary">{channel.name}</span>,
      width: "w-64",
    },
    miners: { component: (channel) => channel.minerCount.toLocaleString(), width: "w-48" },
    releases: {
      component: (channel) => `${channel.releaseCount} ${channel.releaseCount === 1 ? "file" : "files"}`,
      width: "w-48",
    },
    lastUpdated: { component: (channel) => channel.lastUpdated, width: "w-64" },
    actions: {
      component: (channel) => (
        <div className="flex justify-end">
          <Button
            ariaLabel={`Manage ${channel.name}`}
            text="Manage"
            variant={variants.secondary}
            size={sizes.compact}
            onClick={() => onManage(channel)}
          />
        </div>
      ),
      width: "w-32",
    },
  };

  return (
    <div className="flex flex-col gap-6">
      <div>
        <Button variant={variants.primary} size={sizes.compact} text="Create release channel" onClick={onCreate} />
      </div>
      <List<ReleaseChannel, string, ReleaseChannelColumn>
        activeCols={releaseChannelColumns}
        colTitles={releaseChannelColTitles}
        colConfig={releaseChannelColConfig}
        items={channels}
        itemKey="id"
        total={channels.length}
        itemName={{ singular: "release channel", plural: "release channels" }}
        applyColumnWidthsToCells
        stickyFirstColumn={false}
      />
      <div className="text-300 text-text-primary-70">
        Release channels define which firmware versions miners receive.
      </div>
    </div>
  );
}

export default ReleaseChannelsTable;
