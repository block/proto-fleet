import type { ReactNode } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  activeRigRollout,
  batchedRigRollout,
  canaryChannel,
  completedRigRollout,
  completedWithFailuresRigRollout,
  emptyChannel,
  gatedRigRollout,
  productionChannel,
} from "./ReleaseChannels.fixtures";
import ReleaseChannelsTable from "./ReleaseChannelsTable";

// The release channels overview on the shared List: disclosure rows per
// channel with miner counts and a roll-up of active updates ("2 updating, 1
// needs attention"), per-model rows with firmware transitions ("1.4.3 →
// 1.4.4") and update state, and a Manage action per channel. Rows expand
// interactively in the story.
const meta = {
  title: "Proto Fleet/Firmware/Release Channels/Channels Table",
  component: ReleaseChannelsTable,
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof ReleaseChannelsTable>;

export default meta;

type Story = StoryObj<typeof ReleaseChannelsTable>;

const noop = () => {};

const Frame = ({ children }: { children: ReactNode }) => (
  <div className="min-h-screen bg-surface-base p-6">{children}</div>
);

export const WithActiveUpdate: Story = {
  name: "With an update in progress",
  render: () => (
    <Frame>
      <ReleaseChannelsTable
        channels={[canaryChannel, productionChannel, emptyChannel]}
        rollouts={[activeRigRollout, completedRigRollout]}
        onCreate={noop}
        onManage={noop}
      />
    </Frame>
  ),
};

export const ReviewNeeded: Story = {
  name: "With a pilot batch awaiting review",
  render: () => (
    <Frame>
      <ReleaseChannelsTable
        channels={[canaryChannel, productionChannel]}
        rollouts={[gatedRigRollout, completedRigRollout]}
        onCreate={noop}
        onManage={noop}
      />
    </Frame>
  ),
};

export const WithFailures: Story = {
  name: "With failed miners",
  render: () => (
    <Frame>
      <ReleaseChannelsTable
        channels={[canaryChannel, productionChannel]}
        rollouts={[batchedRigRollout, completedWithFailuresRigRollout]}
        onCreate={noop}
        onManage={noop}
      />
    </Frame>
  ),
};

export const AllSettled: Story = {
  name: "Everything up to date",
  render: () => (
    <Frame>
      <ReleaseChannelsTable
        channels={[productionChannel, emptyChannel]}
        rollouts={[completedRigRollout]}
        onCreate={noop}
        onManage={noop}
      />
    </Frame>
  ),
};
