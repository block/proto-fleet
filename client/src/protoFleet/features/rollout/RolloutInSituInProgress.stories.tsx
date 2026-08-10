import { type ReactElement, type ReactNode, useEffect, useState } from "react";
import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { ActivityEntrySchema } from "@/protoFleet/api/generated/activity/v1/activity_pb";
import NavigationMenu from "@/protoFleet/components/NavigationMenu";
import { primaryNavItems } from "@/protoFleet/config/navItems";
import ActivityTable from "@/protoFleet/features/activity/components/ActivityTable";
import CurtailmentHistory from "@/protoFleet/features/energy/CurtailmentHistory";
import { mockCurtailmentHistoryEvents } from "@/protoFleet/features/energy/CurtailmentHistory.fixtures";
import { ActiveRolloutBannerStack } from "@/protoFleet/features/rollout/ActiveRolloutBanner";
import ActiveRolloutStatus from "@/protoFleet/features/rollout/ActiveRolloutStatus";
import {
  FirmwareReleaseChannelsTab,
  FirmwareSettingsSurface,
} from "@/protoFleet/features/rollout/activeRolloutStoryHelpers";
import {
  inProgressCurtailmentEvent,
  inProgressFirmwareEvent,
  inProgressRebootEvent,
  rolloutMinerRowsForEvent,
} from "@/protoFleet/features/rollout/rollout.fixtures";
import RolloutMinersModal from "@/protoFleet/features/rollout/RolloutMinersModal";
import RolloutPill from "@/protoFleet/features/rollout/RolloutPill";
import type { RolloutEvent } from "@/protoFleet/features/rollout/rolloutTypes";
import ViewRolloutModal from "@/protoFleet/features/rollout/ViewRolloutModal";
import { useFleetStore } from "@/protoFleet/store";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";

/**
 * Contextual ("in-situ") **in-progress** surfaces: a rollout once it is live,
 * shown where it lives in the product (header pill, firmware settings, energy,
 * activity feed). Paired with the "Config" bucket, which shows how a rollout is
 * launched. Uses the same shared primitives the real pages use.
 */
const meta = {
  title: "Proto Fleet/Rollout/In Situ/In Progress",
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta;

export default meta;

type Story = StoryObj;

const noop = () => undefined;

function ViewRolloutStoryModal({ event, onDismiss }: { event: RolloutEvent; onDismiss: () => void }): ReactElement {
  const [minersOpen, setMinersOpen] = useState(false);

  return (
    <>
      <ViewRolloutModal
        event={event}
        onDismiss={onDismiss}
        onManage={noop}
        onPause={noop}
        onCancelRemaining={noop}
        onViewMiners={() => setMinersOpen(true)}
      />
      <RolloutMinersModal
        open={minersOpen}
        event={event}
        miners={rolloutMinerRowsForEvent(event)}
        onDismiss={() => setMinersOpen(false)}
      />
    </>
  );
}

/**
 * The real app shell: the `NavigationMenu` sidebar (absolute, w-60) plus a
 * content column inset by that width. Wrap page-context stories in this so they
 * show the navigation + surrounding chrome, not just the page body.
 *
 * Seeds the fleet store with read + settings permissions so the permission-gated
 * primary nav (Fleet / Energy / Activity / Settings) and the settings subnav
 * (Network / Firmware / …) both render, Storybook has no auth session
 * otherwise, which is why the nav previously showed only Home + Settings.
 */
function AppShell({ children }: { children: ReactNode }): ReactElement {
  useEffect(() => {
    useFleetStore
      .getState()
      .auth.setPermissions([
        "fleet:read",
        "miner:read",
        "miner:firmware_update",
        "rack:read",
        "site:read",
        "pool:manage",
        "fleetnode:read",
        "schedule:manage",
        "curtailment:read",
        "curtailment:manage",
        "activity:read",
        "user:read",
        "apikey:manage",
        "serverlog:read",
      ]);
  }, []);
  return (
    <div className="relative min-h-screen overflow-x-hidden bg-surface-base">
      <NavigationMenu items={primaryNavItems} />
      <div className="min-h-screen laptop:pl-16 desktop:pl-50">{children}</div>
    </div>
  );
}

/** Activity rows for the in-situ Activity page story. */
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

// ---- 1. Header bar: the persistent rollout pill (opens the modal) -----------

function HeaderPillStory(): ReactElement {
  const [open, setOpen] = useState(false);
  return (
    <div className="min-h-screen bg-surface-base">
      <div className="flex h-14 items-center justify-between border-b border-border-5 bg-surface-elevated-base px-6">
        <div className="text-emphasis-300 text-text-primary">Denver, Building B</div>
        <RolloutPill event={inProgressFirmwareEvent} onViewRollout={() => setOpen(true)} />
      </div>
      <div className="p-8 text-300 text-text-primary-70">
        Open the pill, then select "View update" to open the progress modal.
      </div>
      {open ? <ViewRolloutStoryModal event={inProgressFirmwareEvent} onDismiss={() => setOpen(false)} /> : null}
    </div>
  );
}

export const HeaderPill: Story = {
  name: "Header pill",
  render: () => <HeaderPillStory />,
};

// ---- 2. Firmware settings page: active rollout in its detail home ----------
// Uses FirmwareSettingsSurface so this story matches the lifecycle stories.

export const FirmwareSettingsPage: Story = {
  name: "Firmware settings page",
  parameters: { withRouter: false },
  render: () => (
    <FirmwareSettingsSurface event={inProgressFirmwareEvent} releaseChannelsTab={<FirmwareReleaseChannelsTab />} />
  ),
};

// ---- 3. Energy UI: rollout card the way curtailment renders it -------------
// Shows a curtailment rollout in the Energy page frame.

function EnergyUiStory(): ReactElement {
  const [minersOpen, setMinersOpen] = useState(false);
  const event = inProgressCurtailmentEvent;

  return (
    <AppShell>
      <div className="px-10 pt-10">
        <section className="grid gap-6">
          <div className="flex items-center justify-between gap-4">
            <Header title="Energy" titleSize="text-heading-300" />
            <div className="flex items-center gap-2">
              <Button variant={variants.secondary} size={sizes.compact} text="Edit settings" onClick={noop} />
              <Button variant={variants.primary} size={sizes.compact} text="Run curtailment" onClick={noop} />
            </div>
          </div>
          <ActiveRolloutStatus
            event={event}
            onManage={noop}
            onPause={noop}
            onCancelRemaining={noop}
            onViewMiners={() => setMinersOpen(true)}
          />
          <CurtailmentHistory events={mockCurtailmentHistoryEvents} pageSize={5} />
        </section>
      </div>
      <RolloutMinersModal
        open={minersOpen}
        event={event}
        miners={rolloutMinerRowsForEvent(event)}
        onDismiss={() => setMinersOpen(false)}
      />
    </AppShell>
  );
}

export const EnergyUi: Story = {
  name: "Energy UI",
  render: () => <EnergyUiStory />,
};

// ---- 4. Activity page: rollout banners in the feed ------------------------
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
      {openIndex === null ? null : (
        <ViewRolloutStoryModal event={events[openIndex]} onDismiss={() => setOpenIndex(null)} />
      )}
    </AppShell>
  );
}

export const ActivityPage: Story = {
  name: "Activity page",
  render: () => <ActivityPageStory />,
};
