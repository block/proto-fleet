import type { Meta, StoryObj } from "@storybook/react-vite";

import { activeRigRollout, completedRigRollout } from "./RolloutChannels.fixtures";
import RolloutDetailModal from "./RolloutDetailModal";

// The update detail modal behind "View update": status lockup with a live
// elapsed timer while running, segmented progress with legend, and the
// scope/target/timing detail rows.
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

export const InProgress: Story = {
  name: "Update in progress",
  render: () => <RolloutDetailModal rollout={activeRigRollout} onClose={noop} />,
};

export const Completed: Story = {
  name: "Completed update",
  render: () => <RolloutDetailModal rollout={completedRigRollout} onClose={noop} />,
};
