import type { ReactNode } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import ActiveUpdatesSection from "./ActiveUpdatesSection";
import { activeRigRollout, activeS19Rollout, gatedRigRollout } from "./RolloutChannels.fixtures";

// The "Active updates" strip at the top of the release channels view: one
// compact row per ongoing rollout with live progress and a "View update"
// action into the full detail modal.
const meta = {
  title: "Proto Fleet/Firmware/Rollout Channels/Active Updates",
  component: ActiveUpdatesSection,
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof ActiveUpdatesSection>;

export default meta;

type Story = StoryObj<typeof ActiveUpdatesSection>;

const noop = () => {};

const Frame = ({ children }: { children: ReactNode }) => (
  <div className="min-h-screen bg-surface-base p-6">{children}</div>
);

export const SingleUpdate: Story = {
  name: "One update running",
  render: () => (
    <Frame>
      <ActiveUpdatesSection rollouts={[activeRigRollout]} onViewUpdate={noop} />
    </Frame>
  ),
};

export const MultipleUpdates: Story = {
  name: "Two updates running",
  render: () => (
    <Frame>
      <ActiveUpdatesSection rollouts={[activeRigRollout, activeS19Rollout]} onViewUpdate={noop} />
    </Frame>
  ),
};

export const ReviewNeeded: Story = {
  name: "Pilot awaiting review",
  render: () => (
    <Frame>
      <ActiveUpdatesSection rollouts={[gatedRigRollout, activeS19Rollout]} onViewUpdate={noop} />
    </Frame>
  ),
};
