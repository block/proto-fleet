import type { Meta, StoryObj } from "@storybook/react-vite";
import { fn } from "storybook/test";

import {
  activeRigRollout,
  batchedRigRollout,
  canceledRemainingRigRollout,
  completedRigRollout,
  completedWithFailuresRigRollout,
  gatedRigRollout,
  pausedRigRollout,
} from "./ReleaseChannels.fixtures";
import RolloutDetailModal from "./RolloutDetailModal";
import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

// The full-screen live view contains its own title and dismissal alongside
// status, lifecycle controls, progress, plan details and telemetry evidence.
const meta = {
  title: "Proto Fleet/Firmware/Release Channels/Update Detail",
  component: RolloutDetailModal,
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component:
          "Fixed server snapshots. Callbacks are recorded in Actions; use the Active Monitor stories for navigation and confirmation flows.",
      },
    },
  },
  args: {
    onClose: fn(),
    onContinue: fn(() => Promise.resolve()),
    onPause: fn(() => Promise.resolve()),
    onResume: fn(() => Promise.resolve()),
    onCancel: fn(),
    onRollback: fn(),
    onRetryFailed: fn(),
    onViewMiners: fn(),
    onManage: fn(),
  },
} satisfies Meta<typeof RolloutDetailModal>;

export default meta;

type Story = StoryObj<typeof RolloutDetailModal>;

const detail = (rollout: Rollout): Story => ({
  render: (args) => (
    <div className="min-h-screen bg-surface-base">
      <RolloutDetailModal {...args} rollout={rollout} />
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
