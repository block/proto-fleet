import type { ReactElement } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import ActiveRolloutStatus from "@/protoFleet/features/rollout/ActiveRolloutStatus";
import { AnimatedRebootInSitu, RebootInSitu } from "@/protoFleet/features/rollout/activeRolloutStoryHelpers";
import {
  completedRebootEvent,
  completedWithFailuresRebootEvent,
  inProgressRebootEvent,
  pausedRebootEvent,
} from "@/protoFleet/features/rollout/rollout.fixtures";

/**
 * The active **reboot** across its lifecycle states, plus a live-animated
 * lifecycle — all rendered **in situ**. Reboot has no settings page of its own:
 * it is a Fleet bulk action (`FleetGroupActionsMenu`), so its in-situ home is
 * the Fleet page (nav sidebar + "Fleet" header + Reboot/Update CTAs + rack
 * list), with the shipped `ActiveRolloutStatus` card inline above the rack
 * list — the same inline treatment the firmware story uses on the Firmware
 * settings page. Reboot is a batched process with no pilot-approval gate, so it
 * has no "scheduled for later" / "pilot review" states of its own; the covered
 * states are the ones a reboot actually reaches.
 */
const meta = {
  title: "Proto Fleet/Rollout/In Situ/Reboot Lifecycle",
  component: ActiveRolloutStatus,
  parameters: {
    layout: "fullscreen",
    // The in-situ surface provides its own MemoryRouter (at /fleet), so opt out
    // of the global StoryRouter — react-router throws on nested routers.
    withRouter: false,
  },
} satisfies Meta<typeof ActiveRolloutStatus>;

export default meta;

type Story = StoryObj<typeof ActiveRolloutStatus>;

export const InProgress: Story = {
  name: "In progress",
  render: () => <RebootInSitu event={inProgressRebootEvent} />,
};

export const Paused: Story = {
  render: () => <RebootInSitu event={pausedRebootEvent} />,
};

export const Completed: Story = {
  render: () => <RebootInSitu event={completedRebootEvent} />,
};

export const CompletedWithFailures: Story = {
  name: "Completed with failures",
  render: () => <RebootInSitu event={completedWithFailuresRebootEvent} />,
};

export const AnimatedRebootLifecycle: Story = {
  name: "Animated reboot lifecycle",
  render: function renderAnimatedRebootLifecycle(): ReactElement {
    return <AnimatedRebootInSitu base={inProgressRebootEvent} />;
  },
};
