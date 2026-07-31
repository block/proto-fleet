import type { Meta, StoryObj } from "@storybook/react-vite";

import ActiveRolloutStatus from "@/protoFleet/features/rollout/ActiveRolloutStatus";
import {
  completedWithFailuresFirmwareEvent,
  inProgressCurtailmentEvent,
  inProgressFirmwareEvent,
  inProgressRebootEvent,
  pilotGateFirmwareEvent,
} from "@/protoFleet/features/rollout/rollout.fixtures";

const meta = {
  title: "Proto Fleet/Rollout/Active Rollout Status",
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

const noop = () => undefined;

export const InProgress: Story = {
  name: "Firmware — in progress",
  args: {
    event: inProgressFirmwareEvent,
    onPause: noop,
    onCancelRemaining: noop,
  },
};

export const PausedAtPilotGate: Story = {
  name: "Firmware — paused at pilot gate",
  args: {
    event: pilotGateFirmwareEvent,
    onContinueFromPilot: noop,
    onRetryFailed: noop,
    onCancelRemaining: noop,
  },
};

export const CompletedWithFailures: Story = {
  name: "Firmware — completed with failures",
  args: {
    event: completedWithFailuresFirmwareEvent,
    onRetryFailed: noop,
  },
};

export const RebootInProgress: Story = {
  name: "Reboot — in progress (process-agnostic)",
  args: {
    event: inProgressRebootEvent,
    onPause: noop,
    onCancelRemaining: noop,
  },
};

export const CurtailmentInProgress: Story = {
  name: "Curtailment — in progress (process-agnostic)",
  args: {
    event: inProgressCurtailmentEvent,
    onPause: noop,
    onCancelRemaining: noop,
  },
};
