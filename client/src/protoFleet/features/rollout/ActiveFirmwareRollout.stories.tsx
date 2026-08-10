import { type ReactElement, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { FileDropZone, FileSelectedStatus } from "@/protoFleet/components/FirmwareUpload";
import TargetSelectButton, { targetSelectPlaceholderLabel } from "@/protoFleet/components/TargetSelectButton";
import { ActiveRolloutBanner } from "@/protoFleet/features/rollout/ActiveRolloutBanner";
import ActiveRolloutStatus from "@/protoFleet/features/rollout/ActiveRolloutStatus";
import {
  AnimatedFirmwareInSitu,
  FirmwareInSitu,
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
import type { RolloutPlanConfig } from "@/protoFleet/features/rollout/rolloutTypes";
import { sizes, variants } from "@/shared/components/Button";
import { DatePickerField } from "@/shared/components/DatePicker";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import SegmentedControl from "@/shared/components/SegmentedControl";
import Select from "@/shared/components/Select";

/**
 * The active **firmware update** across its lifecycle states, plus a
 * live-animated lifecycle — all rendered **in situ**, on the established
 * firmware in-situ surface: the Firmware settings page (nav sidebar + settings
 * subnav + "Firmware" header + Upload CTA + firmware files table), with the
 * shipped `ActiveRolloutStatus` card inline above the files table, exactly the
 * way the `In Situ/In Progress` "Firmware settings page" story already
 * establishes. Each state reads where an operator actually meets it, not as a
 * bare card on a blank canvas. `FirmwareInSitu` is the single shared surface so
 * this can't drift from that story.
 */
const meta = {
  title: "Proto Fleet/Rollout/In Situ/Firmware Lifecycle",
  component: ActiveRolloutStatus,
  parameters: {
    layout: "fullscreen",
    // The in-situ surface provides its own MemoryRouter (at /settings/firmware),
    // so opt out of the global StoryRouter — react-router throws on nested routers.
    withRouter: false,
  },
} satisfies Meta<typeof ActiveRolloutStatus>;

export default meta;

type Story = StoryObj<typeof ActiveRolloutStatus>;

const noop = (): void => undefined;

function SectionTitle({ children }: { children: string }): ReactElement {
  return <div className="text-emphasis-300 text-text-primary">{children}</div>;
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
  const [startDate, setStartDate] = useState<Date | undefined>(new Date("2026-08-04T14:00:00"));
  const [startTime, setStartTime] = useState("14:00");
  const inScopeCount = scheduledFirmwareEvent.totalTargets - scheduledFirmwareEvent.excludedTargets;
  const isScheduled = config.scheduleType === "scheduleForLater";

  const selectMethod = (next: PayloadMethod): void => {
    setMethod(next);
    if (next === "existing") {
      setUploadedFile(null);
    } else {
      setFileId("");
    }
  };

  return (
    <Modal
      title="Manage scheduled firmware rollout"
      description={`${scheduledFirmwareEvent.title}, ${inScopeCount.toLocaleString()} miners in scope`}
      onDismiss={onDismiss}
      divider={false}
      testId="firmware-rollout-modal"
      buttons={[
        {
          text: "Cancel scheduled rollout",
          variant: variants.secondaryDanger,
          onClick: onCancelScheduled,
          dismissModalOnClick: false,
        },
        {
          text: "Save changes",
          variant: variants.primary,
          onClick: onSave,
          dismissModalOnClick: false,
        },
      ]}
    >
      <div className="mt-6 flex flex-col gap-8">
        <section className="grid gap-3">
          <SectionTitle>Firmware payload</SectionTitle>
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
        </section>

        <section className="grid gap-3">
          <SectionTitle>Applies to</SectionTitle>
          <div className="grid divide-y divide-border-5">
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
        </section>

        <RolloutControls config={config} onChange={setConfig} inScopeCount={inScopeCount} />

        <section className="grid gap-3">
          <SectionTitle>Date and time</SectionTitle>
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
                options={[
                  { value: "14:00", label: "2:00 PM" },
                  { value: "18:00", label: "6:00 PM" },
                  { value: "22:00", label: "10:00 PM" },
                ]}
                value={startTime}
                onChange={setStartTime}
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

function ScheduledFirmwareStory(): ReactElement {
  const [configOpen, setConfigOpen] = useState(false);
  const [showScheduledBanner, setShowScheduledBanner] = useState(true);

  return (
    <>
      <FirmwareSettingsSurface
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

export const AnimatedFirmwareLifecycle: Story = {
  name: "Animated firmware lifecycle",
  render: function renderAnimatedFirmwareLifecycle(): ReactElement {
    return <AnimatedFirmwareInSitu base={inProgressFirmwareEvent} />;
  },
};
