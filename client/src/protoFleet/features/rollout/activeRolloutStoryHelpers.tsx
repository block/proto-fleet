import { type ReactElement, type ReactNode, useEffect, useMemo, useState } from "react";
import { MemoryRouter } from "react-router-dom";

import NavigationMenu from "@/protoFleet/components/NavigationMenu";
import SecondaryNavigation from "@/protoFleet/components/SecondaryNavigation";
import { primaryNavItems, secondaryNavItems } from "@/protoFleet/config/navItems";
import type { ActiveCurtailmentEvent } from "@/protoFleet/features/energy/ActiveCurtailmentStatus";
import {
  type CurtailmentTargetRollup,
  formatCurtailmentElapsedDuration,
} from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import CurtailmentHistory from "@/protoFleet/features/energy/CurtailmentHistory";
import { mockCurtailmentHistoryEvents } from "@/protoFleet/features/energy/CurtailmentHistory.fixtures";
import CurtailmentStartModal, {
  type CurtailmentFormValues,
  type CurtailmentPlanPreview,
} from "@/protoFleet/features/energy/CurtailmentStartModal";
import CurtailmentStopConfirmationDialog, {
  type CurtailmentStopConfirmationAction,
} from "@/protoFleet/features/energy/CurtailmentStopConfirmationDialog";
import ActiveCurtailmentRolloutStatus from "@/protoFleet/features/rollout/ActiveCurtailmentRolloutStatus";
import { ActiveRolloutBanner } from "@/protoFleet/features/rollout/ActiveRolloutBanner";
import ActiveRolloutStatus from "@/protoFleet/features/rollout/ActiveRolloutStatus";
import { releaseChannels } from "@/protoFleet/features/rollout/releaseChannel.fixtures";
import ReleaseChannelsTable from "@/protoFleet/features/rollout/ReleaseChannelsTable";
import { rolloutMinerRowsForEvent } from "@/protoFleet/features/rollout/rollout.fixtures";
import { inScopeTargetCount, rolloutPhaseCount } from "@/protoFleet/features/rollout/rolloutDisplayUtils";
import RolloutMinersModal, { type RolloutMinerFilter } from "@/protoFleet/features/rollout/RolloutMinersModal";
import type { RolloutEvent, RolloutPhaseRollup } from "@/protoFleet/features/rollout/rolloutTypes";
import { useFleetStore } from "@/protoFleet/store";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import TabStrip, { TabStripItem } from "@/shared/components/Tab/TabStrip";

/**
 * Shared Storybook helpers for firmware and reboot rollout lifecycle stories.
 * They render the rollout card inside the page where an operator would see it.
 */

const noop = (): void => undefined;

/**
 * The real app shell: the `NavigationMenu` sidebar (absolute, w-60) plus a
 * content column inset by that width. Seeds the fleet store with read +
 * settings permissions so the permission-gated primary nav renders (Storybook
 * has no auth session otherwise). Shared by every in-situ rollout story.
 */
export function AppShell({ children }: { children: ReactNode }): ReactElement {
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

/**
 * Inline rollout card with story handlers for every lifecycle control.
 */
function InlineRolloutCard({
  event,
  defaultDetailsOpen = false,
  onContinueFromReview = noop,
}: {
  event: RolloutEvent;
  defaultDetailsOpen?: boolean;
  onContinueFromReview?: () => void;
}): ReactElement {
  const [minerModalFilter, setMinerModalFilter] = useState<RolloutMinerFilter | null>(null);
  const rolloutMiners = useMemo(() => rolloutMinerRowsForEvent(event), [event]);

  return (
    <>
      <ActiveRolloutStatus
        event={event}
        onManage={noop}
        onPause={noop}
        onResume={noop}
        onCancelRemaining={noop}
        onContinueFromReview={onContinueFromReview}
        onRetryFailed={noop}
        onViewMiners={() => setMinerModalFilter("all")}
        onViewErrors={() => setMinerModalFilter("errors")}
        defaultDetailsOpen={defaultDetailsOpen}
      />
      <RolloutMinersModal
        key={minerModalFilter ?? "closed"}
        open={minerModalFilter !== null}
        event={event}
        miners={rolloutMiners}
        initialFilter={minerModalFilter ?? "all"}
        onDismiss={() => setMinerModalFilter(null)}
      />
    </>
  );
}

// ---- Firmware settings page surface ----------------------------------------
// Firmware rollout home: settings subnav, header, active card, and files table.

interface FirmwareFileRow {
  id: string;
  filename: string;
  target: string;
  version: string;
  uploaded: string;
}

/** Which tab the Firmware settings page shows when the tab row is enabled. */
export type FirmwareSettingsTab = "files" | "releaseChannels";

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

/** Firmware settings "Firmware files" table. */
export function FirmwareFilesTable(): ReactElement {
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

/** Default release-channel tab content for firmware in-situ stories. */
export function FirmwareReleaseChannelsTab(): ReactElement {
  return <ReleaseChannelsTable channels={releaseChannels} onCreate={noop} onManage={noop} />;
}

/**
 * Firmware settings page used by rollout stories. Pass `event` to show an
 * active rollout above the firmware tab navigation.
 */
export function FirmwareSettingsSurface({
  event,
  rolloutBanner,
  releaseChannelsTab,
  initialTab = "files",
  defaultDetailsOpen = false,
  onContinueFromReview,
}: {
  event?: RolloutEvent | null;
  rolloutBanner?: ReactNode;
  releaseChannelsTab?: ReactNode;
  initialTab?: FirmwareSettingsTab;
  defaultDetailsOpen?: boolean;
  onContinueFromReview?: () => void;
}): ReactElement {
  const [activeTab, setActiveTab] = useState<FirmwareSettingsTab>(initialTab);
  const showTabs = releaseChannelsTab !== undefined;
  const showReleaseChannels = showTabs && activeTab === "releaseChannels";
  const rolloutStatus = (
    <>
      {rolloutBanner}
      {event ? (
        <InlineRolloutCard
          event={event}
          defaultDetailsOpen={defaultDetailsOpen}
          onContinueFromReview={onContinueFromReview}
        />
      ) : null}
    </>
  );

  return (
    <MemoryRouter initialEntries={["/settings/firmware"]}>
      <AppShell>
        <div className="flex h-full grow flex-row">
          <SecondaryNavigation items={secondaryNavItems} />
          <div className="flex min-w-0 grow flex-col gap-6 p-10">
            {showTabs ? (
              <>
                <Header title="Firmware" titleSize="text-heading-300" />
                {rolloutStatus}
                <TabStrip
                  activeId={activeTab}
                  onSelect={(id) => setActiveTab(id as FirmwareSettingsTab)}
                  ariaLabel="Firmware views"
                >
                  <TabStripItem id="files" label="Files" />
                  <TabStripItem id="releaseChannels" label="Release channels" />
                </TabStrip>
              </>
            ) : (
              <div className="flex items-center justify-between gap-4">
                <Header
                  title="Firmware"
                  titleSize="text-heading-300"
                  description="Upload and manage firmware files available to your fleet."
                />
                <Button variant={variants.primary} size={sizes.compact} text="Upload firmware" onClick={noop} />
              </div>
            )}
            {showTabs ? null : rolloutStatus}

            {showReleaseChannels ? (
              releaseChannelsTab
            ) : (
              <>
                {showTabs ? (
                  <div>
                    <Button variant={variants.primary} size={sizes.compact} text="Upload firmware" onClick={noop} />
                  </div>
                ) : null}
                <FirmwareFilesTable />
              </>
            )}
          </div>
        </div>
      </AppShell>
    </MemoryRouter>
  );
}

// ---- Fleet page surface (reboot) -------------------------------------------
// Reboot has no dedicated page, it is a Fleet bulk action (FleetGroupActionsMenu),
// so its active rollout surfaces inline on the Fleet page. Mirrors the firmware
// surface's inline treatment (Header + primary CTA row, rollout card inline
// above the list) using the same shared primitives, under a MemoryRouter at
// /fleet so the Fleet nav item is marked current.

interface RackRow {
  id: string;
  name: string;
  building: string;
  miners: string;
  status: string;
}

type RackColumn = "name" | "building" | "miners" | "status";

const rackColumns: RackColumn[] = ["name", "building", "miners", "status"];

const rackColTitles: ColTitles<RackColumn> = {
  name: "Rack",
  building: "Building",
  miners: "Miners",
  status: "Status",
};

const rackColConfig: ColConfig<RackRow, string, RackColumn> = {
  name: {
    component: (rack) => <span className="text-emphasis-300 text-text-primary">{rack.name}</span>,
    width: "w-48",
  },
  building: { component: (rack) => rack.building, width: "w-48" },
  miners: { component: (rack) => rack.miners, width: "w-32" },
  status: { component: (rack) => rack.status, width: "w-40" },
};

const rackRows: RackRow[] = [
  { id: "r1", name: "Rack A3", building: "Building A", miners: "40 miners", status: "Rebooting" },
  { id: "r2", name: "Rack A4", building: "Building A", miners: "40 miners", status: "Rebooting" },
  { id: "r3", name: "Rack B1", building: "Building B", miners: "38 miners", status: "Queued" },
  { id: "r4", name: "Rack B2", building: "Building B", miners: "40 miners", status: "Queued" },
];

function FleetRacksTable(): ReactElement {
  return (
    <List<RackRow, string, RackColumn>
      activeCols={rackColumns}
      colTitles={rackColTitles}
      colConfig={rackColConfig}
      items={rackRows}
      itemKey="id"
      total={rackRows.length}
      itemName={{ singular: "rack", plural: "racks" }}
      applyColumnWidthsToCells
      stickyFirstColumn={false}
    />
  );
}

/**
 * The Fleet page with an active reboot rollout shown inline above the rack
 * list. Fleet is the reboot surface because there is no reboot settings page.
 */
export function FleetSurface({
  event,
  rolloutBanner,
  defaultDetailsOpen = false,
  onContinueFromReview,
}: {
  event?: RolloutEvent | null;
  rolloutBanner?: ReactNode;
  defaultDetailsOpen?: boolean;
  onContinueFromReview?: () => void;
}): ReactElement {
  return (
    <MemoryRouter initialEntries={["/fleet"]}>
      <AppShell>
        <div className="flex min-w-0 grow flex-col gap-6 p-10">
          <div className="flex items-center justify-between gap-4">
            <Header title="Fleet" titleSize="text-heading-300" />
            <div className="flex items-center gap-2">
              <Button variant={variants.secondary} size={sizes.compact} text="Reboot" onClick={noop} />
              <Button variant={variants.primary} size={sizes.compact} text="Update firmware" onClick={noop} />
            </div>
          </div>
          {rolloutBanner}
          {event ? (
            <InlineRolloutCard
              event={event}
              defaultDetailsOpen={defaultDetailsOpen}
              onContinueFromReview={onContinueFromReview}
            />
          ) : null}
          <FleetRacksTable />
        </div>
      </AppShell>
    </MemoryRouter>
  );
}

// ---- Per-process in-situ entry points --------------------------------------

/** A firmware rollout state, shown inline on the Firmware settings page. */
export function FirmwareInSitu({ event }: { event: RolloutEvent }): ReactElement {
  return <FirmwareSettingsSurface event={event} releaseChannelsTab={<FirmwareReleaseChannelsTab />} />;
}

/** A reboot rollout state, shown inline on the Fleet page. */
export function RebootInSitu({ event }: { event: RolloutEvent }): ReactElement {
  return <FleetSurface event={event} />;
}

// ---- Energy page surface (curtailment) -------------------------------------

function formatCurtailmentEstimate(seconds: number): string {
  return seconds > 0 ? `~${formatCurtailmentElapsedDuration(seconds)}` : "Immediate";
}

function curtailmentEditorValues(event: ActiveCurtailmentEvent): Partial<CurtailmentFormValues> {
  return {
    curtailmentMode: "fixedKwReduction",
    targetKw: String(event.targetKw ?? event.estimatedReductionKw),
    curtailBatchSize: event.curtailBatchSize ? String(event.curtailBatchSize) : "",
    curtailBatchIntervalSec: event.curtailBatchIntervalSec ? String(event.curtailBatchIntervalSec) : "",
    restoreBatchSize: event.restoreBatchSize ? String(event.restoreBatchSize) : "",
    restoreIntervalSec: event.restoreBatchIntervalSec ? String(event.restoreBatchIntervalSec) : "",
    reason: event.reason,
  };
}

function curtailmentEditorPreview(event: ActiveCurtailmentEvent): CurtailmentPlanPreview {
  const curtailBatchSize = Math.max(event.curtailBatchSize ?? event.selectedMiners, 1);
  const curtailBatches = Math.ceil(event.selectedMiners / curtailBatchSize);
  const curtailSeconds = Math.max(curtailBatches - 1, 0) * (event.curtailBatchIntervalSec ?? 0);
  const restoreBatchSize = Math.max(event.restoreBatchSize || event.selectedMiners, 1);
  const restoreBatches = Math.ceil(event.selectedMiners / restoreBatchSize);
  const restoreSeconds = Math.max(restoreBatches - 1, 0) * event.restoreBatchIntervalSec;

  return {
    selectedMinerCount: event.selectedMiners,
    facilityFanDeviceCount: event.facilityFanDeviceCount,
    targetKw: event.targetKw ?? event.estimatedReductionKw,
    estimatedReductionKw: event.estimatedReductionKw,
    curtailEstimate: formatCurtailmentEstimate(curtailSeconds),
    restoreEstimate: formatCurtailmentEstimate(restoreSeconds),
    scopeLabel: event.scopeLabel,
  };
}

function scheduledCurtailmentEditorEvent(event: RolloutEvent): ActiveCurtailmentEvent {
  const selectedMiners = inScopeTargetCount(event);
  return {
    reason: event.title,
    state: "pending",
    scopeLabel: event.scopeLabel,
    sourceLabel: "Manual",
    isAutomationOwned: false,
    selectedMiners,
    estimatedReductionKw: 540,
    targetKw: 540,
    observedReductionKw: 0,
    curtailBatchSize: event.batchSize,
    curtailBatchIntervalSec: event.batchIntervalSec,
    restoreBatchSize: event.batchSize ?? selectedMiners,
    restoreBatchIntervalSec: 120,
    rollups: [{ state: "pending", count: selectedMiners }],
  };
}

function CurtailmentEditor({
  event,
  onDismiss,
}: {
  event: ActiveCurtailmentEvent;
  onDismiss: () => void;
}): ReactElement {
  return (
    <CurtailmentStartModal
      open
      mode="edit"
      initialValues={curtailmentEditorValues(event)}
      preview={curtailmentEditorPreview(event)}
      onDismiss={onDismiss}
      onSubmit={onDismiss}
      onStopCurtailment={onDismiss}
    />
  );
}

function CurtailmentStatusCard({
  event,
  rolloutEvent,
  defaultDetailsOpen = false,
  onContinueFromReview = noop,
}: {
  event: ActiveCurtailmentEvent;
  rolloutEvent?: RolloutEvent;
  defaultDetailsOpen?: boolean;
  onContinueFromReview?: () => void;
}): ReactElement {
  const [manageOpen, setManageOpen] = useState(false);
  const [confirmationAction, setConfirmationAction] = useState<CurtailmentStopConfirmationAction>();
  const rolloutMiners = useMemo(() => (rolloutEvent ? rolloutMinerRowsForEvent(rolloutEvent) : []), [rolloutEvent]);

  return (
    <>
      <ActiveCurtailmentRolloutStatus
        event={event}
        rolloutEvent={rolloutEvent}
        miners={rolloutMiners}
        defaultDetailsOpen={defaultDetailsOpen}
        onManage={() => setManageOpen(true)}
        onPause={noop}
        onResume={noop}
        onAbort={noop}
        onContinueFromReview={onContinueFromReview}
        onRestore={() => setConfirmationAction("restore")}
        onRestoreNow={() => setConfirmationAction("restore")}
        onStopRestore={noop}
        onDismiss={noop}
      />
      {manageOpen ? <CurtailmentEditor event={event} onDismiss={() => setManageOpen(false)} /> : null}
      <CurtailmentStopConfirmationDialog
        open={confirmationAction !== undefined}
        action={confirmationAction ?? "restore"}
        onCancel={() => setConfirmationAction(undefined)}
        onConfirm={() => setConfirmationAction(undefined)}
      />
    </>
  );
}

function curtailmentEventFromRollout(event: RolloutEvent): ActiveCurtailmentEvent {
  const selectedMiners = inScopeTargetCount(event);
  const done = rolloutPhaseCount(event.rollups, "done");
  const dispatched = rolloutPhaseCount(event.rollups, "inProgress") + rolloutPhaseCount(event.rollups, "retrying");
  const pending = rolloutPhaseCount(event.rollups, "queued");
  const unavailable = rolloutPhaseCount(event.rollups, "failed");
  const targetKw = 540;
  const observedReductionKw = selectedMiners > 0 ? targetKw * (done / selectedMiners) : 0;
  const rollups = (
    [
      { state: "confirmed", count: done },
      { state: "dispatched", count: dispatched },
      { state: "pending", count: pending },
      { state: "unavailable", count: unavailable },
    ] satisfies CurtailmentTargetRollup[]
  ).filter((rollup) => rollup.count > 0);

  return {
    reason: event.title,
    state: event.state === "completedWithFailures" ? "completedWithFailures" : "active",
    scopeLabel: event.scopeLabel,
    sourceLabel: "Manual",
    isAutomationOwned: false,
    startedAt: event.startedAt,
    scheduledStartAt: event.scheduledStartAt,
    selectedMiners,
    estimatedReductionKw: targetKw,
    targetKw,
    observedReductionKw,
    remainingPowerKw: Math.max(840 - observedReductionKw, 0),
    curtailBatchSize: event.batchSize,
    curtailBatchIntervalSec: event.batchIntervalSec,
    restoreBatchSize: event.batchSize ?? selectedMiners,
    restoreBatchIntervalSec: 120,
    rollups,
  };
}

/**
 * The Energy page with an active curtailment rollout shown inline above history.
 */
export function EnergySurface({
  event,
  curtailmentEvent,
  scheduledEvent,
  rolloutBanner,
  defaultDetailsOpen = false,
  onContinueFromReview,
}: {
  event?: RolloutEvent | null;
  curtailmentEvent?: ActiveCurtailmentEvent | null;
  scheduledEvent?: RolloutEvent | null;
  rolloutBanner?: ReactNode;
  defaultDetailsOpen?: boolean;
  onContinueFromReview?: () => void;
}): ReactElement {
  const [scheduledManageOpen, setScheduledManageOpen] = useState(false);
  const scheduledEditorEvent = scheduledEvent ? scheduledCurtailmentEditorEvent(scheduledEvent) : null;
  const resolvedCurtailmentEvent =
    curtailmentEvent ?? (event?.processType === "curtailment" ? curtailmentEventFromRollout(event) : null);

  return (
    <MemoryRouter initialEntries={["/energy"]}>
      <AppShell>
        <div className="p-6 laptop:p-10">
          <section className="grid gap-6">
            <div className="flex items-center justify-between gap-4 phone:flex-col phone:items-stretch">
              <Header title="Curtailment" titleSize="text-heading-300" />
              <div className="flex items-center gap-2 phone:flex-col phone:items-stretch">
                <Button
                  variant={variants.secondary}
                  size={sizes.base}
                  text="Edit settings"
                  onClick={noop}
                  className="phone:w-full"
                />
                <Button
                  variant={variants.primary}
                  size={sizes.base}
                  text="Run curtailment"
                  onClick={noop}
                  className="phone:w-full"
                />
              </div>
            </div>
            {scheduledEvent ? (
              <ActiveRolloutBanner event={scheduledEvent} onManage={() => setScheduledManageOpen(true)} />
            ) : null}
            {rolloutBanner}
            {resolvedCurtailmentEvent ? (
              <CurtailmentStatusCard
                event={resolvedCurtailmentEvent}
                rolloutEvent={event ?? undefined}
                defaultDetailsOpen={defaultDetailsOpen}
                onContinueFromReview={onContinueFromReview}
              />
            ) : null}
            {event && !resolvedCurtailmentEvent ? (
              <InlineRolloutCard
                event={event}
                defaultDetailsOpen={defaultDetailsOpen}
                onContinueFromReview={onContinueFromReview}
              />
            ) : null}
            <CurtailmentHistory events={mockCurtailmentHistoryEvents} pageSize={5} />
          </section>
        </div>
        {scheduledManageOpen && scheduledEditorEvent ? (
          <CurtailmentEditor event={scheduledEditorEvent} onDismiss={() => setScheduledManageOpen(false)} />
        ) : null}
      </AppShell>
    </MemoryRouter>
  );
}

/** A curtailment rollout state, shown inline on the Energy page. */
export function CurtailmentInSitu({ event }: { event: RolloutEvent }): ReactElement {
  return <EnergySurface event={event} />;
}

/** Existing curtailment lifecycle state, shown in the same in-situ Energy shell. */
export function CurtailmentStatusInSitu({
  event,
  rolloutEvent,
}: {
  event: ActiveCurtailmentEvent;
  rolloutEvent?: RolloutEvent;
}): ReactElement {
  return <EnergySurface event={rolloutEvent} curtailmentEvent={event} />;
}

const animationStepPercent = 10;
const animationStepMs = 450;
const completedHoldMs = 2600;
const reviewSeriesStepMs = 1500;

/**
 * Derive an in-flight (or just-finished) rollout at a given completion percent
 * from a base in-progress fixture. Mirrors curtailment's `buildAnimatedEvent`:
 * the done count grows, one batch sits in progress, the rest stays queued, and
 * at 100% the event flips to `completed`. Excluded targets pass through
 * untouched (they're never in the bar).
 */
function buildAnimatedRolloutEvent(base: RolloutEvent, donePercent: number, startedAt: string): RolloutEvent {
  const inScope = inScopeTargetCount(base);
  const done = Math.round((inScope * donePercent) / 100);
  const remaining = Math.max(inScope - done, 0);
  const isComplete = donePercent >= 100;
  const activeBatch = isComplete ? 0 : Math.min(base.batchSize ?? remaining, remaining);
  const queued = Math.max(remaining - activeBatch, 0);

  const rollups: RolloutPhaseRollup[] = [
    { phase: "done", count: done },
    { phase: "inProgress", count: activeBatch },
    { phase: "queued", count: queued },
  ];
  if (base.excludedTargets > 0) {
    rollups.push({ phase: "excluded", count: base.excludedTargets });
  }

  const batchSize = base.batchSize ?? inScope;
  const currentBatch =
    base.totalBatches && batchSize > 0
      ? Math.min(Math.floor(done / batchSize) + 1, base.totalBatches)
      : base.currentBatch;

  return {
    ...base,
    state: isComplete ? "completed" : "inProgress",
    startedAt,
    currentBatch,
    estimatedSecondsRemaining: isComplete ? 0 : base.estimatedSecondsRemaining,
    performance: base.performance,
    errors: isComplete ? undefined : base.errors,
    rollups,
  };
}

/**
 * A base rollout ticking from 0% to 100% done on a loop. `startedAt` resets
 * each loop so the card's elapsed timer counts up from zero.
 */
function useAnimatedRolloutEvent(base: RolloutEvent): RolloutEvent {
  const [donePercent, setDonePercent] = useState(0);
  const [startedAt, setStartedAt] = useState(() => new Date().toISOString());

  useEffect(() => {
    if (donePercent >= 100) {
      const timeoutId = window.setTimeout(() => {
        setDonePercent(0);
        setStartedAt(new Date().toISOString());
      }, completedHoldMs);
      return () => window.clearTimeout(timeoutId);
    }

    const intervalId = window.setInterval(() => {
      setDonePercent((current) => Math.min(current + animationStepPercent, 100));
    }, animationStepMs);
    return () => window.clearInterval(intervalId);
  }, [donePercent]);

  return useMemo(() => buildAnimatedRolloutEvent(base, donePercent, startedAt), [base, donePercent, startedAt]);
}

function waveCount(base: RolloutEvent): number {
  const inScope = inScopeTargetCount(base);
  if (base.strategy === "pilotThenContinue") {
    const pilotSize = Math.min(base.pilotSize ?? inScope, inScope);
    return inScope - pilotSize > 0 ? 2 : 1;
  }

  if (base.totalBatches) {
    return base.totalBatches;
  }

  const batchSize = Math.max(base.batchSize ?? inScope, 1);
  return Math.max(Math.ceil(inScope / batchSize), 1);
}

function reviewSeriesLength(base: RolloutEvent): number {
  // Each reviewed wave runs, waits for telemetry to stabilize, then pauses for
  // review. The final step is completed, so the last wave does not pause again.
  return waveCount(base) * 3 - 1;
}

function waveSize(base: RolloutEvent, waveNumber: number): number {
  const inScope = inScopeTargetCount(base);
  const batchSize = Math.max(base.batchSize ?? inScope, 1);
  if (base.strategy === "pilotThenContinue" && waveNumber === 1) {
    return Math.min(base.pilotSize ?? batchSize, inScope);
  }
  if (base.strategy === "pilotThenContinue") {
    return Math.max(inScope - Math.min(base.pilotSize ?? batchSize, inScope), 0);
  }

  const doneBeforeWave = (waveNumber - 1) * batchSize;
  return Math.min(batchSize, Math.max(inScope - doneBeforeWave, 0));
}

function doneCountForWaves(base: RolloutEvent, completedWaves: number): number {
  let done = 0;
  for (let waveNumber = 1; waveNumber <= completedWaves; waveNumber += 1) {
    done += waveSize(base, waveNumber);
  }
  return Math.min(done, inScopeTargetCount(base));
}

function buildReviewSeriesEvent(base: RolloutEvent, stepIndex: number, startedAt: string): RolloutEvent {
  const inScope = inScopeTargetCount(base);
  const totalBatches = waveCount(base);
  const normalizedStep = Math.min(stepIndex, reviewSeriesLength(base) - 1);
  const isComplete = normalizedStep === reviewSeriesLength(base) - 1;
  const stepWithinWave = normalizedStep % 3;
  const isTelemetryStabilizing = !isComplete && stepWithinWave === 1;
  const isReviewGate = !isComplete && stepWithinWave === 2;
  const currentBatch = Math.min(Math.floor(normalizedStep / 3) + 1, totalBatches);
  const completedWaves = isComplete ? totalBatches : Math.max(currentBatch - (stepWithinWave === 0 ? 1 : 0), 0);
  const done = doneCountForWaves(base, completedWaves);
  const remaining = Math.max(inScope - done, 0);
  const activeBatch =
    isComplete || isTelemetryStabilizing || isReviewGate ? 0 : Math.min(waveSize(base, currentBatch), remaining);
  const queued = Math.max(remaining - activeBatch, 0);
  const rollups: RolloutPhaseRollup[] = [
    { phase: "done", count: done },
    { phase: "inProgress", count: activeBatch },
    { phase: "queued", count: queued },
  ];
  if (base.excludedTargets > 0) {
    rollups.push({ phase: "excluded", count: base.excludedTargets });
  }

  return {
    ...base,
    state: isComplete
      ? "completed"
      : isTelemetryStabilizing
        ? "stabilizingTelemetry"
        : isReviewGate && base.strategy === "pilotThenContinue" && currentBatch === 1
          ? "pausedAtPilotGate"
          : isReviewGate
            ? "pausedAtBatchReview"
            : "inProgress",
    startedAt,
    currentBatch,
    estimatedSecondsRemaining: isComplete
      ? 0
      : isTelemetryStabilizing
        ? 1_800
        : Math.max((totalBatches - completedWaves) * (base.batchIntervalSec ?? 0), 0),
    performance: isTelemetryStabilizing ? undefined : base.performance,
    errors: isComplete ? undefined : base.errors,
    rollups,
  };
}

function useAnimatedReviewSeriesEvent(base: RolloutEvent): { event: RolloutEvent; continueFromReview: () => void } {
  const [stepIndex, setStepIndex] = useState(0);
  const [startedAt, setStartedAt] = useState(() => new Date().toISOString());
  const seriesLength = reviewSeriesLength(base);
  const event = useMemo(() => buildReviewSeriesEvent(base, stepIndex, startedAt), [base, startedAt, stepIndex]);

  useEffect(() => {
    if (event.state !== "inProgress" && event.state !== "stabilizingTelemetry") {
      return;
    }

    const timeoutId = window.setTimeout(() => {
      setStepIndex((current) => {
        return Math.min(current + 1, seriesLength - 1);
      });
    }, reviewSeriesStepMs);
    return () => window.clearTimeout(timeoutId);
  }, [event.state, seriesLength]);

  useEffect(() => {
    if (event.state !== "completed") {
      return;
    }

    const timeoutId = window.setTimeout(() => {
      setStepIndex(0);
      setStartedAt(new Date().toISOString());
    }, completedHoldMs);
    return () => window.clearTimeout(timeoutId);
  }, [event.state]);

  const continueFromReview = (): void => {
    if (event.state !== "pausedAtPilotGate" && event.state !== "pausedAtBatchReview") {
      return;
    }
    setStepIndex((current) => Math.min(current + 1, seriesLength - 1));
  };

  return { event, continueFromReview };
}

/** The animated firmware lifecycle, shown inline on the Firmware settings page. */
export function AnimatedFirmwareInSitu({
  base,
  defaultDetailsOpen = false,
}: {
  base: RolloutEvent;
  defaultDetailsOpen?: boolean;
}): ReactElement {
  const event = useAnimatedRolloutEvent(base);
  return (
    <FirmwareSettingsSurface
      event={event}
      releaseChannelsTab={<FirmwareReleaseChannelsTab />}
      defaultDetailsOpen={defaultDetailsOpen}
    />
  );
}

/** A batched firmware update that pauses for review after every batch. */
export function AnimatedFirmwareBatchReviewSeriesInSitu({
  base,
  defaultDetailsOpen = false,
}: {
  base: RolloutEvent;
  defaultDetailsOpen?: boolean;
}): ReactElement {
  const { event, continueFromReview } = useAnimatedReviewSeriesEvent(base);
  return (
    <FirmwareSettingsSurface
      event={event}
      releaseChannelsTab={<FirmwareReleaseChannelsTab />}
      defaultDetailsOpen={defaultDetailsOpen}
      onContinueFromReview={continueFromReview}
    />
  );
}

/** A pilot firmware update that pauses at review gates until Continue is clicked. */
export function AnimatedFirmwarePilotReviewSeriesInSitu({
  base,
  defaultDetailsOpen = false,
}: {
  base: RolloutEvent;
  defaultDetailsOpen?: boolean;
}): ReactElement {
  const { event, continueFromReview } = useAnimatedReviewSeriesEvent(base);
  return (
    <FirmwareSettingsSurface
      event={event}
      releaseChannelsTab={<FirmwareReleaseChannelsTab />}
      defaultDetailsOpen={defaultDetailsOpen}
      onContinueFromReview={continueFromReview}
    />
  );
}

/** The animated reboot lifecycle, shown inline on the Fleet page. */
export function AnimatedRebootInSitu({
  base,
  defaultDetailsOpen = false,
}: {
  base: RolloutEvent;
  defaultDetailsOpen?: boolean;
}): ReactElement {
  const event = useAnimatedRolloutEvent(base);
  return <FleetSurface event={event} defaultDetailsOpen={defaultDetailsOpen} />;
}

/** A batched reboot that pauses for review after every batch. */
export function AnimatedRebootBatchReviewSeriesInSitu({
  base,
  defaultDetailsOpen = false,
}: {
  base: RolloutEvent;
  defaultDetailsOpen?: boolean;
}): ReactElement {
  const { event, continueFromReview } = useAnimatedReviewSeriesEvent(base);
  return (
    <FleetSurface event={event} defaultDetailsOpen={defaultDetailsOpen} onContinueFromReview={continueFromReview} />
  );
}

/** A pilot reboot that pauses at review gates until Continue is clicked. */
export function AnimatedRebootPilotReviewSeriesInSitu({
  base,
  defaultDetailsOpen = false,
}: {
  base: RolloutEvent;
  defaultDetailsOpen?: boolean;
}): ReactElement {
  const { event, continueFromReview } = useAnimatedReviewSeriesEvent(base);
  return (
    <FleetSurface event={event} defaultDetailsOpen={defaultDetailsOpen} onContinueFromReview={continueFromReview} />
  );
}

/** The animated curtailment lifecycle, shown inline on the Energy page. */
export function AnimatedCurtailmentInSitu({
  base,
  defaultDetailsOpen = false,
}: {
  base: RolloutEvent;
  defaultDetailsOpen?: boolean;
}): ReactElement {
  const event = useAnimatedRolloutEvent(base);
  return <EnergySurface event={event} defaultDetailsOpen={defaultDetailsOpen} />;
}

/** A batched curtailment that pauses for review after every batch. */
export function AnimatedCurtailmentBatchReviewSeriesInSitu({
  base,
  defaultDetailsOpen = false,
}: {
  base: RolloutEvent;
  defaultDetailsOpen?: boolean;
}): ReactElement {
  const { event, continueFromReview } = useAnimatedReviewSeriesEvent(base);
  return (
    <EnergySurface event={event} defaultDetailsOpen={defaultDetailsOpen} onContinueFromReview={continueFromReview} />
  );
}

/** A pilot curtailment that pauses at review gates until Continue is clicked. */
export function AnimatedCurtailmentPilotReviewSeriesInSitu({
  base,
  defaultDetailsOpen = false,
}: {
  base: RolloutEvent;
  defaultDetailsOpen?: boolean;
}): ReactElement {
  const { event, continueFromReview } = useAnimatedReviewSeriesEvent(base);
  return (
    <EnergySurface event={event} defaultDetailsOpen={defaultDetailsOpen} onContinueFromReview={continueFromReview} />
  );
}
