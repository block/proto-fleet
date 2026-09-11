import type { Meta, StoryObj } from "@storybook/react-vite";

import RolloutPill from "./RolloutPill";
import {
  activeRigRollout,
  batchedRigRollout,
  gatedRigRollout,
} from "@/protoFleet/features/settings/components/ReleaseChannels/ReleaseChannels.fixtures";

// The app-wide header pill shown while firmware updates are running. Its
// popover lists each update's channel, model, target version and progress,
// and deep-links to the release channels view. An update parked at a review
// gate or carrying failed miners takes over the trigger copy and stops the
// dot pulsing: it is waiting on the operator, not the fleet.
const meta = {
  title: "Proto Fleet/Firmware/Release Channels/Header Pill",
  component: RolloutPill,
  parameters: {
    layout: "centered",
  },
} satisfies Meta<typeof RolloutPill>;

export default meta;

type Story = StoryObj<typeof RolloutPill>;

export const SingleUpdate: Story = {
  name: "One update in progress",
  render: () => <RolloutPill rollouts={[activeRigRollout]} />,
};

export const ReviewNeeded: Story = {
  name: "An update needs review",
  render: () => <RolloutPill rollouts={[gatedRigRollout, activeRigRollout]} />,
};

export const WithFailures: Story = {
  name: "An update has failed miners",
  render: () => <RolloutPill rollouts={[batchedRigRollout]} />,
};
