import { type ReactElement, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  completedWithFailuresFirmwareEvent,
  inProgressFirmwareEvent,
  pilotGateFirmwareEvent,
} from "@/protoFleet/features/rollout/rollout.fixtures";
import RolloutPill from "@/protoFleet/features/rollout/RolloutPill";
import type { RolloutEvent } from "@/protoFleet/features/rollout/rolloutTypes";
import ViewRolloutModal from "@/protoFleet/features/rollout/ViewRolloutModal";

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

const noop = () => undefined;

/** The pill opens its popover; "View rollout" summons the ViewRolloutModal in
 * place (no navigation) — the always-on entry point to progress from any page. */
function RolloutPillStory({ event }: { event: RolloutEvent }): ReactElement {
  const [open, setOpen] = useState(false);
  return (
    <>
      <RolloutPill event={event} onViewRollout={() => setOpen(true)} />
      <ViewRolloutModal
        event={open ? event : null}
        onDismiss={() => setOpen(false)}
        onPause={noop}
        onCancelRemaining={noop}
        onContinueFromPilot={noop}
        onRetryFailed={noop}
      />
    </>
  );
}

export const InProgress: Story = {
  name: "In progress (open the popover → View rollout)",
  render: () => <RolloutPillStory event={inProgressFirmwareEvent} />,
};

export const PausedAtPilotGate: Story = {
  render: () => <RolloutPillStory event={pilotGateFirmwareEvent} />,
};

export const CompletedWithFailures: Story = {
  render: () => <RolloutPillStory event={completedWithFailuresFirmwareEvent} />,
};
