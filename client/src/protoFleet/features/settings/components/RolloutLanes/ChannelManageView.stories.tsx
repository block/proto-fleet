import type { ReactNode } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import ChannelManageView from "./ChannelManageView";
import {
  activeRigRollout,
  canaryChannel,
  canaryChannelSettled,
  emptyChannel,
  firmwareFiles,
  minerNames,
} from "./RolloutChannels.fixtures";

// The per-channel management surface behind a channel's "Manage" action:
// per-model table rows with the firmware picker, update status, and "View
// miners", live progress under updating rows, and the staged-change banner.
// Picking a different firmware in the story stages it live; "Apply changes"
// resolves after a short delay.
const meta = {
  title: "Proto Fleet/Firmware/Rollout Channels/Manage Channel",
  component: ChannelManageView,
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof ChannelManageView>;

export default meta;

type Story = StoryObj<typeof ChannelManageView>;

const noop = () => {};
const applyAfterDelay = () => new Promise<void>((resolve) => setTimeout(resolve, 800));

const Frame = ({ children }: { children: ReactNode }) => (
  <div className="min-h-screen bg-surface-base p-6">{children}</div>
);

export const RolloutInProgress: Story = {
  name: "Rollout in progress",
  render: () => (
    <Frame>
      <ChannelManageView
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
      <ChannelManageView
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
      <ChannelManageView
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
