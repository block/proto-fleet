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

/** A stand-in page behind the modal, so the overlay/dim reads as an overlay
 * (the real app has fleet content here). */
function DemoFleetPageBehind(): ReactElement {
  const rows = ["M-1042", "M-1043", "M-1044", "M-1045", "M-1046", "M-1047", "M-1048", "M-1049"];
  return (
    <div className="mx-auto max-w-4xl">
      <div className="mb-4 text-heading-300 text-text-primary">Fleet — Building B</div>
      <div className="overflow-hidden rounded-xl border border-border-5 bg-surface-elevated-base">
        <div className="grid grid-cols-[120px_1fr_120px_140px] gap-4 border-b border-border-5 px-4 py-2.5 text-200 text-text-primary-50">
          <span>Miner</span>
          <span>Model</span>
          <span>Status</span>
          <span>Firmware</span>
        </div>
        {rows.map((miner) => (
          <div
            key={miner}
            className="grid grid-cols-[120px_1fr_120px_140px] gap-4 border-b border-border-5 px-4 py-3 last:border-b-0"
          >
            <span className="text-300 text-text-primary">{miner}</span>
            <span className="text-300 text-text-primary-70">Antminer S21</span>
            <span className="text-300 text-text-primary-70">Hashing</span>
            <span className="text-300 text-text-primary-70">5.0.2</span>
          </div>
        ))}
      </div>
    </div>
  );
}

/** Demonstrates "View rollout" summoning the progress card in a modal over the
 * current page, so the operator never loses context. Toggle it closed to see
 * the page behind. */
function ViewRolloutModalStory({ event }: { event: RolloutEvent }): ReactElement {
  const [open, setOpen] = useState(true);
  return (
    <div className="min-h-screen bg-surface-base p-8">
      <div className="mb-6 flex justify-center">
        <Button variant={variants.primary} text="View rollout" onClick={() => setOpen(true)} />
      </div>
      <DemoFleetPageBehind />
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
