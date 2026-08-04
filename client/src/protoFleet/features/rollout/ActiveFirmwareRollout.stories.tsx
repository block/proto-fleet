import type { ReactElement } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import ActiveRolloutStatus from "@/protoFleet/features/rollout/ActiveRolloutStatus";
import {
  ActiveRolloutStatusCard,
  AnimatedRolloutLifecycle,
} from "@/protoFleet/features/rollout/activeRolloutStoryHelpers";
import {
  completedFirmwareEvent,
  completedWithFailuresFirmwareEvent,
  inProgressFirmwareEvent,
  pausedFirmwareEvent,
  pilotGateFirmwareEvent,
  scheduledFirmwareEvent,
} from "@/protoFleet/features/rollout/rollout.fixtures";

/**
 * The active **firmware update** detail card across its lifecycle states, plus
 * a live-animated lifecycle — the same showcase shape as
 * `ActiveCurtailmentStatus.stories` (each state as its own story, then one
 * animated loop). All stories render the shipped `ActiveRolloutStatus` with
 * fixture events; the shared helpers keep the lifecycle animation identical to
 * the reboot story so the two can't drift.
 */
const meta = {
  title: "Proto Fleet/Rollout/Active Firmware Update",
  component: ActiveRolloutStatus,
  parameters: {
    layout: "fullscreen",
  },
  decorators: [
    (Story) => (
      <div className="min-h-screen bg-surface-base p-8">
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof ActiveRolloutStatus>;

export default meta;

type Story = StoryObj<typeof ActiveRolloutStatus>;

export const Scheduled: Story = {
  render: () => <ActiveRolloutStatusCard event={scheduledFirmwareEvent} />,
};

export const InProgress: Story = {
  name: "In progress",
  render: () => <ActiveRolloutStatusCard event={inProgressFirmwareEvent} />,
};

export const Paused: Story = {
  render: () => <ActiveRolloutStatusCard event={pausedFirmwareEvent} />,
};

export const PilotReview: Story = {
  name: "Pilot review",
  render: () => <ActiveRolloutStatusCard event={pilotGateFirmwareEvent} />,
};

export const Completed: Story = {
  render: () => <ActiveRolloutStatusCard event={completedFirmwareEvent} />,
};

export const CompletedWithFailures: Story = {
  name: "Completed with failures",
  render: () => <ActiveRolloutStatusCard event={completedWithFailuresFirmwareEvent} />,
};

export const AnimatedFirmwareLifecycle: Story = {
  name: "Animated firmware lifecycle",
  render: function renderAnimatedFirmwareLifecycle(): ReactElement {
    return <AnimatedRolloutLifecycle base={inProgressFirmwareEvent} />;
  },
};
