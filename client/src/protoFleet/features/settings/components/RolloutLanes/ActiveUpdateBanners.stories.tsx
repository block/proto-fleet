import type { ReactNode } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import ActiveUpdateBanners from "./ActiveUpdateBanners";
import {
  activeRigRollout,
  activeS19Rollout,
  batchedRigRollout,
  gatedRigRollout,
  pausedRigRollout,
} from "./RolloutChannels.fixtures";

// The banner stack for ongoing firmware updates, one Callout per rollout,
// stacked above the firmware page tabs. Each carries progress, miners
// needing attention and the current step, and opens the update detail.
const meta = {
  title: "Proto Fleet/Firmware/Rollout Channels/Active Update Banners",
  component: ActiveUpdateBanners,
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof ActiveUpdateBanners>;

export default meta;

type Story = StoryObj<typeof ActiveUpdateBanners>;

const noop = () => {};

const Frame = ({ children }: { children: ReactNode }) => (
  <div className="min-h-screen bg-surface-base p-6">{children}</div>
);

export const SingleUpdate: Story = {
  name: "One update running",
  render: () => (
    <Frame>
      <ActiveUpdateBanners rollouts={[activeRigRollout]} onViewUpdate={noop} />
    </Frame>
  ),
};

export const ConcurrentUpdates: Story = {
  name: "Concurrent updates",
  render: () => (
    <Frame>
      <ActiveUpdateBanners rollouts={[batchedRigRollout, activeS19Rollout]} onViewUpdate={noop} />
    </Frame>
  ),
};

export const ReviewAndPaused: Story = {
  name: "Awaiting review and paused",
  render: () => (
    <Frame>
      <ActiveUpdateBanners rollouts={[gatedRigRollout, pausedRigRollout]} onViewUpdate={noop} />
    </Frame>
  ),
};
