import { type ReactElement, type ReactNode, useEffect, useMemo, useState } from "react";
import { MemoryRouter } from "react-router-dom";

import NavigationMenu from "@/protoFleet/components/NavigationMenu";
import SecondaryNavigation from "@/protoFleet/components/SecondaryNavigation";
import { primaryNavItems, secondaryNavItems } from "@/protoFleet/config/navItems";
import ActiveRolloutStatus from "@/protoFleet/features/rollout/ActiveRolloutStatus";
import { inScopeTargetCount } from "@/protoFleet/features/rollout/rolloutDisplayUtils";
import type { RolloutEvent, RolloutPhaseRollup } from "@/protoFleet/features/rollout/rolloutTypes";
import { useFleetStore } from "@/protoFleet/store";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import TabStrip, { TabStripItem } from "@/shared/components/Tab/TabStrip";

/**
 * Shared Storybook glue for the per-process "active rollout" lifecycle story
 * files (firmware update, reboot). Curtailment's `ActiveCurtailmentStatus`
 * stories showcase each lifecycle state plus one live-animated lifecycle; these
 * helpers do the same against the shipped `ActiveRolloutStatus` card, but
 * **in situ** — rendered inline in the real page each process lives on, the way
 * the existing `Rollout/In Situ/In Progress` "Firmware settings page" story
 * already establishes for firmware. Each state reads where an operator actually
 * meets it (page chrome, nav, surrounding content), not as a bare card on a
 * blank canvas.
 *
 * Firmware and reboot render through the same `RolloutInSituSurface`, so the
 * inline treatment can't drift between them; only the page chrome differs
 * (firmware → Firmware settings page; reboot → Fleet page, its bulk-action
 * home). The shipped `FirmwareSettingsPage` story consumes `FirmwareSettingsSurface`
 * too, keeping a single source of truth for the firmware surface.
 *
 * Story-only: no product code lives here — it just wires fixtures and noop
 * handlers into the real components.
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
    <div className="relative min-h-screen bg-surface-base">
      <NavigationMenu items={primaryNavItems} />
      <div className="min-h-screen pl-60">{children}</div>
    </div>
  );
}

/**
 * The inline detail card for a rollout, wired with the full set of lifecycle
 * handlers as noops so each state shows exactly the CTA set
 * `rolloutLifecycleActions` gates for it (Manage/Pause/Resume/Continue/Retry/
 * Cancel) — the per-state stories double as an action-set showcase. Rendered
 * inline (not embedded), so it keeps its own elevated card surface within the
 * page body.
 */
function InlineRolloutCard({ event }: { event: RolloutEvent }): ReactElement {
  return (
    <ActiveRolloutStatus
      event={event}
      onManage={noop}
      onPause={noop}
      onResume={noop}
      onCancelRemaining={noop}
      onContinueFromPilot={noop}
      onRetryFailed={noop}
    />
  );
}

// ---- Firmware settings page surface ----------------------------------------
// The established in-situ home for a firmware rollout: the Firmware settings
// page, with the active-rollout card inline above the firmware files table.
// Reproduces `SettingsLayout` chrome (the shipped `SecondaryNavigation` subnav
// beside a p-10 body) under a MemoryRouter at /settings/firmware so the subnav
// filters to the settings group and marks Firmware current. Shared by both the
// per-state firmware stories and the shipped `FirmwareSettingsPage` story.

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

/**
 * Firmware settings "Firmware files" table, built on the shared `List` (the
 * same component `MinerList` / `ActivityTable` use) rather than bespoke grid
 * markup, so the settings surface reads with the product's real table styling.
 */
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

/**
 * The Firmware settings page with an active firmware rollout shown inline. When
 * `event` is null (the shipped page's default), only the page and its files
 * table render; pass an event to show the rollout card above the table, exactly
 * where an operator watching a firmware update sees it.
 *
 * Pass `releaseChannelsTab` to add the shipped `TabStrip` (Files / Release
 * channels) under the page header and switch the body by tab — the surface the
 * release-channels stories render in situ. Omit it and the page renders exactly
 * as before (no tab row), so the existing lifecycle stories are unchanged.
 */
export function FirmwareSettingsSurface({
  event,
  releaseChannelsTab,
  initialTab = "files",
}: {
  event?: RolloutEvent | null;
  releaseChannelsTab?: ReactNode;
  initialTab?: FirmwareSettingsTab;
}): ReactElement {
  const [activeTab, setActiveTab] = useState<FirmwareSettingsTab>(initialTab);
  const showTabs = releaseChannelsTab !== undefined;
  const showReleaseChannels = showTabs && activeTab === "releaseChannels";

  return (
    <MemoryRouter initialEntries={["/settings/firmware"]}>
      <AppShell>
        <div className="flex h-full grow flex-row">
          <SecondaryNavigation items={secondaryNavItems} />
          <div className="flex min-w-0 grow flex-col gap-6 p-10">
            {showTabs ? (
              <>
                <Header title="Firmware" titleSize="text-heading-300" />
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

            {showReleaseChannels ? (
              releaseChannelsTab
            ) : (
              <>
                {showTabs ? (
                  <div>
                    <Button variant={variants.primary} size={sizes.compact} text="Upload firmware" onClick={noop} />
                  </div>
                ) : null}
                {event ? <InlineRolloutCard event={event} /> : null}
                <div className="text-emphasis-300 text-text-primary">Firmware files</div>
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
// Reboot has no dedicated page — it is a Fleet bulk action (FleetGroupActionsMenu),
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
 * list, the honest home for a fleet-wide reboot (there is no reboot settings
 * page). Same inline treatment as the firmware surface.
 */
export function FleetSurface({ event }: { event: RolloutEvent }): ReactElement {
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
          <InlineRolloutCard event={event} />
          <FleetRacksTable />
        </div>
      </AppShell>
    </MemoryRouter>
  );
}

// ---- Per-process in-situ entry points --------------------------------------

/** A firmware rollout state, shown inline on the Firmware settings page. */
export function FirmwareInSitu({ event }: { event: RolloutEvent }): ReactElement {
  return <FirmwareSettingsSurface event={event} />;
}

/** A reboot rollout state, shown inline on the Fleet page. */
export function RebootInSitu({ event }: { event: RolloutEvent }): ReactElement {
  return <FleetSurface event={event} />;
}

const animationStepPercent = 10;
const animationStepMs = 450;
const completedHoldMs = 2600;

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
    rollups,
  };
}

/**
 * A base rollout ticking from 0% to 100% done on a loop: it advances, holds
 * briefly on the completed state, then restarts — the process-agnostic analog
 * of curtailment's `AnimatedCurtailmentLifecycle`. `startedAt` resets each loop
 * so the card's elapsed timer counts up from zero.
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

/** The animated firmware lifecycle, shown inline on the Firmware settings page. */
export function AnimatedFirmwareInSitu({ base }: { base: RolloutEvent }): ReactElement {
  const event = useAnimatedRolloutEvent(base);
  return <FirmwareSettingsSurface event={event} />;
}

/** The animated reboot lifecycle, shown inline on the Fleet page. */
export function AnimatedRebootInSitu({ base }: { base: RolloutEvent }): ReactElement {
  const event = useAnimatedRolloutEvent(base);
  return <FleetSurface event={event} />;
}
