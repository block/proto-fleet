import { type ReactElement, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { batchedFirmwareConfig } from "@/protoFleet/features/rollout/rollout.fixtures";
import RolloutConfigModal from "@/protoFleet/features/rollout/RolloutConfigModal";
import { useRolloutConfigModalState } from "@/protoFleet/features/rollout/useRolloutConfigModalState";
import { variants } from "@/shared/components/Button";
import Button from "@/shared/components/Button";

const meta = {
  title: "Proto Fleet/Rollout/Config Modal",
  component: RolloutConfigModal,
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof RolloutConfigModal>;

export default meta;

type Story = StoryObj<typeof RolloutConfigModal>;

const noop = () => undefined;

const scopeTargets = [
  { label: "Sites", value: "Select", onClick: noop },
  { label: "Buildings", value: "1 building", onClick: noop },
  { label: "Racks", value: "Select", onClick: noop },
  { label: "Groups", value: "Select", onClick: noop },
  { label: "Miners", value: "Select", onClick: noop },
];

/** Launched from a Bulk-actions button over the fleet page; the primary CTA
 * sits in the modal's top bar and the body scrolls when it outgrows the
 * viewport. */
function ConfigModalStory(): ReactElement {
  const [open, setOpen] = useState(true);
  const state = useRolloutConfigModalState(batchedFirmwareConfig);

  return (
    <div className="min-h-screen bg-surface-base p-8">
      <div className="mb-6 flex justify-center">
        <Button variant={variants.primary} text="Bulk actions — Update firmware" onClick={() => setOpen(true)} />
      </div>
      <div className="mx-auto max-w-4xl text-300 text-text-primary-70">
        The fleet page stays mounted behind the overlay.
      </div>
      {open ? (
        <RolloutConfigModal
          title="Update firmware"
          description="Antminer S21 (5.0.2 → 5.1.0)"
          config={state.config}
          onConfigChange={state.setConfig}
          onDismiss={() => setOpen(false)}
          onSubmit={() => setOpen(false)}
          scopeTargets={scopeTargets}
          startDate={state.startDate}
          onStartDateChange={state.setStartDate}
          startTime={state.startTime}
          onStartTimeChange={state.setStartTime}
        />
      ) : null}
    </div>
  );
}

export const UpdateFirmware: Story = {
  name: "Update firmware (CTAs in top bar, scrolls)",
  render: () => <ConfigModalStory />,
};
