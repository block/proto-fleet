import type { ReactNode } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import ModelRolloutStatus from "./ModelRolloutStatus";
import { activeRigRollout, nearlyDoneRigRollout } from "./RolloutChannels.fixtures";

// The live card shown in the "Active rollouts" section at the top of the
// rollout channels view: channel and model lockup, progress summary with a
// running elapsed timer, segmented progress bar, and legend.
const meta = {
  title: "Proto Fleet/Firmware/Rollout Channels/Active Rollout Card",
  component: ModelRolloutStatus,
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof ModelRolloutStatus>;

export default meta;

type Story = StoryObj<typeof ModelRolloutStatus>;

const Frame = ({ children }: { children: ReactNode }) => (
  <div className="min-h-screen bg-surface-base p-6">{children}</div>
);

export const InProgress: Story = {
  name: "In progress (2 of 6 updated)",
  render: () => (
    <Frame>
      <ModelRolloutStatus rollout={activeRigRollout} />
    </Frame>
  ),
};

export const NearlyDone: Story = {
  name: "Nearly done (5 of 6 updated)",
  render: () => (
    <Frame>
      <ModelRolloutStatus rollout={nearlyDoneRigRollout} />
    </Frame>
  ),
};
