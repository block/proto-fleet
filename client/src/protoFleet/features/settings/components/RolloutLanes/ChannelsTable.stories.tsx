import type { ReactNode } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import ChannelsTable from "./ChannelsTable";
import { activeRigRollout, canaryChannel, emptyChannel, productionChannel } from "./RolloutChannels.fixtures";

// The release channels overview table: expandable channel rows with
// aggregate miner counts and update status, per-model sub-rows with firmware
// transitions ("1.4.3 → 1.4.4") and live update state, and a Manage action
// per channel. Rows expand interactively in the story.
const meta = {
  title: "Proto Fleet/Firmware/Rollout Channels/Channels Table",
  component: ChannelsTable,
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof ChannelsTable>;

export default meta;

type Story = StoryObj<typeof ChannelsTable>;

const noop = () => {};

const Frame = ({ children }: { children: ReactNode }) => (
  <div className="min-h-screen bg-surface-base p-6">{children}</div>
);

export const WithActiveUpdate: Story = {
  name: "With an update in progress",
  render: () => (
    <Frame>
      <ChannelsTable
        lanes={[canaryChannel, productionChannel, emptyChannel]}
        rollouts={[activeRigRollout]}
        onManage={noop}
      />
    </Frame>
  ),
};

export const AllQuiet: Story = {
  name: "No active updates",
  render: () => (
    <Frame>
      <ChannelsTable lanes={[productionChannel, emptyChannel]} rollouts={[]} onManage={noop} />
    </Frame>
  ),
};
