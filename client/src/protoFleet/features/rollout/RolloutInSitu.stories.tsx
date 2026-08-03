import { type ReactElement, type ReactNode, useEffect, useMemo, useState } from "react";
import clsx from "clsx";
import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { ActivityEntrySchema } from "@/protoFleet/api/generated/activity/v1/activity_pb";
import { FileDropZone } from "@/protoFleet/components/FirmwareUpload";
import NavigationMenu from "@/protoFleet/components/NavigationMenu";
import { primaryNavItems } from "@/protoFleet/config/navItems";
import ActivityTable from "@/protoFleet/features/activity/components/ActivityTable";
import CurtailmentHistory from "@/protoFleet/features/energy/CurtailmentHistory";
import { mockCurtailmentHistoryEvents } from "@/protoFleet/features/energy/CurtailmentHistory.fixtures";
import ActionBar from "@/protoFleet/features/fleetManagement/components/ActionBar";
import BulkActionsWidget, { BulkActionsPopover } from "@/protoFleet/features/fleetManagement/components/BulkActions";
import type { BulkAction } from "@/protoFleet/features/fleetManagement/components/BulkActions/types";
import {
  type DeviceAction,
  deviceActions,
  type PerformanceAction,
  performanceActions,
} from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/constants";
import { ActiveRolloutBannerStack } from "@/protoFleet/features/rollout/ActiveRolloutBanner";
import ActiveRolloutStatus from "@/protoFleet/features/rollout/ActiveRolloutStatus";
import {
  batchedCurtailmentConfig,
  batchedFirmwareConfig,
  batchedRebootConfig,
  completedWithFailuresFirmwareEvent,
  inProgressCurtailmentEvent,
  inProgressFirmwareEvent,
  inProgressRebootEvent,
  pilotGateFirmwareEvent,
} from "@/protoFleet/features/rollout/rollout.fixtures";
import RolloutConfigModal from "@/protoFleet/features/rollout/RolloutConfigModal";
import RolloutControls from "@/protoFleet/features/rollout/RolloutControls";
import RolloutPill from "@/protoFleet/features/rollout/RolloutPill";
import type { RolloutPlanConfig } from "@/protoFleet/features/rollout/rolloutTypes";
import { useRolloutConfigModalState } from "@/protoFleet/features/rollout/useRolloutConfigModalState";
import ViewRolloutModal from "@/protoFleet/features/rollout/ViewRolloutModal";
import { useFleetStore } from "@/protoFleet/store";
import { ChevronDown, Curtail, Reboot, Settings } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import { DatePickerField } from "@/shared/components/DatePicker";
import Header from "@/shared/components/Header";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import Modal from "@/shared/components/Modal";
import { PopoverProvider } from "@/shared/components/Popover";
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

// ---- 1. Bulk actions: the dark selection ActionBar → each action's modal ----
// The real ActionBar + BulkActionsWidget (the widget that hosts miner bulk
// actions today), with the rollout processes as its actions. Each opens the
// config surface that process uses: firmware grafts the rollout controls onto
// the shipped "Add firmware payload" modal; reboot + curtail — which have no
// bespoke product modal — use the generic RolloutConfigModal.

type SupportedAction = DeviceAction | PerformanceAction;
type OpenModal = "firmware" | "reboot" | "curtail" | null;

function SectionTitle({ children }: { children: string }): ReactElement {
  return <div className="text-emphasis-300 text-text-primary">{children}</div>;
}

interface FirmwareFile {
  id: string;
  filename: string;
  target: string;
  version: string;
}

const firmwarePayloads: FirmwareFile[] = [
  { id: "f1", filename: "antminer-s21-5.1.0.tar.gz", target: "Antminer S21", version: "5.1.0" },
  { id: "f2", filename: "antminer-s21-5.0.2.tar.gz", target: "Antminer S21", version: "5.0.2" },
];

/**
 * Firmware's config surface: the existing "Add firmware payload" modal (its
 * file picker + "Upload new file" path) with the rollout framework's controls
 * composed below — a Storybook composition validating the integrated surface
 * without editing the shipped component or its `onConfirm` contract.
 */
function FirmwareRolloutModal({ onDismiss }: { onDismiss: () => void }): ReactElement {
  const [fileId, setFileId] = useState<string | null>("f1");
  const [showUploadZone, setShowUploadZone] = useState(false);
  const [firmwareVersion, setFirmwareVersion] = useState("");
  const [config, setConfig] = useState<RolloutPlanConfig>(batchedFirmwareConfig);
  const [startDate, setStartDate] = useState<Date | undefined>(new Date("2026-08-04T14:00:00"));
  const isScheduled = config.scheduleType === "scheduleForLater";

  return (
    <Modal
      title="Add firmware payload"
      onDismiss={onDismiss}
      divider={false}
      testId="firmware-rollout-modal"
      buttons={
        fileId
          ? [
              {
                text: isScheduled ? "Schedule rollout" : "Start rollout",
                variant: variants.primary,
                onClick: onDismiss,
                dismissModalOnClick: false,
              },
            ]
          : undefined
      }
    >
      <div className="mt-2 text-300 text-text-primary-70">
        Select a firmware payload to update your miners, then choose how it rolls out.
      </div>
      <div className="mt-6 flex flex-col gap-8">
        {/* --- existing FirmwareUpdateModal content: file picker --- */}
        <div className="flex flex-col gap-2">
          <div className="text-300 text-text-primary">Select an existing firmware file</div>
          <div className="flex flex-col gap-1" role="radiogroup" aria-label="Existing firmware files">
            {firmwarePayloads.map((f) => (
              <button
                key={f.id}
                type="button"
                role="radio"
                aria-checked={fileId === f.id}
                onClick={() => setFileId(f.id)}
                className={clsx(
                  "flex cursor-pointer items-center gap-3 rounded-lg border p-3 text-left transition-colors",
                  fileId === f.id
                    ? "border-border-20 bg-surface-elevated-base"
                    : "border-border-5 hover:border-border-20",
                )}
              >
                <div className="flex h-4 w-4 shrink-0 items-center justify-center rounded-full border-2 border-border-20">
                  {fileId === f.id ? <div className="h-2 w-2 rounded-full bg-core-primary-fill" /> : null}
                </div>
                <div className="flex min-w-0 flex-col">
                  <div className="truncate text-300 text-text-primary">{f.filename}</div>
                  <div className="text-200 text-text-primary-70">
                    {f.target} ({f.version})
                  </div>
                </div>
              </button>
            ))}
          </div>

          {/* --- existing FirmwareUpdateModal content: "Upload new file" path --- */}
          <div className="flex items-center gap-3 py-2">
            <div className="h-px flex-1 bg-border-5" />
            <Button
              variant={variants.secondary}
              size={sizes.compact}
              text={showUploadZone ? "Hide upload" : "Upload new file"}
              onClick={() => setShowUploadZone((prev) => !prev)}
            />
            <div className="h-px flex-1 bg-border-5" />
          </div>

          {showUploadZone ? (
            <div className="flex flex-col gap-4">
              <div className="grid gap-4 tablet:grid-cols-2">
                <Input id="fw-upload-manufacturer" label="Product" initValue="Antminer" disabled required />
                <Input id="fw-upload-model" label="Model" initValue="S21" disabled required />
              </div>
              <Input
                id="fw-upload-version"
                label="Firmware version"
                initValue={firmwareVersion}
                onChange={setFirmwareVersion}
                required
              />
              <FileDropZone extensions={[".tar.gz", ".zip"]} onFileSelect={() => undefined} />
            </div>
          ) : null}
        </div>

        {/* --- rollout framework: pacing controls --- */}
        <RolloutControls config={config} onChange={setConfig} />

        {/* --- rollout framework: date and time --- */}
        <section className="grid gap-3">
          <SectionTitle>Date and time</SectionTitle>
          <Select
            id="fw-rollout-schedule-type"
            label="Type"
            options={[
              { value: "startNow", label: "Start now" },
              { value: "scheduleForLater", label: "Schedule for later" },
            ]}
            value={config.scheduleType}
            onChange={(value) => setConfig({ ...config, scheduleType: value as RolloutPlanConfig["scheduleType"] })}
            forceBelow
          />
          {isScheduled ? (
            <div className="grid gap-3 tablet:grid-cols-2">
              <DatePickerField
                id="fw-rollout-start-date"
                label="Start date"
                labelPlacement="floating"
                selectedDate={startDate}
                onSelectedDateChange={setStartDate}
              />
              <Select
                id="fw-rollout-start-time"
                label="Time"
                options={[
                  { value: "14:00", label: "2:00 PM" },
                  { value: "18:00", label: "6:00 PM" },
                ]}
                value="14:00"
                onChange={noop}
                forceBelow
              />
            </div>
          ) : null}
          <div className="text-200 text-text-primary-70">Times shown in America/Denver (MDT)</div>
        </section>
      </div>
    </Modal>
  );
}

const bulkScopeTargets = [
  { label: "Sites", value: "Select", onClick: noop },
  { label: "Buildings", value: "1 building", onClick: noop },
  { label: "Racks", value: "Select", onClick: noop },
  { label: "Groups", value: "Select", onClick: noop },
  { label: "Miners", value: "222 miners", onClick: noop },
];

function BulkActionsStory(): ReactElement {
  const selectedMiners = useMemo(() => Array.from({ length: 222 }, (_, i) => `M-${i}`), []);
  const [openModal, setOpenModal] = useState<OpenModal>(null);
  const [currentAction, setCurrentAction] = useState<SupportedAction | null>(null);

  // reboot / curtail have no bespoke product modal, so they share the generic
  // RolloutConfigModal — one config-state hook each, kept at the top level.
  const rebootState = useRolloutConfigModalState(batchedRebootConfig);
  const curtailState = useRolloutConfigModalState(batchedCurtailmentConfig);

  const open = (action: SupportedAction, modal: Exclude<OpenModal, null>) => {
    setCurrentAction(action);
    setOpenModal(modal);
  };

  const actions = useMemo<BulkAction<SupportedAction>[]>(
    () => [
      {
        action: deviceActions.firmwareUpdate,
        title: "Update firmware",
        icon: <Settings />,
        actionHandler: () => open(deviceActions.firmwareUpdate, "firmware"),
        requiresConfirmation: false,
      },
      {
        action: deviceActions.reboot,
        title: "Reboot",
        icon: <Reboot />,
        actionHandler: () => open(deviceActions.reboot, "reboot"),
        requiresConfirmation: false,
      },
      {
        action: performanceActions.curtail,
        title: "Curtail",
        icon: <Curtail />,
        actionHandler: () => open(performanceActions.curtail, "curtail"),
        requiresConfirmation: false,
      },
    ],
    [],
  );

  return (
    <div className="min-h-screen bg-surface-base">
      <div className="px-10 pt-10 pb-40 text-300 text-text-primary-70">
        <div className="mb-2 text-heading-300 text-text-primary">Fleet</div>
        Select miners to reveal the bulk-actions bar, then launch a process. Firmware opens the "Add firmware payload"
        modal with rollout controls; Reboot and Curtail open the generic rollout config modal.
      </div>
      <ActionBar
        className="fixed right-0 bottom-4 left-0 z-[45]"
        selectedItems={selectedMiners}
        selectionMode="subset"
        renderActions={() => (
          <PopoverProvider>
            <BulkActionsWidget<SupportedAction>
              buttonTitle="Bulk actions"
              buttonIconSuffix={<ChevronDown width={iconSizes.xSmall} />}
              actions={actions}
              onCancel={noop}
              currentAction={currentAction}
              renderPopover={(beforeEach, closePopover) => (
                <BulkActionsPopover<SupportedAction>
                  actions={actions}
                  beforeEach={beforeEach}
                  testId="rollout-bulk-popover"
                  closePopover={closePopover}
                />
              )}
              testId="rollout-bulk"
            />
          </PopoverProvider>
        )}
      />
      {openModal === "firmware" ? <FirmwareRolloutModal onDismiss={() => setOpenModal(null)} /> : null}
      {openModal === "reboot" ? (
        <RolloutConfigModal
          title="Reboot miners"
          description="222 miners in scope"
          config={rebootState.config}
          onConfigChange={rebootState.setConfig}
          onDismiss={() => setOpenModal(null)}
          onSubmit={() => setOpenModal(null)}
          scopeTargets={bulkScopeTargets}
          startDate={rebootState.startDate}
          onStartDateChange={rebootState.setStartDate}
          startTime={rebootState.startTime}
          onStartTimeChange={rebootState.setStartTime}
        />
      ) : null}
      {openModal === "curtail" ? (
        <RolloutConfigModal
          title="Curtail miners"
          description="222 miners in scope"
          config={curtailState.config}
          onConfigChange={curtailState.setConfig}
          onDismiss={() => setOpenModal(null)}
          onSubmit={() => setOpenModal(null)}
          scopeTargets={bulkScopeTargets}
          startDate={curtailState.startDate}
          onStartDateChange={curtailState.setStartDate}
          startTime={curtailState.startTime}
          onStartTimeChange={curtailState.setStartTime}
        />
      ) : null}
    </div>
  );
}

export const BulkActions: Story = {
  name: "Bulk actions (launch a process from the selection bar)",
  render: () => <BulkActionsStory />,
};

// ---- 2. Header bar: the persistent rollout pill (opens the modal) -----------

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

// ---- 3. Activity "Active now": stacked banners (each opens the modal) --------

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

// ---- 4. Firmware settings page: active rollout in its detail home ----------
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

// ---- 5. Energy UI: rollout card the way curtailment renders it -------------
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

// ---- 6. Activity page: rollout banners in the feed ------------------------
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

// ---- 7. Activity detail: a rollout opened into its detail surface ----------
// Where a rollout lives once you drill in from Activity — the full
// ActiveRolloutStatus card in the Activity page's detail home. Migrated from
// the old standalone "Active Rollout Status" bucket: the states that need a
// lifecycle decision (paused at the pilot gate) and the retained record of a
// finished-with-failures rollout, each shown where the product surfaces them.

function ActivityRolloutDetailStory(): ReactElement {
  return (
    <AppShell>
      <div className="flex flex-col gap-10 px-10 pt-10 pb-16">
        <div className="flex items-center gap-2 text-200 text-text-primary-70">
          <span>Activity</span>
          <span>/</span>
          <span className="text-text-primary">Rollout detail</span>
        </div>
        <ActiveRolloutStatus
          event={pilotGateFirmwareEvent}
          onContinueFromPilot={noop}
          onRetryFailed={noop}
          onCancelRemaining={noop}
        />
        <ActiveRolloutStatus event={completedWithFailuresFirmwareEvent} onRetryFailed={noop} />
      </div>
    </AppShell>
  );
}

export const ActivityRolloutDetail: Story = {
  name: "Activity — rollout detail (pilot gate, completed with failures)",
  render: () => <ActivityRolloutDetailStory />,
};
