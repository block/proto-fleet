import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  abortedRigRollout,
  activeRigRollout,
  batchedRigRollout,
  completedRigRollout,
  gatedRigRollout,
  pausedRigRollout,
  pilotRigRollout,
} from "./RolloutChannels.fixtures";
import RolloutDetailModal from "./RolloutDetailModal";
import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

// The update detail modal behind "View update": status lockup with a live
// elapsed timer while running, segmented progress with legend, the review
// gate with per-miner evidence against baseline, operator controls (pause,
// resume, abort, continue), and the scope/target/timing detail rows.
const meta = {
  title: "Proto Fleet/Firmware/Rollout Channels/Update Detail Modal",
  component: RolloutDetailModal,
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof RolloutDetailModal>;

export default meta;

type Story = StoryObj<typeof RolloutDetailModal>;

const noop = () => {};
const afterDelay = () => new Promise<void>((resolve) => setTimeout(resolve, 800));

const Render = ({ rollout }: { rollout: Rollout }) => (
  <RolloutDetailModal
    rollout={rollout}
    onClose={noop}
    onContinue={afterDelay}
    onPause={afterDelay}
    onResume={afterDelay}
    onAbort={noop}
  />
);

export const InProgress: Story = {
  name: "Update in progress",
  render: () => <Render rollout={activeRigRollout} />,
};

export const PilotInProgress: Story = {
  name: "Pilot in progress",
  render: () => <Render rollout={pilotRigRollout} />,
};

export const PilotAwaitingReview: Story = {
  name: "Pilot awaiting review (healthy evidence)",
  render: () => <Render rollout={gatedRigRollout} />,
};

export const BatchesHoldingOnEvidence: Story = {
  name: "Batch 2 of 3 holding (degraded evidence, auto-advance)",
  render: () => <Render rollout={batchedRigRollout} />,
};

export const Paused: Story = {
  name: "Paused update",
  render: () => <Render rollout={pausedRigRollout} />,
};

export const Completed: Story = {
  name: "Completed update",
  render: () => <Render rollout={completedRigRollout} />,
};

export const Aborted: Story = {
  name: "Aborted update",
  render: () => <Render rollout={abortedRigRollout} />,
};
