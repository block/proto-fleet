import { type ReactElement, type ReactNode, useEffect, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import NavigationMenu from "@/protoFleet/components/NavigationMenu";
import { primaryNavItems } from "@/protoFleet/config/navItems";
import { ActiveRolloutBanner, ActiveRolloutBannerStack } from "@/protoFleet/features/rollout/ActiveRolloutBanner";
import ActiveRolloutStatus from "@/protoFleet/features/rollout/ActiveRolloutStatus";
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
import { useFleetStore } from "@/protoFleet/store";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";

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

/**
 * The real app shell: the `NavigationMenu` sidebar (absolute, w-60) plus a
 * content column inset by that width. Wrap page-context stories in this so they
 * show the navigation + surrounding chrome, not just the page body.
 *
 * Seeds the fleet store with read permissions so the permission-gated nav items
 * (Fleet / Energy / Activity / Settings) render — Storybook has no auth session
 * otherwise, which is why the nav previously showed only Home + Settings.
 */
function AppShell({ children }: { children: ReactNode }): ReactElement {
  useEffect(() => {
    useFleetStore
      .getState()
      .auth.setPermissions(["fleet:read", "miner:read", "rack:read", "site:read", "curtailment:read", "activity:read"]);
  }, []);
  return (
    <div className="relative min-h-screen bg-surface-base">
      <NavigationMenu items={primaryNavItems} />
      <div className="min-h-screen pl-60">{children}</div>
    </div>
  );
}

/** A simple faux data table used to fill the surrounding page content with
 * representative (dummy) rows, so the in-situ stories read as populated screens
 * rather than a header + one card. `gridClass` is a static Tailwind grid-cols
 * class (JIT can't see runtime-built class strings). */
function DummyTable({
  gridClass,
  columns,
  rows,
}: {
  gridClass: string;
  columns: string[];
  rows: string[][];
}): ReactElement {
  return (
    <div className="overflow-hidden rounded-xl border border-border-5 bg-surface-elevated-base">
      <div className={`grid ${gridClass} gap-4 border-b border-border-5 px-4 py-2.5 text-200 text-text-primary-50`}>
        {columns.map((c) => (
          <span key={c}>{c}</span>
        ))}
      </div>
      {rows.map((row) => (
        <div
          key={row.join("|")}
          className={`grid ${gridClass} gap-4 border-b border-border-5 px-4 py-3 text-300 last:border-b-0`}
        >
          {row.map((cell, i) => (
            <span key={i} className={i === 0 ? "text-text-primary" : "text-text-primary-70"}>
              {cell}
            </span>
          ))}
        </div>
      ))}
    </div>
  );
}

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
  const [open, setOpen] = useState(false);
  return (
    <div className="min-h-screen bg-surface-base p-8">
      <div className="mx-auto flex max-w-4xl flex-col gap-4">
        <div className="text-heading-300 text-text-primary">Building B</div>
        <ActiveRolloutBanner event={inProgressFirmwareEvent} onView={() => setOpen(true)} />
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
      <ViewRolloutModal
        event={open ? inProgressFirmwareEvent : null}
        onDismiss={() => setOpen(false)}
        onPause={noop}
        onCancelRemaining={noop}
      />
    </div>
  );
}

export const FleetTable: Story = {
  name: "Fleet table (banner + Firmware column)",
  render: () => <FleetTableStory />,
};

// ---- 3. Header bar: the persistent rollout pill (opens the modal) -----------

function HeaderPillStory(): ReactElement {
  const [open, setOpen] = useState(false);
  return (
    <div className="min-h-screen bg-surface-base">
      <div className="flex h-14 items-center justify-between border-b border-border-5 bg-surface-elevated-base px-6">
        <div className="text-emphasis-300 text-text-primary">Denver — Building B</div>
        <RolloutPill event={inProgressFirmwareEvent} onViewRollout={() => setOpen(true)} />
      </div>
      <div className="p-8 text-300 text-text-primary-70">
        Open the pill, then "View rollout" to summon the progress modal in place.
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

export const HeaderPill: Story = {
  name: "Header pill (opens the modal)",
  render: () => <HeaderPillStory />,
};

// ---- 4. Activity "Active now": stacked banners (each opens the modal) --------

function ActivityActiveNowStory(): ReactElement {
  const events = [inProgressFirmwareEvent, inProgressRebootEvent, inProgressCurtailmentEvent];
  const [openIndex, setOpenIndex] = useState<number | null>(null);
  return (
    <div className="min-h-screen bg-surface-base p-8">
      <div className="mx-auto flex max-w-4xl flex-col gap-4">
        <div className="text-heading-300 text-text-primary">Activity</div>
        <div className="flex items-center justify-between gap-4">
          <div className="text-emphasis-300 text-text-primary">Active now</div>
          <span className="text-200 text-text-primary-70">3 processes running</span>
        </div>
        <ActiveRolloutBannerStack events={events} onView={(_event, index) => setOpenIndex(index)} />
      </div>
      <ViewRolloutModal
        event={openIndex === null ? null : events[openIndex]}
        onDismiss={() => setOpenIndex(null)}
        onPause={noop}
        onCancelRemaining={noop}
      />
    </div>
  );
}

export const ActivityActiveNow: Story = {
  name: "Activity — Active now (banners open the modal)",
  render: () => <ActivityActiveNowStory />,
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

// ---- 6. Firmware settings page: active rollout in its detail home ----------
// Reproduces the Firmware settings page chrome (SettingsPageHeader token +
// px-10/pt-10 insets) with the active rollout card as its detail surface.

function FirmwareSettingsPageStory(): ReactElement {
  return (
    <AppShell>
      <div className="flex flex-col gap-6 px-10 pt-10">
        <div className="flex items-center justify-between gap-4">
          <Header title="Firmware" titleSize="text-heading-300" />
          <Button variant={variants.secondary} size={sizes.compact} text="Upload firmware" onClick={noop} />
        </div>
        <ActiveRolloutStatus event={inProgressFirmwareEvent} onPause={noop} onCancelRemaining={noop} />
        <div className="text-emphasis-300 text-text-primary">Firmware files</div>
        <DummyTable
          gridClass="grid-cols-[1.5fr_1fr_0.8fr_0.8fr]"
          columns={["File name", "Target", "Version", "Uploaded"]}
          rows={[
            ["antminer-s21-5.1.0.tar.gz", "Antminer S21", "5.1.0", "Aug 1, 2026"],
            ["antminer-s21-5.0.2.tar.gz", "Antminer S21", "5.0.2", "Jun 12, 2026"],
            ["whatsminer-m60-3.4.1.tar.gz", "Whatsminer M60", "3.4.1", "May 3, 2026"],
          ]}
        />
      </div>
    </AppShell>
  );
}

export const FirmwareSettingsPage: Story = {
  name: "Firmware settings page (active rollout in context)",
  render: () => <FirmwareSettingsPageStory />,
};

// ---- 7. Energy UI: rollout card the way curtailment renders it -------------
// Mirrors CurtailmentManagementPanel's frame (section grid gap-6, Header +
// action buttons row) so a process rollout reads exactly like active
// curtailment does in the energy UI.

function EnergyUiStory(): ReactElement {
  return (
    <AppShell>
      <div className="px-10 pt-10">
        <section className="grid gap-6">
          <div className="flex items-center justify-between gap-4">
            <Header title="Energy" titleSize="text-heading-300" />
            <div className="flex items-center gap-2">
              <Button variant={variants.secondary} size={sizes.base} text="Edit settings" onClick={noop} />
              <Button variant={variants.primary} size={sizes.base} text="Run curtailment" onClick={noop} />
            </div>
          </div>
          <ActiveRolloutStatus event={inProgressCurtailmentEvent} onPause={noop} onCancelRemaining={noop} />
          <div className="text-emphasis-300 text-text-primary">History</div>
          <DummyTable
            gridClass="grid-cols-[1.2fr_1fr_1fr_0.8fr]"
            columns={["Reason", "Scope", "Reduction", "Ended"]}
            rows={[
              ["Peak demand response", "Whole site", "4.0 MW", "2 days ago"],
              ["Grid event", "Building B", "1.8 MW", "4 days ago"],
              ["Scheduled economics", "Whole site", "3.2 MW", "6 days ago"],
            ]}
          />
        </section>
      </div>
    </AppShell>
  );
}

export const EnergyUi: Story = {
  name: "Energy UI (curtailment rollout, as today)",
  render: () => <EnergyUiStory />,
};

// ---- 8. Activity page: rollout banners in the feed ------------------------
// Mirrors ActivityPage chrome (sticky "Activity" header + px-10 insets) with
// the active-rollout banners stacked above the activity table region.

function ActivityPageStory(): ReactElement {
  const events = [inProgressFirmwareEvent, inProgressRebootEvent, inProgressCurtailmentEvent];
  const [openIndex, setOpenIndex] = useState<number | null>(null);
  return (
    <AppShell>
      <div className="px-10 pt-10">
        <div className="pb-6">
          <Header title="Activity" titleSize="text-heading-300" />
        </div>
        <div className="grid gap-3 pb-6">
          <div className="flex items-center justify-between gap-4">
            <div className="text-emphasis-300 text-text-primary">Active now</div>
            <span className="text-200 text-text-primary-70">3 processes running</span>
          </div>
          <ActiveRolloutBannerStack events={events} onView={(_event, index) => setOpenIndex(index)} />
        </div>
        <div className="mb-3 text-emphasis-300 text-text-primary">Recent activity</div>
        <DummyTable
          gridClass="grid-cols-[1.4fr_1fr_1fr_0.9fr]"
          columns={["Event", "Scope", "User", "When"]}
          rows={[
            ["Firmware update to 5.0.2", "Building A, 180 miners", "jmarr", "2 days ago"],
            ["Reboot", "Rack B7, 40 miners", "automation", "3 days ago"],
            ["Curtailment", "Whole site", "automation", "5 days ago"],
            ["Firmware update to 5.0.2", "Building C, 96 miners", "dwitkin", "1 week ago"],
          ]}
        />
      </div>
      <ViewRolloutModal
        event={openIndex === null ? null : events[openIndex]}
        onDismiss={() => setOpenIndex(null)}
        onPause={() => undefined}
        onCancelRemaining={() => undefined}
      />
    </AppShell>
  );
}

export const ActivityPage: Story = {
  name: "Activity page (rollout banners in the feed)",
  render: () => <ActivityPageStory />,
};
