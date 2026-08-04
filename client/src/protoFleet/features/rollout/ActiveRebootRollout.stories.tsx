import type { ReactElement } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import ActiveRolloutStatus from "@/protoFleet/features/rollout/ActiveRolloutStatus";
import { AnimatedInSituRollout, InSituRollout } from "@/protoFleet/features/rollout/activeRolloutStoryHelpers";
import {
  completedRebootEvent,
  completedWithFailuresRebootEvent,
  inProgressRebootEvent,
  pausedRebootEvent,
} from "@/protoFleet/features/rollout/rollout.fixtures";

/**
 * The active **reboot** across its lifecycle states, plus a live-animated
 * lifecycle — all rendered **in situ**, inside the real app shell with the
 * shipped `RolloutPill` in a page header and the shipped `ViewRolloutModal`
 * opened on the rollout, the same as the firmware story. Reboot is a batched
 * process with no pilot-approval gate, so it has no "scheduled for later" /
 * "pilot review" states of its own; the covered states are the ones a reboot
 * actually reaches. Uses the shared in-situ helpers so the two can't drift.
 */
const meta = {
  title: "Proto Fleet/Rollout/In Situ/Reboot Lifecycle",
  component: ActiveRolloutStatus,
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof ActiveRolloutStatus>;

export default meta;

type Story = StoryObj<typeof ActiveRolloutStatus>;

export const InProgress: Story = {
  name: "In progress",
  render: () => <InSituRollout event={inProgressRebootEvent} />,
};

export const Paused: Story = {
  render: () => <InSituRollout event={pausedRebootEvent} />,
};

export const Completed: Story = {
  render: () => <InSituRollout event={completedRebootEvent} />,
};

export const CompletedWithFailures: Story = {
  name: "Completed with failures",
  render: () => <InSituRollout event={completedWithFailuresRebootEvent} />,
};

export const AnimatedRebootLifecycle: Story = {
  name: "Animated reboot lifecycle",
  render: function renderAnimatedRebootLifecycle(): ReactElement {
    return <AnimatedInSituRollout base={inProgressRebootEvent} />;
  },
};
