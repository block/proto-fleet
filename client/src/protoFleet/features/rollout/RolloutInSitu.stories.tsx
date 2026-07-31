import { type ReactElement, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import TargetSelectButton from "@/protoFleet/components/TargetSelectButton";

import { ActiveRolloutBanner, ActiveRolloutBannerStack } from "@/protoFleet/features/rollout/ActiveRolloutBanner";
import {
  batchedFirmwareConfig,
  inProgressCurtailmentEvent,
  inProgressFirmwareEvent,
  inProgressRebootEvent,
} from "@/protoFleet/features/rollout/rollout.fixtures";
import RolloutColumnState from "@/protoFleet/features/rollout/RolloutColumnState";
import RolloutControls from "@/protoFleet/features/rollout/RolloutControls";
import RolloutPill from "@/protoFleet/features/rollout/RolloutPill";
import type { RolloutPlanConfig, RolloutTargetPhase } from "@/protoFleet/features/rollout/rolloutTypes";
import ViewRolloutModal from "@/protoFleet/features/rollout/ViewRolloutModal";
import Button, { variants } from "@/shared/components/Button";
import { DatePickerField } from "@/shared/components/DatePicker";
import Select from "@/shared/components/Select";

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

function SectionTitle({ children }: { children: string }): ReactElement {
  return <div className="text-emphasis-300 text-text-primary">{children}</div>;
}

function ConfigModalStory(): ReactElement {
  const [config, setConfig] = useState<RolloutPlanConfig>(batchedFirmwareConfig);
  const [startDate, setStartDate] = useState<Date | undefined>(new Date("2026-08-04T14:00:00"));

  return (
    <div className="min-h-screen bg-core-primary-5 p-8">
      <div className="mx-auto w-[min(100%,560px)] overflow-hidden rounded-3xl bg-surface-elevated-base shadow-300">
        <div className="flex flex-col gap-8 p-8">
          <div>
            <div className="text-heading-300 text-text-primary">Update firmware</div>
            <div className="mt-1 text-300 text-text-primary-70">Antminer S21 (5.0.2 → 5.1.0)</div>
          </div>

          <section className="grid gap-3">
            <SectionTitle>Apply to</SectionTitle>
            <div className="grid divide-y divide-border-5">
              <TargetSelectButton label="Sites" value="Select" onClick={noop} />
              <TargetSelectButton label="Buildings" value="1 building" onClick={noop} />
              <TargetSelectButton label="Racks" value="Select" onClick={noop} />
              <TargetSelectButton label="Groups" value="Select" onClick={noop} />
              <TargetSelectButton label="Miners" value="Select" onClick={noop} />
            </div>
          </section>

          <RolloutControls config={config} onChange={setConfig} />

          <section className="grid gap-3">
            <SectionTitle>Date and time</SectionTitle>
            <Select
              id="rollout-schedule-type"
              label="Type"
              options={[
                { value: "startNow", label: "Start now" },
                { value: "scheduleForLater", label: "Schedule for later" },
              ]}
              value={config.scheduleType}
              onChange={(value) => setConfig({ ...config, scheduleType: value as RolloutPlanConfig["scheduleType"] })}
              forceBelow
            />
            {config.scheduleType === "scheduleForLater" ? (
              <div className="grid gap-3 tablet:grid-cols-2">
                <DatePickerField
                  id="rollout-start-date"
                  label="Start date"
                  labelPlacement="floating"
                  selectedDate={startDate}
                  onSelectedDateChange={setStartDate}
                />
                <Select
                  id="rollout-start-time"
                  label="Time"
                  options={[
                    { value: "14:00", label: "2:00 PM" },
                    { value: "18:00", label: "6:00 PM" },
                    { value: "22:00", label: "10:00 PM" },
                  ]}
                  value="14:00"
                  onChange={noop}
                  forceBelow
                />
              </div>
            ) : null}
            <div className="text-200 text-text-primary-70">Times shown in America/Denver (MDT)</div>
          </section>

          <div className="flex justify-end gap-3 border-t border-border-5 pt-6">
            <Button variant={variants.secondary} text="Cancel" onClick={noop} />
            <Button
              variant={variants.primary}
              text={config.scheduleType === "scheduleForLater" ? "Schedule rollout" : "Start rollout"}
              onClick={noop}
            />
          </div>
        </div>
      </div>
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
