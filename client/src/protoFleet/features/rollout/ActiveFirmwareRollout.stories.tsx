import type { ReactElement } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import ActiveRolloutStatus from "@/protoFleet/features/rollout/ActiveRolloutStatus";
import { AnimatedFirmwareInSitu, FirmwareInSitu } from "@/protoFleet/features/rollout/activeRolloutStoryHelpers";
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
 * live-animated lifecycle — all rendered **in situ**, on the established
 * firmware in-situ surface: the Firmware settings page (nav sidebar + settings
 * subnav + "Firmware" header + Upload CTA + firmware files table), with the
 * shipped `ActiveRolloutStatus` card inline above the files table, exactly the
 * way the `In Situ/In Progress` "Firmware settings page" story already
 * establishes. Each state reads where an operator actually meets it, not as a
 * bare card on a blank canvas. `FirmwareInSitu` is the single shared surface so
 * this can't drift from that story.
 */
const meta = {
  title: "Proto Fleet/Rollout/In Situ/Firmware Lifecycle",
  component: ActiveRolloutStatus,
  parameters: {
    layout: "fullscreen",
    // The in-situ surface provides its own MemoryRouter (at /settings/firmware),
    // so opt out of the global StoryRouter — react-router throws on nested routers.
    withRouter: false,
  },
} satisfies Meta<typeof ActiveRolloutStatus>;

export default meta;

type Story = StoryObj<typeof ActiveRolloutStatus>;

export const Scheduled: Story = {
  render: () => <FirmwareInSitu event={scheduledFirmwareEvent} />,
};

export const InProgress: Story = {
  name: "In progress",
  render: () => <FirmwareInSitu event={inProgressFirmwareEvent} />,
};

export const Paused: Story = {
  render: () => <FirmwareInSitu event={pausedFirmwareEvent} />,
};

export const PilotReview: Story = {
  name: "Pilot review",
  render: () => <FirmwareInSitu event={pilotGateFirmwareEvent} />,
};

export const Completed: Story = {
  render: () => <FirmwareInSitu event={completedFirmwareEvent} />,
};

export const CompletedWithFailures: Story = {
  name: "Completed with failures",
  render: () => <FirmwareInSitu event={completedWithFailuresFirmwareEvent} />,
};

export const AnimatedFirmwareLifecycle: Story = {
  name: "Animated firmware lifecycle",
  render: function renderAnimatedFirmwareLifecycle(): ReactElement {
    return <AnimatedFirmwareInSitu base={inProgressFirmwareEvent} />;
  },
};
