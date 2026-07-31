import { type ReactElement, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { ActiveRolloutBanner, ActiveRolloutBannerStack } from "@/protoFleet/features/rollout/ActiveRolloutBanner";
import {
  batchedFirmwareConfig,
  inProgressCurtailmentEvent,
  inProgressFirmwareEvent,
  inProgressRebootEvent,
} from "@/protoFleet/features/rollout/rollout.fixtures";
import RolloutColumnState from "@/protoFleet/features/rollout/RolloutColumnState";
import RolloutConfigModal from "@/protoFleet/features/rollout/RolloutConfigModal";
import RolloutPill from "@/protoFleet/features/rollout/RolloutPill";
import type { RolloutTargetPhase } from "@/protoFleet/features/rollout/rolloutTypes";
import { useRolloutConfigModalState } from "@/protoFleet/features/rollout/useRolloutConfigModalState";
import ViewRolloutModal from "@/protoFleet/features/rollout/ViewRolloutModal";
import Button, { variants } from "@/shared/components/Button";

/**
 * Contextual ("in-situ") stories: each rollout component shown where it lives
 * in the product, so the surfaces read as real screens rather than isolated
 * parts. Uses the same shared primitives the real pages use.
 */
const meta = {
  title: "Proto Fleet/Rollout/In Situ",
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta;

export default meta;

type Story = StoryObj;

const noop = () => undefined;

// ---- 1. Config modal: Apply to + Rollout controls + Date and time ----------
// Uses the real RolloutConfigModal (shared Modal): CTAs in the top bar, body
// scrolls, dismiss via close / Escape / click-outside.

function ConfigModalStory(): ReactElement {
  const [open, setOpen] = useState(true);
  const state = useRolloutConfigModalState(batchedFirmwareConfig);
  return (
    <div className="min-h-screen bg-surface-base p-8">
      <div className="mb-6 flex justify-center">
        <Button variant={variants.primary} text="Bulk actions — Update firmware" onClick={() => setOpen(true)} />
      </div>
      <div className="mx-auto max-w-4xl text-300 text-text-primary-70">
        Launched from Bulk actions over the fleet page.
      </div>
      {open ? (
        <RolloutConfigModal
          title="Update firmware"
          description="Antminer S21 (5.0.2 → 5.1.0)"
          config={state.config}
          onConfigChange={state.setConfig}
          onDismiss={() => setOpen(false)}
          onSubmit={() => setOpen(false)}
          scopeTargets={[
            { label: "Sites", value: "Select", onClick: noop },
            { label: "Buildings", value: "1 building", onClick: noop },
            { label: "Racks", value: "Select", onClick: noop },
            { label: "Groups", value: "Select", onClick: noop },
            { label: "Miners", value: "Select", onClick: noop },
          ]}
          startDate={state.startDate}
          onStartDateChange={state.setStartDate}
          startTime={state.startTime}
          onStartTimeChange={state.setStartTime}
        />
      ) : null}
    </div>
  );
}

export const ConfigModal: Story = {
  name: "Config modal (Apply to + Rollout + Date and time)",
  render: () => <ConfigModalStory />,
};

// ---- 2. Fleet table: in-progress banner + per-miner Firmware column ---------

interface FleetRow {
  miner: string;
  model: string;
  status: string;
  phase: RolloutTargetPhase;
  doneLabel?: string;
  idleLabel?: string;
}

const fleetRows: FleetRow[] = [
  { miner: "M-1042", model: "Antminer S21", status: "Hashing", phase: "done", doneLabel: "5.1.0" },
  { miner: "M-1043", model: "Antminer S21", status: "Hashing", phase: "inProgress" },
  { miner: "M-1044", model: "Antminer S21", status: "Hashing", phase: "retrying" },
  { miner: "M-1045", model: "Antminer S21", status: "Offline", phase: "failed" },
  { miner: "M-1046", model: "Whatsminer M60", status: "Hashing", phase: "excluded" },
  { miner: "M-1047", model: "Antminer S21", status: "Hashing", phase: "queued", idleLabel: "5.0.2" },
];

function FleetTableStory(): ReactElement {
  const cols = "grid grid-cols-[120px_1fr_120px_160px] items-center gap-4 px-4";
  return (
    <div className="min-h-screen bg-surface-base p-8">
      <div className="mx-auto flex max-w-4xl flex-col gap-4">
        <div className="text-heading-300 text-text-primary">Building B</div>
        <ActiveRolloutBanner event={inProgressFirmwareEvent} onView={noop} />
        <div className="overflow-hidden rounded-xl border border-border-5 bg-surface-elevated-base">
          <div className={`${cols} border-b border-border-5 py-2.5 text-200 text-text-primary-50`}>
            <span>Miner</span>
            <span>Model</span>
            <span>Status</span>
            <span>Firmware</span>
          </div>
          {fleetRows.map((row) => (
            <div key={row.miner} className={`${cols} border-b border-border-5 py-3 last:border-b-0`}>
              <span className="text-300 text-text-primary">{row.miner}</span>
              <span className="text-300 text-text-primary-70">{row.model}</span>
              <span className="text-300 text-text-primary-70">{row.status}</span>
              <RolloutColumnState phase={row.phase} doneLabel={row.doneLabel} idleLabel={row.idleLabel} />
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

export const FleetTable: Story = {
  name: "Fleet table (banner + Firmware column)",
  render: () => <FleetTableStory />,
};

// ---- 3. Header bar: the persistent rollout pill ------------------------------

export const HeaderPill: Story = {
  name: "Header pill (in the top bar)",
  render: () => (
    <div className="min-h-screen bg-surface-base">
      <div className="flex h-14 items-center justify-between border-b border-border-5 bg-surface-elevated-base px-6">
        <div className="text-emphasis-300 text-text-primary">Denver — Building B</div>
        <RolloutPill event={inProgressFirmwareEvent} detailsPath="/activity/rollouts/firmware-5-1-0" />
      </div>
      <div className="p-8 text-300 text-text-primary-70">Open the pill to see the quick-status popover.</div>
    </div>
  ),
};

// ---- 4. Activity "Active now": stacked banners ------------------------------

export const ActivityActiveNow: Story = {
  name: "Activity — Active now (stacked banners)",
  render: () => (
    <div className="min-h-screen bg-surface-base p-8">
      <div className="mx-auto flex max-w-4xl flex-col gap-4">
        <div className="text-heading-300 text-text-primary">Activity</div>
        <div className="flex items-center justify-between gap-4">
          <div className="text-emphasis-300 text-text-primary">Active now</div>
          <span className="text-200 text-text-primary-70">3 processes running</span>
        </div>
        <ActiveRolloutBannerStack
          events={[inProgressFirmwareEvent, inProgressRebootEvent, inProgressCurtailmentEvent]}
          onView={noop}
        />
      </div>
    </div>
  ),
};

// ---- 5. View rollout modal over the fleet page ------------------------------

function ViewRolloutInSituStory(): ReactElement {
  const [open, setOpen] = useState(true);
  return (
    <div className="min-h-screen bg-surface-base p-8">
      <div className="mx-auto flex max-w-4xl flex-col gap-4">
        <div className="text-heading-300 text-text-primary">Building B</div>
        <ActiveRolloutBanner event={inProgressFirmwareEvent} onView={() => setOpen(true)} />
        <div className="rounded-xl border border-border-5 bg-surface-elevated-base p-6 text-300 text-text-primary-70">
          The fleet page stays here behind the overlay — "View rollout" summons progress without navigating away.
        </div>
      </div>
      <ViewRolloutModal
        event={open ? inProgressFirmwareEvent : null}
        onDismiss={() => setOpen(false)}
        onPause={noop}
        onCancelRemaining={noop}
      />
    </div>
  );
}

export const ViewRolloutInSitu: Story = {
  name: "View rollout modal (over the fleet page)",
  render: () => <ViewRolloutInSituStory />,
};
