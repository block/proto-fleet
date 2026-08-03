import { type ReactElement, type ReactNode, useEffect, useState } from "react";
import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { ActivityEntrySchema } from "@/protoFleet/api/generated/activity/v1/activity_pb";
import NavigationMenu from "@/protoFleet/components/NavigationMenu";
import { primaryNavItems } from "@/protoFleet/config/navItems";
import ActivityTable from "@/protoFleet/features/activity/components/ActivityTable";
import CurtailmentHistory from "@/protoFleet/features/energy/CurtailmentHistory";
import { mockCurtailmentHistoryEvents } from "@/protoFleet/features/energy/CurtailmentHistory.fixtures";
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
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";

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

/**
 * Firmware settings "Firmware files" table, built on the shared `List` (the
 * same component `MinerList` / `ActivityTable` use) rather than bespoke grid
 * markup, so the settings surface reads with the product's real table styling.
 */
interface FirmwareFileRow {
  id: string;
  filename: string;
  target: string;
  version: string;
  uploaded: string;
}

type FirmwareFileColumn = "filename" | "target" | "version" | "uploaded";

const firmwareFileColumns: FirmwareFileColumn[] = ["filename", "target", "version", "uploaded"];

const firmwareFileColTitles: ColTitles<FirmwareFileColumn> = {
  filename: "File name",
  target: "Target",
  version: "Version",
  uploaded: "Uploaded",
};

const firmwareFileColConfig: ColConfig<FirmwareFileRow, string, FirmwareFileColumn> = {
  filename: {
    component: (file) => <span className="text-emphasis-300 text-text-primary">{file.filename}</span>,
    width: "w-[22rem]",
  },
  target: { component: (file) => file.target, width: "w-48" },
  version: { component: (file) => file.version, width: "w-32" },
  uploaded: { component: (file) => file.uploaded, width: "w-40" },
};

const firmwareFileRows: FirmwareFileRow[] = [
  {
    id: "f1",
    filename: "antminer-s21-5.1.0.tar.gz",
    target: "Antminer S21",
    version: "5.1.0",
    uploaded: "Aug 1, 2026",
  },
  {
    id: "f2",
    filename: "antminer-s21-5.0.2.tar.gz",
    target: "Antminer S21",
    version: "5.0.2",
    uploaded: "Jun 12, 2026",
  },
  {
    id: "f3",
    filename: "whatsminer-m60-3.4.1.tar.gz",
    target: "Whatsminer M60",
    version: "3.4.1",
    uploaded: "May 3, 2026",
  },
];

function FirmwareFilesTable(): ReactElement {
  return (
    <List<FirmwareFileRow, string, FirmwareFileColumn>
      activeCols={firmwareFileColumns}
      colTitles={firmwareFileColTitles}
      colConfig={firmwareFileColConfig}
      items={firmwareFileRows}
      itemKey="id"
      total={firmwareFileRows.length}
      itemName={{ singular: "firmware file", plural: "firmware files" }}
      applyColumnWidthsToCells
      stickyFirstColumn={false}
    />
  );
}

/** Dummy ActivityEntry rows for the in-situ Activity page. Built with the real
 * `ActivityEntrySchema` so the reused `ActivityTable` renders them exactly as
 * the product does (icons, formatted descriptions, scope). */
const activityFeedEntries = [
  create(ActivityEntrySchema, {
    eventId: "act-1",
    eventCategory: "device_command",
    eventType: "firmware_update.completed",
    scopeType: "building",
    scopeLabel: "Building A",
    scopeCount: 180,
    actorType: "user",
    username: "jmarr",
    result: "success",
    createdAt: { seconds: 1_785_000_000n },
    batchId: "batch-fw-1",
    metadata: { success_count: 180, failure_count: 0 },
  }),
  create(ActivityEntrySchema, {
    eventId: "act-2",
    eventCategory: "device_command",
    eventType: "reboot.completed",
    scopeType: "rack",
    scopeLabel: "Rack B7",
    scopeCount: 40,
    actorType: "system",
    username: "automation",
    result: "success",
    createdAt: { seconds: 1_784_900_000n },
    batchId: "batch-reboot-1",
    metadata: { success_count: 40, failure_count: 0 },
  }),
  create(ActivityEntrySchema, {
    eventId: "act-3",
    eventCategory: "curtailment",
    eventType: "curtailment_started",
    scopeType: "site",
    scopeLabel: "Whole site",
    scopeCount: 512,
    actorType: "system",
    username: "automation",
    result: "success",
    createdAt: { seconds: 1_784_800_000n },
  }),
  create(ActivityEntrySchema, {
    eventId: "act-4",
    eventCategory: "device_command",
    eventType: "firmware_update.completed",
    scopeType: "building",
    scopeLabel: "Building C",
    scopeCount: 96,
    actorType: "user",
    username: "dwitkin",
    result: "failure",
    createdAt: { seconds: 1_784_600_000n },
    batchId: "batch-fw-2",
    metadata: { success_count: 92, failure_count: 4 },
  }),
];

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
  const [open, setOpen] = useState(false);
  const fleetColConfig: ColConfig<FleetRow, string, "miner" | "model" | "status" | "firmware"> = {
    miner: {
      component: (row) => <span className="text-emphasis-300 text-text-primary">{row.miner}</span>,
      width: "w-32",
    },
    model: { component: (row) => row.model, width: "w-48" },
    status: { component: (row) => row.status, width: "w-32" },
    firmware: {
      component: (row) => <RolloutColumnState phase={row.phase} doneLabel={row.doneLabel} idleLabel={row.idleLabel} />,
      width: "w-48",
    },
  };
  return (
    <div className="min-h-screen bg-surface-base p-8">
      <div className="mx-auto flex max-w-4xl flex-col gap-4">
        <div className="text-heading-300 text-text-primary">Building B</div>
        <ActiveRolloutBanner event={inProgressFirmwareEvent} onView={() => setOpen(true)} />
        <List<FleetRow, string, "miner" | "model" | "status" | "firmware">
          activeCols={["miner", "model", "status", "firmware"]}
          colTitles={{ miner: "Miner", model: "Model", status: "Status", firmware: "Firmware" }}
          colConfig={fleetColConfig}
          items={fleetRows}
          itemKey="miner"
          total={fleetRows.length}
          itemName={{ singular: "miner", plural: "miners" }}
          applyColumnWidthsToCells
          stickyFirstColumn={false}
        />
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
        <FirmwareFilesTable />
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
          <CurtailmentHistory events={mockCurtailmentHistoryEvents} pageSize={5} />
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
        <ActivityTable activities={activityFeedEntries} />
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
