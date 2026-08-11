import { type ReactElement, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { FileDropZone, FileSelectedStatus } from "@/protoFleet/components/FirmwareUpload";
import CurtailmentStartModal, {
  type CurtailmentFormValues,
  type CurtailmentPlanPreview,
} from "@/protoFleet/features/energy/CurtailmentStartModal";
import { allAtOnceRebootConfig, batchedFirmwareConfig } from "@/protoFleet/features/rollout/rollout.fixtures";
import RolloutConfigModal from "@/protoFleet/features/rollout/RolloutConfigModal";
import RolloutControls from "@/protoFleet/features/rollout/RolloutControls";
import type { RolloutPlanConfig } from "@/protoFleet/features/rollout/rolloutTypes";
import { useRolloutConfigModalState } from "@/protoFleet/features/rollout/useRolloutConfigModalState";
import { Curtail, Reboot, Settings } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import { DatePickerField } from "@/shared/components/DatePicker";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import SegmentedControl from "@/shared/components/SegmentedControl";
import Select from "@/shared/components/Select";

/** Story-only launch controls for bulk rollout config surfaces. */
const meta = {
  title: "Proto Fleet/Rollout/In Situ/Config",
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta;

export default meta;

type Story = StoryObj;

const noop = () => undefined;

// ---- Bulk launch controls ---------------------------------------------------
// Firmware, reboot, and curtailment each open their process-specific config UI.

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

// Several models and versions keep the picker close to real fleet scale.
const firmwarePayloads: FirmwareFile[] = [
  { id: "f1", filename: "antminer-s21-5.1.0.tar.gz", target: "Antminer S21", version: "5.1.0" },
  { id: "f2", filename: "antminer-s21-5.0.2.tar.gz", target: "Antminer S21", version: "5.0.2" },
  { id: "f3", filename: "antminer-s21-5.0.1.tar.gz", target: "Antminer S21", version: "5.0.1" },
  { id: "f4", filename: "antminer-s19xp-4.3.0.tar.gz", target: "Antminer S19 XP", version: "4.3.0" },
  { id: "f5", filename: "antminer-s19xp-4.2.1.tar.gz", target: "Antminer S19 XP", version: "4.2.1" },
  { id: "f6", filename: "whatsminer-m50s-2.8.0.zip", target: "Whatsminer M50S", version: "2.8.0" },
];

const firmwareFileOptions = firmwarePayloads.map((f) => ({
  value: f.id,
  label: f.filename,
  description: `${f.target} (${f.version})`,
}));

// Choosing a payload source clears the hidden source.
type PayloadMethod = "existing" | "upload";

const payloadMethodSegments = [
  { key: "existing", title: "Choose existing" },
  { key: "upload", title: "Upload new" },
];

/**
 * Firmware config combines payload selection with rollout pacing and scheduling.
 */
function FirmwareRolloutModal({ onDismiss }: { onDismiss: () => void }): ReactElement {
  const [method, setMethod] = useState<PayloadMethod>("existing");
  const [fileId, setFileId] = useState<string | null>(null);
  const [uploadedFile, setUploadedFile] = useState<{ name: string; size: number } | null>(null);
  const [firmwareVersion, setFirmwareVersion] = useState("");
  const [config, setConfig] = useState<RolloutPlanConfig>(batchedFirmwareConfig);
  const [startDate, setStartDate] = useState<Date | undefined>(new Date("2026-08-04T14:00:00"));
  const isScheduled = config.scheduleType === "scheduleForLater";

  // Switching methods clears the other side so a hidden selection can't linger.
  const selectMethod = (next: PayloadMethod) => {
    setMethod(next);
    if (next === "existing") {
      setUploadedFile(null);
    } else {
      setFileId(null);
    }
  };

  const hasPayload = method === "existing" ? fileId != null : uploadedFile != null;

  return (
    <Modal
      title="Add firmware payload"
      onDismiss={onDismiss}
      testId="firmware-rollout-modal"
      forceTitleCollapsed
      buttons={
        hasPayload
          ? [
              {
                text: isScheduled ? "Schedule update" : "Start update",
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
        {/* --- existing FirmwareUpdateModal content: payload input --- */}
        <div className="flex flex-col gap-3">
          <div className="text-300 text-text-primary">Firmware payload</div>
          <SegmentedControl
            segments={payloadMethodSegments}
            initialSegmentKey={method}
            onSelect={(key) => selectMethod(key as PayloadMethod)}
          />

          {method === "existing" ? (
            <Select
              id="fw-existing-file"
              label="Firmware file"
              placeholder="Select a firmware file"
              options={firmwareFileOptions}
              value={fileId ?? ""}
              onChange={setFileId}
              forceBelow
            />
          ) : (
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
        </div>

        {/* --- rollout framework: pacing controls --- */}
        <RolloutControls config={config} onChange={setConfig} inScopeCount={222} />

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

// Curtail opens the full-screen config UI. The scope comes from the
// selection, so we lead with a populated plan (reason + pacing) and a preview.
const curtailInitialValues: Partial<CurtailmentFormValues> = {
  curtailmentMode: "fullFleet",
  targetKw: "",
  curtailBatchSize: "20",
  curtailBatchIntervalSec: "30",
  restoreBatchSize: "20",
  restoreIntervalSec: "120",
  reason: "Grid peak - ERCOT 4CP signal",
};

const curtailPreview: CurtailmentPlanPreview = {
  selectedMinerCount: 222,
  targetKw: 540,
  estimatedReductionKw: 540,
  curtailEstimate: "~6 minutes",
  restoreEstimate: "~8 minutes",
  scopeLabel: "222 selected miners",
};

function BulkActionsStory(): ReactElement {
  const [openModal, setOpenModal] = useState<OpenModal>(null);

  // Reboot uses the generic RolloutConfigModal. It defaults to all-at-once,
  // with batching and scheduling available as advanced options.
  const rebootState = useRolloutConfigModalState(allAtOnceRebootConfig);

  return (
    <div className="min-h-screen bg-surface-base">
      <div className="px-10 pt-10">
        <div className="mb-2 text-heading-300 text-text-primary">Fleet</div>
        <div className="mb-6 text-300 text-text-primary-70">222 miners selected</div>
        <div className="flex flex-wrap gap-3">
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            text="Bulk firmware update"
            prefixIcon={<Settings />}
            onClick={() => setOpenModal("firmware")}
            testId="rollout-config-launch-firmware"
          />
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            text="Bulk reboot"
            prefixIcon={<Reboot />}
            onClick={() => setOpenModal("reboot")}
            testId="rollout-config-launch-reboot"
          />
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            text="Bulk curtail"
            prefixIcon={<Curtail />}
            onClick={() => setOpenModal("curtail")}
            testId="rollout-config-launch-curtailment"
          />
        </div>
      </div>
      {openModal === "firmware" ? <FirmwareRolloutModal onDismiss={() => setOpenModal(null)} /> : null}
      {openModal === "reboot" ? (
        <RolloutConfigModal
          title="Reboot miners"
          description="222 miners in scope"
          config={rebootState.config}
          onConfigChange={rebootState.setConfig}
          inScopeCount={222}
          onDismiss={() => setOpenModal(null)}
          onSubmit={() => setOpenModal(null)}
          startDate={rebootState.startDate}
          onStartDateChange={rebootState.setStartDate}
          startTime={rebootState.startTime}
          onStartTimeChange={rebootState.setStartTime}
        />
      ) : null}
      <CurtailmentStartModal
        open={openModal === "curtail"}
        onDismiss={() => setOpenModal(null)}
        onSubmit={() => setOpenModal(null)}
        initialValues={curtailInitialValues}
        preview={curtailPreview}
      />
    </div>
  );
}

export const BulkActions: Story = {
  name: "Bulk actions",
  render: () => <BulkActionsStory />,
};
