import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  activeRigRollout,
  batchedRigRollout,
  canceledRemainingRigRollout,
  completedRigRollout,
  completedWithFailuresRigRollout,
  gatedRigRollout,
  listRolloutDevicesFixture,
  minerNames,
  pausedRigRollout,
} from "./ReleaseChannels.fixtures";
import RolloutDetailModal from "./RolloutDetailModal";
import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

// The full-screen update detail: lifecycle actions in the sticky header,
// failures first, the status lockup, plan stats, progress against plan and
// the telemetry evidence strip.
const meta = {
  title: "Proto Fleet/Firmware/Release Channels/Update Detail",
  component: RolloutDetailModal,
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof RolloutDetailModal>;

export default meta;

type Story = StoryObj<typeof RolloutDetailModal>;

const settle = () => Promise.resolve();
const noop = () => {};

const detail = (rollout: Rollout): Story => ({
  render: () => (
    <div className="min-h-screen bg-surface-base">
      <RolloutDetailModal
        rollout={rollout}
        minerNames={minerNames}
        listRolloutDevices={listRolloutDevicesFixture}
        onClose={noop}
        onContinue={settle}
        onPause={settle}
        onResume={settle}
        onCancel={noop}
        onRollback={noop}
        onRetryFailed={settle}
        onManage={noop}
      />
    </div>
  ),
});

export const InProgress: Story = { name: "Single batch in progress", ...detail(activeRigRollout) };
export const PilotReview: Story = { name: "Pilot batch review", ...detail(gatedRigRollout) };
export const BatchReviewWithFailure: Story = { name: "Batch review with a failed miner", ...detail(batchedRigRollout) };
export const Paused: Story = { ...detail(pausedRigRollout) };
export const Completed: Story = { ...detail(completedRigRollout) };
export const CompletedWithFailures: Story = {
  name: "Completed with failures",
  ...detail(completedWithFailuresRigRollout),
};
export const CanceledRemaining: Story = { name: "Canceled remaining", ...detail(canceledRemainingRigRollout) };
