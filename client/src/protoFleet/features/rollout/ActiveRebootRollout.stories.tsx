import type { ReactElement } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import ActiveRolloutStatus from "@/protoFleet/features/rollout/ActiveRolloutStatus";
import {
  ActiveRolloutStatusCard,
  AnimatedRolloutLifecycle,
} from "@/protoFleet/features/rollout/activeRolloutStoryHelpers";
import {
  completedRebootEvent,
  completedWithFailuresRebootEvent,
  inProgressRebootEvent,
  pausedRebootEvent,
} from "@/protoFleet/features/rollout/rollout.fixtures";

/**
 * The active **reboot** detail card across its lifecycle states, plus a
 * live-animated lifecycle — the same showcase shape as
 * `ActiveCurtailmentStatus.stories` and the firmware story. Reboot is a
 * batched process with no pilot-approval gate, so it has no "scheduled for
 * later" / "pilot review" states of its own; the covered states are the ones a
 * reboot actually reaches. Renders the shipped `ActiveRolloutStatus` with
 * fixture events via the shared helpers.
 */
const meta = {
  title: "Proto Fleet/Rollout/Active Reboot",
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

export const InProgress: Story = {
  name: "In progress",
  render: () => <ActiveRolloutStatusCard event={inProgressRebootEvent} />,
};

export const Paused: Story = {
  render: () => <ActiveRolloutStatusCard event={pausedRebootEvent} />,
};

export const Completed: Story = {
  render: () => <ActiveRolloutStatusCard event={completedRebootEvent} />,
};

export const CompletedWithFailures: Story = {
  name: "Completed with failures",
  render: () => <ActiveRolloutStatusCard event={completedWithFailuresRebootEvent} />,
};

export const AnimatedRebootLifecycle: Story = {
  name: "Animated reboot lifecycle",
  render: function renderAnimatedRebootLifecycle(): ReactElement {
    return <AnimatedRolloutLifecycle base={inProgressRebootEvent} />;
  },
};
