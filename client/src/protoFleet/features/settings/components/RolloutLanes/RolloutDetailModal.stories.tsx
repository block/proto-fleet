import type { Meta, StoryObj } from "@storybook/react-vite";

import { activeRigRollout, completedRigRollout, gatedRigRollout, pilotRigRollout } from "./RolloutChannels.fixtures";
import RolloutDetailModal from "./RolloutDetailModal";

// The update detail modal behind "View update": status lockup with a live
// elapsed timer while running, segmented progress with legend, the pilot
// review gate for gated rollouts, and the scope/target/timing detail rows.
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
const continueAfterDelay = () => new Promise<void>((resolve) => setTimeout(resolve, 800));

export const InProgress: Story = {
  name: "Update in progress",
  render: () => <RolloutDetailModal rollout={activeRigRollout} onClose={noop} onContinue={continueAfterDelay} />,
};

export const PilotInProgress: Story = {
  name: "Pilot in progress",
  render: () => <RolloutDetailModal rollout={pilotRigRollout} onClose={noop} onContinue={continueAfterDelay} />,
};

export const PilotAwaitingReview: Story = {
  name: "Pilot awaiting review",
  render: () => <RolloutDetailModal rollout={gatedRigRollout} onClose={noop} onContinue={continueAfterDelay} />,
};

export const Completed: Story = {
  name: "Completed update",
  render: () => <RolloutDetailModal rollout={completedRigRollout} onClose={noop} onContinue={continueAfterDelay} />,
};
