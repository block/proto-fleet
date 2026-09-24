import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  activeRigRollout,
  batchedRigRollout,
  gatedRigRollout,
  listRolloutDevicesFixture,
  minerNames,
  pausedRigRollout,
} from "./ReleaseChannels.fixtures";
import RolloutLiveView from "./RolloutLiveView";

const settle = () => Promise.resolve();
const noop = () => {};

const meta = {
  title: "Proto Fleet/Firmware/Release Channels/Active Update Card",
  component: RolloutLiveView,
  parameters: { layout: "fullscreen" },
  decorators: [
    (Story) => (
      <div className="min-h-screen bg-surface-base p-6 tablet:p-10">
        <Story />
      </div>
    ),
  ],
  args: {
    presentation: "inline",
    rollout: activeRigRollout,
    currentGeneration: activeRigRollout.assignmentGeneration,
    minerNames,
    listRolloutDevices: listRolloutDevicesFixture,
    onContinue: settle,
    onPause: settle,
    onResume: settle,
    onCancel: noop,
    onRollback: noop,
    onRetryFailed: settle,
    onManage: noop,
    onViewUpdate: noop,
  },
} satisfies Meta<typeof RolloutLiveView>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Updating: Story = {};
export const PilotReview: Story = { args: { rollout: gatedRigRollout } };
export const HoldingForReview: Story = { args: { rollout: batchedRigRollout } };
export const Paused: Story = { args: { rollout: pausedRigRollout } };
