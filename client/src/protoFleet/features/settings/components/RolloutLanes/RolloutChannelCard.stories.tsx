import type { ReactNode } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  activeRigRollout,
  canaryChannel,
  canaryChannelSettled,
  emptyChannel,
  firmwareFiles,
  minerNames,
} from "./RolloutChannels.fixtures";
import { LaneCard } from "./RolloutLanesTab";

// The channel card as rendered in Settings → Firmware → Rollout channels:
// per-model rows with the firmware picker and "View miners", staged-change
// banner, and in-card rollout progress. Picking a different firmware in the
// story stages it live; "Apply changes" resolves after a short delay.
const meta = {
  title: "Proto Fleet/Firmware/Rollout Channels/Channel Card",
  component: LaneCard,
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof LaneCard>;

export default meta;

type Story = StoryObj<typeof LaneCard>;

const noop = () => {};
const applyAfterDelay = () => new Promise<void>((resolve) => setTimeout(resolve, 800));

const Frame = ({ children }: { children: ReactNode }) => (
  <div className="min-h-screen bg-surface-base p-6">{children}</div>
);

export const RolloutInProgress: Story = {
  name: "Rollout in progress",
  render: () => (
    <Frame>
      <LaneCard
        lane={canaryChannel}
        rollouts={[activeRigRollout]}
        firmwareFiles={firmwareFiles}
        minerNames={minerNames}
        onManageMiners={noop}
        onShowHistory={noop}
        onDelete={noop}
        onApply={applyAfterDelay}
      />
    </Frame>
  ),
};

export const IdleCompliant: Story = {
  name: "Idle, all miners compliant",
  render: () => (
    <Frame>
      <LaneCard
        lane={canaryChannelSettled}
        rollouts={[]}
        firmwareFiles={firmwareFiles}
        minerNames={minerNames}
        onManageMiners={noop}
        onShowHistory={noop}
        onDelete={noop}
        onApply={applyAfterDelay}
      />
    </Frame>
  ),
};

export const EmptyChannel: Story = {
  name: "Empty channel",
  render: () => (
    <Frame>
      <LaneCard
        lane={emptyChannel}
        rollouts={[]}
        firmwareFiles={firmwareFiles}
        minerNames={minerNames}
        onManageMiners={noop}
        onShowHistory={noop}
        onDelete={noop}
        onApply={applyAfterDelay}
      />
    </Frame>
  ),
};
