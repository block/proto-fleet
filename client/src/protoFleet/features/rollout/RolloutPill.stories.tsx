import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  completedWithFailuresFirmwareEvent,
  inProgressFirmwareEvent,
  pilotGateFirmwareEvent,
} from "@/protoFleet/features/rollout/rollout.fixtures";
import RolloutPill from "@/protoFleet/features/rollout/RolloutPill";

const meta = {
  title: "Proto Fleet/Rollout/Rollout Pill",
  component: RolloutPill,
  parameters: {
    layout: "centered",
  },
  decorators: [
    (Story) => (
      // Mimic the header bar: pill anchored top-right, room below for the popover.
      <div className="flex h-80 w-[560px] max-w-full justify-end bg-surface-base p-4">
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof RolloutPill>;

export default meta;

type Story = StoryObj<typeof RolloutPill>;

export const InProgress: Story = {
  name: "In progress (open the popover)",
  args: {
    event: inProgressFirmwareEvent,
    detailsPath: "/activity/rollouts/firmware-5-1-0",
  },
};

export const PausedAtPilotGate: Story = {
  args: {
    event: pilotGateFirmwareEvent,
    detailsPath: "/activity/rollouts/firmware-5-1-0",
  },
};

export const CompletedWithFailures: Story = {
  args: {
    event: completedWithFailuresFirmwareEvent,
    detailsPath: "/activity/rollouts/firmware-5-1-0",
  },
};
