import type { ReactNode } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import ChannelsTable from "./ChannelsTable";
import {
  activeRigRollout,
  batchedRigRollout,
  canaryChannel,
  completedRigRollout,
  emptyChannel,
  gatedRigRollout,
  productionChannel,
} from "./RolloutChannels.fixtures";

// The release channels overview on the shared List: disclosure rows per
// channel with aggregate miner counts and a roll-up of active updates
// ("2 active, 1 needs attention"), per-model rows with firmware transitions
// ("1.4.3 → 1.4.4") and update state, and a Manage action per channel. Rows
// expand interactively in the story.
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
        rollouts={[activeRigRollout, completedRigRollout]}
        onCreate={noop}
        onManage={noop}
      />
    </Frame>
  ),
};

export const NeedsAttention: Story = {
  name: "With an update needing attention",
  render: () => (
    <Frame>
      <ChannelsTable
        lanes={[canaryChannel, productionChannel]}
        rollouts={[batchedRigRollout, completedRigRollout]}
        onCreate={noop}
        onManage={noop}
      />
    </Frame>
  ),
};

export const ReviewNeeded: Story = {
  name: "With a pilot awaiting review",
  render: () => (
    <Frame>
      <ChannelsTable
        lanes={[canaryChannel, productionChannel]}
        rollouts={[gatedRigRollout, completedRigRollout]}
        onCreate={noop}
        onManage={noop}
      />
    </Frame>
  ),
};

export const AllQuiet: Story = {
  name: "No active updates",
  render: () => (
    <Frame>
      <ChannelsTable
        lanes={[productionChannel, emptyChannel]}
        rollouts={[completedRigRollout]}
        onCreate={noop}
        onManage={noop}
      />
    </Frame>
  ),
};
