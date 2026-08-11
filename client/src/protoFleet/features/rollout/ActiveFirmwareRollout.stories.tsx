import { type ReactElement, type ReactNode, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { FileDropZone, FileSelectedStatus } from "@/protoFleet/components/FirmwareUpload";
import FullScreenTwoPaneModal, {
  type FullScreenTwoPaneModalProps,
} from "@/protoFleet/components/FullScreenTwoPaneModal";
import TargetSelectButton, { targetSelectPlaceholderLabel } from "@/protoFleet/components/TargetSelectButton";
import { ActiveRolloutBanner } from "@/protoFleet/features/rollout/ActiveRolloutBanner";
import ActiveRolloutStatus from "@/protoFleet/features/rollout/ActiveRolloutStatus";
import {
  AnimatedFirmwareInSitu,
  FirmwareInSitu,
  FirmwareReleaseChannelsTab,
  FirmwareSettingsSurface,
} from "@/protoFleet/features/rollout/activeRolloutStoryHelpers";
import {
  completedFirmwareEvent,
  completedWithFailuresFirmwareEvent,
  inProgressFirmwareEvent,
  pausedFirmwareEvent,
  pilotGateFirmwareEvent,
  scheduledFirmwareEvent,
} from "@/protoFleet/features/rollout/rollout.fixtures";
import RolloutControls from "@/protoFleet/features/rollout/RolloutControls";
import { rolloutPlanReadout } from "@/protoFleet/features/rollout/rolloutDisplayUtils";
import type { RolloutEvent, RolloutPlanConfig } from "@/protoFleet/features/rollout/rolloutTypes";
import { sizes, variants } from "@/shared/components/Button";
import { DatePickerField } from "@/shared/components/DatePicker";
import Input from "@/shared/components/Input";
import SegmentedControl from "@/shared/components/SegmentedControl";
import Select from "@/shared/components/Select";

/**
 * Firmware rollout lifecycle states rendered on the Firmware settings page.
 * These stories show the rollout card in its expected page context.
 */
const meta = {
  title: "Proto Fleet/Rollout/In Situ/Firmware Lifecycle",
  component: ActiveRolloutStatus,
  parameters: {
    layout: "fullscreen",
    // The page shell provides its own MemoryRouter at /settings/firmware.
    withRouter: false,
  },
} satisfies Meta<typeof ActiveRolloutStatus>;

export default meta;

type Story = StoryObj<typeof ActiveRolloutStatus>;

const noop = (): void => undefined;

function SectionTitle({ children }: { children: string }): ReactElement {
  return <div className="text-emphasis-300 text-text-primary">{children}</div>;
}

function Section({ title, children }: { title: string; children: ReactNode }): ReactElement {
  return (
    <section className="grid gap-3">
      <SectionTitle>{title}</SectionTitle>
      {children}
    </section>
  );
}

const scheduledFirmwareConfig: RolloutPlanConfig = {
  processType: scheduledFirmwareEvent.processType,
  strategy: scheduledFirmwareEvent.strategy,
  order: scheduledFirmwareEvent.order,
  maxConcurrentOffline: 50,
  batchSize: scheduledFirmwareEvent.batchSize,
  batchIntervalSec: scheduledFirmwareEvent.batchIntervalSec,
  scheduleType: "scheduleForLater",
  scheduledStartAt: scheduledFirmwareEvent.scheduledStartAt,
};

type PayloadMethod = "existing" | "upload";

const payloadMethodSegments = [
  { key: "existing", title: "Choose existing" },
  { key: "upload", title: "Upload new" },
];

const scheduledTimeOptions = [
  { value: "14:00", label: "2:00 PM" },
  { value: "18:00", label: "6:00 PM" },
  { value: "22:00", label: "10:00 PM" },
];

const firmwareFileOptions = [
  {
    value: "f1",
    label: "antminer-s21-5.1.0.tar.gz",
    description: "Antminer S21 (5.1.0)",
  },
  {
    value: "f2",
    label: "antminer-s21-5.0.2.tar.gz",
    description: "Antminer S21 (5.0.2)",
  },
  {
    value: "f3",
    label: "whatsminer-m60-3.4.1.tar.gz",
    description: "Whatsminer M60 (3.4.1)",
  },
];

const scheduledFirmwareScopeTargets = [
  { label: "Sites", value: targetSelectPlaceholderLabel },
  { label: "Buildings", value: scheduledFirmwareEvent.scopeLabel },
  { label: "Racks", value: targetSelectPlaceholderLabel },
  { label: "Groups", value: targetSelectPlaceholderLabel },
  { label: "Miners", value: targetSelectPlaceholderLabel },
];

const animatedAllAtOnceFirmwareEvent: RolloutEvent = {
  processType: "firmware",
  state: "inProgress",
  title: "Firmware update to 5.1.0",
  scopeLabel: "Building B",
  strategy: "allAtOnce",
  order: "leastEfficientFirst",
  totalTargets: 240,
  excludedTargets: 18,
  startedAt: new Date(Date.now() - 60_000).toISOString(),
  estimatedSecondsRemaining: 90,
  performance: inProgressFirmwareEvent.performance,
  rollups: [
    { phase: "inProgress", count: 222 },
    { phase: "excluded", count: 18 },
  ],
};

const animatedBatchesReviewFirmwareEvent: RolloutEvent = {
  ...inProgressFirmwareEvent,
  currentBatch: 1,
  reviewAfterEachBatch: true,
  rollups: [
    { phase: "inProgress", count: 20 },
    { phase: "queued", count: 202 },
    { phase: "excluded", count: 18 },
  ],
};

const animatedPilotReviewFirmwareEvent: RolloutEvent = {
  ...inProgressFirmwareEvent,
  strategy: "pilotThenContinue",
  pilotSize: 10,
  batchSize: 25,
  batchIntervalSec: 90,
  currentBatch: 1,
  totalBatches: 10,
  reviewAfterEachBatch: true,
  estimatedSecondsRemaining: 810,
  rollups: [
    { phase: "inProgress", count: 10 },
    { phase: "queued", count: 212 },
    { phase: "excluded", count: 18 },
  ],
};

function formatScheduledStart(config: RolloutPlanConfig, startDate: Date | undefined, startTime: string): string {
  if (config.scheduleType === "startNow") {
    return "Starts after save";
  }

  if (!startDate) {
    return "Not scheduled";
  }

  const date = startDate.toLocaleDateString("en-US", {
    weekday: "long",
    month: "short",
    day: "numeric",
  });
  const time = scheduledTimeOptions.find((option) => option.value === startTime)?.label ?? startTime;
  return `${date} at ${time}`;
}

function PreviewRow({ label, value }: { label: string; value: string }): ReactElement {
  return (
    <div className="grid gap-1 border-b border-border-5 py-3 last:border-b-0">
      <div className="text-200 text-text-primary-50">{label}</div>
      <div className="text-300 text-text-primary">{value}</div>
    </div>
  );
}

function ScheduledFirmwarePreview({
  config,
  inScopeCount,
  payloadSummary,
  startDate,
  startTime,
}: {
  config: RolloutPlanConfig;
  inScopeCount: number;
  payloadSummary: string;
  startDate: Date | undefined;
  startTime: string;
}): ReactElement {
  const planReadout = rolloutPlanReadout({ inScopeCount, config }) ?? "Plan incomplete";

  return (
    <div className="flex min-h-[360px] flex-1 flex-col gap-10 rounded-[24px] bg-surface-overlay px-8 py-12 laptop:min-h-0 laptop:px-16 laptop:py-10">
      <div className="text-heading-100 text-text-primary">
        {scheduledFirmwareEvent.title} is scheduled for {inScopeCount.toLocaleString()} miners in{" "}
        {scheduledFirmwareEvent.scopeLabel}.
      </div>

      <div className="grid">
        <PreviewRow label="Firmware payload" value={payloadSummary} />
        <PreviewRow label="Starts" value={formatScheduledStart(config, startDate, startTime)} />
        <PreviewRow label="Pacing" value={planReadout} />
        <PreviewRow label="Offline limit" value={`${config.maxConcurrentOffline.toLocaleString()} miners`} />
        <PreviewRow label="Excluded" value={`${scheduledFirmwareEvent.excludedTargets.toLocaleString()} miners`} />
      </div>
    </div>
  );
}

function ManageScheduledFirmwareRolloutModal({
  onCancelScheduled,
  onDismiss,
  onSave,
}: {
  onCancelScheduled: () => void;
  onDismiss: () => void;
  onSave: () => void;
}): ReactElement {
  const [method, setMethod] = useState<PayloadMethod>("existing");
  const [fileId, setFileId] = useState("f1");
  const [uploadedFile, setUploadedFile] = useState<{ name: string; size: number } | null>(null);
  const [firmwareVersion, setFirmwareVersion] = useState("5.1.0");
  const [config, setConfig] = useState<RolloutPlanConfig>(scheduledFirmwareConfig);
  const [startDate, setStartDate] = useState<Date | undefined>(new Date("2026-08-14T14:00:00"));
  const [startTime, setStartTime] = useState("14:00");
  const inScopeCount = scheduledFirmwareEvent.totalTargets - scheduledFirmwareEvent.excludedTargets;
  const isScheduled = config.scheduleType === "scheduleForLater";
  const selectedFile = firmwareFileOptions.find((option) => option.value === fileId);
  const payloadSummary =
    method === "existing"
      ? selectedFile
        ? `${selectedFile.label}, ${selectedFile.description}`
        : "No firmware file selected"
      : uploadedFile
        ? `${uploadedFile.name}, ${firmwareVersion}`
        : `New firmware ${firmwareVersion}`;
  const previewPane = (
    <ScheduledFirmwarePreview
      config={config}
      inScopeCount={inScopeCount}
      payloadSummary={payloadSummary}
      startDate={startDate}
      startTime={startTime}
    />
  );
  const buttons: NonNullable<FullScreenTwoPaneModalProps["buttons"]> = [
    {
      text: "Cancel scheduled update",
      variant: variants.secondaryDanger,
      onClick: onCancelScheduled,
    },
    {
      text: "Save changes",
      variant: variants.primary,
      onClick: onSave,
    },
  ];

  const selectMethod = (next: PayloadMethod): void => {
    setMethod(next);
    if (next === "existing") {
      setUploadedFile(null);
    } else {
      setFileId("");
    }
  };

  return (
    <FullScreenTwoPaneModal
      open
      title="Manage scheduled update"
      closeAriaLabel="Close scheduled update editor"
      onDismiss={onDismiss}
      buttons={buttons}
      abovePanes={<div className="px-6 pb-6 laptop:hidden">{previewPane}</div>}
      primaryPane={
        <section className="flex flex-col gap-12 pr-6 pb-6 laptop:pr-10 laptop:pb-10">
          <Section title="Firmware payload">
            <SegmentedControl
              segments={payloadMethodSegments}
              initialSegmentKey={method}
              onSelect={(key) => selectMethod(key as PayloadMethod)}
            />
            {method === "existing" ? (
              <Select
                id="scheduled-fw-existing-file"
                label="Firmware file"
                placeholder="Select a firmware file"
                options={firmwareFileOptions}
                value={fileId}
                onChange={setFileId}
                forceBelow
              />
            ) : (
              <div className="flex flex-col gap-4">
                <div className="grid gap-4 tablet:grid-cols-2">
                  <Input id="scheduled-fw-upload-manufacturer" label="Product" initValue="Antminer" disabled required />
                  <Input id="scheduled-fw-upload-model" label="Model" initValue="S21" disabled required />
                </div>
                <Input
                  id="scheduled-fw-upload-version"
                  label="Firmware version"
                  initValue={firmwareVersion}
                  onChange={setFirmwareVersion}
                  required
                />
                {uploadedFile ? (
                  <FileSelectedStatus
                    fileName={uploadedFile.name}
                    fileSize={uploadedFile.size}
                    onRemove={() => setUploadedFile(null)}
                  />
                ) : (
                  <FileDropZone
                    extensions={[".tar.gz", ".zip"]}
                    onFileSelect={(file) => setUploadedFile({ name: file.name, size: file.size })}
                  />
                )}
              </div>
            )}
          </Section>

          <Section title="Apply to">
            <div className="grid">
              {scheduledFirmwareScopeTargets.map((target) => (
                <TargetSelectButton
                  key={target.label}
                  label={target.label}
                  value={target.value}
                  size={sizes.compact}
                  onClick={noop}
                />
              ))}
            </div>
          </Section>

          <RolloutControls config={config} onChange={setConfig} inScopeCount={inScopeCount} />

          <Section title="Date and time">
            <Select
              id="scheduled-fw-rollout-schedule-type"
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
                  id="scheduled-fw-rollout-start-date"
                  label="Start date"
                  labelPlacement="floating"
                  selectedDate={startDate}
                  onSelectedDateChange={setStartDate}
                />
                <Select
                  id="scheduled-fw-rollout-start-time"
                  label="Time"
                  options={scheduledTimeOptions}
                  value={startTime}
                  onChange={setStartTime}
                  forceBelow
                />
              </div>
            ) : null}
            <div className="text-200 text-text-primary-70">Times shown in America/Denver (MDT)</div>
          </Section>
        </section>
      }
      secondaryPane={previewPane}
      secondaryPaneClassName="!hidden !bg-transparent laptop:!flex laptop:!pl-0 laptop:!rounded-[24px]"
    />
  );
}

function ScheduledFirmwareStory(): ReactElement {
  const [configOpen, setConfigOpen] = useState(false);
  const [showScheduledBanner, setShowScheduledBanner] = useState(true);

  return (
    <>
      <FirmwareSettingsSurface
        releaseChannelsTab={<FirmwareReleaseChannelsTab />}
        rolloutBanner={
          showScheduledBanner ? (
            <ActiveRolloutBanner event={scheduledFirmwareEvent} onManage={() => setConfigOpen(true)} />
          ) : null
        }
      />
      {configOpen ? (
        <ManageScheduledFirmwareRolloutModal
          onDismiss={() => setConfigOpen(false)}
          onSave={() => setConfigOpen(false)}
          onCancelScheduled={() => {
            setConfigOpen(false);
            setShowScheduledBanner(false);
          }}
        />
      ) : null}
    </>
  );
}

export const Scheduled: Story = {
  render: () => <ScheduledFirmwareStory />,
};

export const InProgress: Story = {
  name: "In progress",
  render: () => <FirmwareInSitu event={inProgressFirmwareEvent} />,
};

export const Paused: Story = {
  render: () => <FirmwareInSitu event={pausedFirmwareEvent} />,
};

export const PilotReview: Story = {
  name: "Pilot review",
  render: () => <FirmwareInSitu event={pilotGateFirmwareEvent} />,
};

export const Completed: Story = {
  render: () => <FirmwareInSitu event={completedFirmwareEvent} />,
};

export const CompletedWithFailures: Story = {
  name: "Completed with failures",
  render: () => <FirmwareInSitu event={completedWithFailuresFirmwareEvent} />,
};

export const AnimatedAllAtOnce: Story = {
  name: "Animated all at once",
  render: function renderAnimatedAllAtOnce(): ReactElement {
    return <AnimatedFirmwareInSitu base={animatedAllAtOnceFirmwareEvent} defaultDetailsOpen />;
  },
};

export const AnimatedBatchesWithReview: Story = {
  name: "Animated batches with review",
  render: function renderAnimatedBatchesWithReview(): ReactElement {
    return <AnimatedFirmwareInSitu base={animatedBatchesReviewFirmwareEvent} defaultDetailsOpen />;
  },
};

export const AnimatedPilotWithReview: Story = {
  name: "Animated pilot with review",
  render: function renderAnimatedPilotWithReview(): ReactElement {
    return <AnimatedFirmwareInSitu base={animatedPilotReviewFirmwareEvent} defaultDetailsOpen />;
  },
};
