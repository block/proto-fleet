import type { Meta, StoryObj } from "@storybook/react-vite";

import ActiveUpdateBanners from "./ActiveUpdateBanners";
import ChannelHistoryModal from "./ChannelHistoryModal";
import {
  activeRigRollout,
  batchedRigRollout,
  canaryChannel,
  canaryHistory,
  gatedRigRollout,
  pausedRigRollout,
} from "./ReleaseChannels.fixtures";

// Inline progress banners for ongoing firmware updates, stacked above the
// firmware page tabs; and the per-channel update history they lead to.
const meta = {
  title: "Proto Fleet/Firmware/Release Channels/Active Updates",
  component: ActiveUpdateBanners,
  parameters: {
    layout: "padded",
  },
} satisfies Meta<typeof ActiveUpdateBanners>;

export default meta;

type Story = StoryObj<typeof ActiveUpdateBanners>;

const noop = () => {};

export const Stacked: Story = {
  name: "Running, gated, failing and paused updates",
  render: () => (
    <div className="max-w-4xl">
      <ActiveUpdateBanners
        rollouts={[activeRigRollout, gatedRigRollout, batchedRigRollout, pausedRigRollout]}
        onViewUpdate={noop}
      />
    </div>
  ),
};

export const History: Story = {
  name: "Channel update history",
  render: () => (
    <div className="min-h-screen bg-surface-base">
      <ChannelHistoryModal
        channel={canaryChannel}
        rollouts={canaryHistory}
        onView={noop}
        onRollback={noop}
        onClose={noop}
      />
    </div>
  ),
};
