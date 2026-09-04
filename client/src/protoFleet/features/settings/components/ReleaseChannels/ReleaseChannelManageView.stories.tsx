import type { ReactNode } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import ReleaseChannelManageView from "./ReleaseChannelManageView";
import {
  activeRigRollout,
  batchedRigRollout,
  canaryChannel,
  canaryChannelSettled,
  canaryHistory,
  canaryPreview,
  completedRigRollout,
  conflictingPreview,
  emptyChannel,
  firmwareFiles,
  listChannelMinersFixture,
  listRolloutDevicesFixture,
  minerNames,
  productionChannel,
} from "./ReleaseChannels.fixtures";

// The per-channel management surface behind "Manage": General, Applies to
// and Update behavior are saved together; Firmware is assigned per model and
// starts an update paced by the saved behavior. The same view creates a
// channel when no channel is passed.
const meta = {
  title: "Proto Fleet/Firmware/Release Channels/Manage View",
  component: ReleaseChannelManageView,
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof ReleaseChannelManageView>;

export default meta;

type Story = StoryObj<typeof ReleaseChannelManageView>;

const resolveTo = (preview: typeof canaryPreview) => () => Promise.resolve(preview);
const settle = () => Promise.resolve();

const Frame = ({ children }: { children: ReactNode }) => (
  <div className="min-h-screen bg-surface-base p-6">
    <div className="max-w-5xl">{children}</div>
  </div>
);

export const UpdateInProgress: Story = {
  name: "Rig update in progress",
  render: () => (
    <Frame>
      <ReleaseChannelManageView
        channel={canaryChannel}
        rollouts={canaryHistory}
        firmwareFiles={firmwareFiles}
        minerNames={minerNames}
        previewScope={resolveTo(canaryPreview)}
        listChannelMiners={listChannelMinersFixture}
        listRolloutDevices={listRolloutDevicesFixture}
        onSave={settle}
        onDelete={() => {}}
        onApply={settle}
      />
    </Frame>
  ),
};

export const BatchReviewWithFailure: Story = {
  name: "Batch review with a failed miner",
  render: () => (
    <Frame>
      <ReleaseChannelManageView
        channel={{ ...canaryChannel, behavior: batchedRigRollout.behavior }}
        rollouts={[batchedRigRollout, completedRigRollout]}
        firmwareFiles={firmwareFiles}
        minerNames={minerNames}
        previewScope={resolveTo(canaryPreview)}
        listChannelMiners={listChannelMinersFixture}
        listRolloutDevices={listRolloutDevicesFixture}
        onSave={settle}
        onDelete={() => {}}
        onApply={settle}
      />
    </Frame>
  ),
};

export const Settled: Story = {
  name: "Every model up to date",
  render: () => (
    <Frame>
      <ReleaseChannelManageView
        channel={canaryChannelSettled}
        rollouts={[completedRigRollout]}
        firmwareFiles={firmwareFiles}
        minerNames={minerNames}
        previewScope={resolveTo(canaryPreview)}
        listChannelMiners={listChannelMinersFixture}
        listRolloutDevices={listRolloutDevicesFixture}
        onSave={settle}
        onDelete={() => {}}
        onApply={settle}
      />
    </Frame>
  ),
};

export const ScopeOverlap: Story = {
  name: "Scope overlaps another channel",
  render: () => (
    <Frame>
      <ReleaseChannelManageView
        channel={productionChannel}
        rollouts={[]}
        firmwareFiles={firmwareFiles}
        minerNames={minerNames}
        previewScope={resolveTo(conflictingPreview)}
        listChannelMiners={listChannelMinersFixture}
        listRolloutDevices={listRolloutDevicesFixture}
        onSave={settle}
        onDelete={() => {}}
        onApply={settle}
      />
    </Frame>
  ),
};

export const EmptyChannel: Story = {
  name: "Empty channel",
  render: () => (
    <Frame>
      <ReleaseChannelManageView
        channel={emptyChannel}
        rollouts={[]}
        firmwareFiles={firmwareFiles}
        minerNames={minerNames}
        previewScope={resolveTo({ ...canaryPreview, minerCount: 0, models: [] })}
        listChannelMiners={listChannelMinersFixture}
        listRolloutDevices={listRolloutDevicesFixture}
        onSave={settle}
        onDelete={() => {}}
        onApply={settle}
      />
    </Frame>
  ),
};

export const Create: Story = {
  name: "Create a release channel",
  render: () => (
    <Frame>
      <ReleaseChannelManageView
        rollouts={[activeRigRollout]}
        firmwareFiles={firmwareFiles}
        minerNames={minerNames}
        previewScope={resolveTo(canaryPreview)}
        listChannelMiners={listChannelMinersFixture}
        listRolloutDevices={listRolloutDevicesFixture}
        onSave={settle}
        onApply={settle}
      />
    </Frame>
  ),
};
