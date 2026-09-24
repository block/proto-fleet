import { type ReactNode, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import ReleaseChannelManageView from "./ReleaseChannelManageView";
import {
  activeRigRollout,
  batchedRigRollout,
  canaryChannel,
  canaryChannelSettled,
  canaryHistory,
  canaryPreview,
  channelWithActiveRollout,
  completedRigRollout,
  conflictingPreview,
  emptyChannel,
  firmwareFiles,
  listChannelMinersFixture,
  listRolloutDevicesFixture,
  minerNames,
  productionChannel,
} from "./ReleaseChannels.fixtures";
import ReleaseChannelsTable from "./ReleaseChannelsTable";
import Button, { sizes, variants } from "@/shared/components/Button";

// Manage prioritizes assigned miners; channel settings and creation use modals.
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
        channel={{
          ...channelWithActiveRollout(canaryChannel, batchedRigRollout),
          behavior: batchedRigRollout.behavior,
        }}
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
  name: "Rig miners up to date",
  render: () => (
    <Frame>
      <ReleaseChannelManageView
        channel={canaryChannelSettled}
        rollouts={[]}
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
        channel={canaryChannel}
        rollouts={[activeRigRollout]}
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
  render: function CreateStory() {
    const [isCreating, setIsCreating] = useState(true);
    return (
      <Frame>
        <div className="flex flex-col gap-6" inert={isCreating}>
          <div className="flex">
            <Button
              variant={variants.primary}
              size={sizes.compact}
              text="Create release channel"
              onClick={() => setIsCreating(true)}
              className="shrink-0 phone:w-full"
              testId="create-release-channel"
            />
          </div>
          <ReleaseChannelsTable
            channels={[canaryChannel, productionChannel]}
            rollouts={[activeRigRollout]}
            onManage={() => {}}
          />
        </div>
        {isCreating ? (
          <ReleaseChannelManageView
            rollouts={[activeRigRollout]}
            firmwareFiles={firmwareFiles}
            minerNames={minerNames}
            previewScope={resolveTo(canaryPreview)}
            listChannelMiners={listChannelMinersFixture}
            listRolloutDevices={listRolloutDevicesFixture}
            onCancelCreate={() => setIsCreating(false)}
            onSave={async () => setIsCreating(false)}
            onApply={settle}
          />
        ) : null}
      </Frame>
    );
  },
};
