import type { ReactElement } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import ActiveRolloutStatus from "@/protoFleet/features/rollout/ActiveRolloutStatus";
import { AnimatedInSituRollout, InSituRollout } from "@/protoFleet/features/rollout/activeRolloutStoryHelpers";
import {
  completedFirmwareEvent,
  completedWithFailuresFirmwareEvent,
  inProgressFirmwareEvent,
  pausedFirmwareEvent,
  pilotGateFirmwareEvent,
  scheduledFirmwareEvent,
} from "@/protoFleet/features/rollout/rollout.fixtures";

/**
 * The active **firmware update** across its lifecycle states, plus a
 * live-animated lifecycle — all rendered **in situ**, inside the real app shell
 * with the shipped `RolloutPill` in a page header and the shipped
 * `ViewRolloutModal` opened on the rollout. Each state reads where an operator
 * actually meets it (product chrome behind the detail overlay) rather than as a
 * bare card on a blank canvas. The shared helpers keep this identical to the
 * reboot story so the two can't drift.
 */
const meta = {
  title: "Proto Fleet/Rollout/In Situ/Firmware Lifecycle",
  component: ActiveRolloutStatus,
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof ActiveRolloutStatus>;

export default meta;

type Story = StoryObj<typeof ActiveRolloutStatus>;

export const Scheduled: Story = {
  render: () => <InSituRollout event={scheduledFirmwareEvent} />,
};

export const InProgress: Story = {
  name: "In progress",
  render: () => <InSituRollout event={inProgressFirmwareEvent} />,
};

export const Paused: Story = {
  render: () => <InSituRollout event={pausedFirmwareEvent} />,
};

export const PilotReview: Story = {
  name: "Pilot review",
  render: () => <InSituRollout event={pilotGateFirmwareEvent} />,
};

export const Completed: Story = {
  render: () => <InSituRollout event={completedFirmwareEvent} />,
};

export const CompletedWithFailures: Story = {
  name: "Completed with failures",
  render: () => <InSituRollout event={completedWithFailuresFirmwareEvent} />,
};

export const AnimatedFirmwareLifecycle: Story = {
  name: "Animated firmware lifecycle",
  render: function renderAnimatedFirmwareLifecycle(): ReactElement {
    return <AnimatedInSituRollout base={inProgressFirmwareEvent} />;
  },
};
