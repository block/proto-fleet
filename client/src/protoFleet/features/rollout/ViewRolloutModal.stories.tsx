import { type ReactElement, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  completedWithFailuresFirmwareEvent,
  inProgressFirmwareEvent,
  pilotGateFirmwareEvent,
} from "@/protoFleet/features/rollout/rollout.fixtures";
import type { RolloutEvent } from "@/protoFleet/features/rollout/rolloutTypes";
import ViewRolloutModal from "@/protoFleet/features/rollout/ViewRolloutModal";
import Button, { variants } from "@/shared/components/Button";

const meta = {
  title: "Proto Fleet/Rollout/View Rollout Modal",
  component: ViewRolloutModal,
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof ViewRolloutModal>;

export default meta;

type Story = StoryObj<typeof ViewRolloutModal>;

const noop = () => undefined;

/** Demonstrates "View rollout" summoning the progress card in a modal over the
 * current page, so the operator never loses context. Toggle it closed to see
 * the (stand-in) page behind. */
function ViewRolloutModalStory({ event }: { event: RolloutEvent }): ReactElement {
  const [open, setOpen] = useState(true);
  return (
    <div className="min-h-screen bg-surface-base p-8">
      <div className="mx-auto max-w-4xl">
        <div className="mb-2 text-heading-200 text-text-primary">Fleet — Building B</div>
        <div className="text-300 text-text-primary-70">
          The page stays mounted behind the overlay. Press Escape or click outside to dismiss.
        </div>
        <div className="mt-6">
          <Button variant={variants.primary} text="View rollout" onClick={() => setOpen(true)} />
        </div>
      </div>
      <ViewRolloutModal
        event={open ? event : null}
        onDismiss={() => setOpen(false)}
        onPause={noop}
        onCancelRemaining={noop}
        onContinueFromPilot={noop}
        onRetryFailed={noop}
      />
    </div>
  );
}

export const InProgress: Story = {
  name: "In progress",
  render: () => <ViewRolloutModalStory event={inProgressFirmwareEvent} />,
};

export const PausedAtPilotGate: Story = {
  render: () => <ViewRolloutModalStory event={pilotGateFirmwareEvent} />,
};

export const CompletedWithFailures: Story = {
  render: () => <ViewRolloutModalStory event={completedWithFailuresFirmwareEvent} />,
};
