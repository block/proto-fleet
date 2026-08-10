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
 * Reboot rollout lifecycle states rendered on the Fleet page. Reboot is a bulk
 * action, so these stories use the Fleet page as the in-product home.
 */
const meta = {
  title: "Proto Fleet/Rollout/In Situ/Reboot Lifecycle",
  component: ActiveRolloutStatus,
  parameters: {
    layout: "fullscreen",
    // The page shell provides its own MemoryRouter at /fleet.
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
