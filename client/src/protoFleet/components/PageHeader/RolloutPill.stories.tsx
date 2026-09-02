import type { Meta, StoryObj } from "@storybook/react-vite";

import RolloutPill from "./RolloutPill";
import {
  activeRigRollout,
  activeS19Rollout,
  gatedRigRollout,
} from "@/protoFleet/features/settings/components/RolloutLanes/RolloutChannels.fixtures";

// The app-wide header pill shown while firmware rollouts are running. Its
// popover lists each rollout's channel, model, target version, and progress,
// and deep-links to the release channels view. A rollout parked at its
// review gate takes over the trigger copy and stops the dot pulsing: it is
// waiting on the operator, not the fleet.
const meta = {
  title: "Proto Fleet/Firmware/Rollout Channels/Header Pill",
  component: RolloutPill,
  parameters: {
    layout: "centered",
  },
} satisfies Meta<typeof RolloutPill>;

export default meta;

type Story = StoryObj<typeof RolloutPill>;

export const SingleRollout: Story = {
  name: "One rollout in progress",
  render: () => <RolloutPill rollouts={[activeRigRollout]} />,
};

export const MultipleRollouts: Story = {
  name: "Two concurrent rollouts",
  render: () => <RolloutPill rollouts={[activeRigRollout, activeS19Rollout]} />,
};

export const ReviewNeeded: Story = {
  name: "A rollout needs review",
  render: () => <RolloutPill rollouts={[gatedRigRollout, activeS19Rollout]} />,
};
