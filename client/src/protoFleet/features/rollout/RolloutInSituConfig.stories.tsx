import { type ReactElement, useMemo, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { FileDropZone, FileSelectedStatus } from "@/protoFleet/components/FirmwareUpload";
import CurtailmentStartModal, {
  type CurtailmentFormValues,
  type CurtailmentPlanPreview,
} from "@/protoFleet/features/energy/CurtailmentStartModal";
import ActionBar from "@/protoFleet/features/fleetManagement/components/ActionBar";
import BulkActionsWidget, { BulkActionsPopover } from "@/protoFleet/features/fleetManagement/components/BulkActions";
import type { BulkAction } from "@/protoFleet/features/fleetManagement/components/BulkActions/types";
import {
  type DeviceAction,
  deviceActions,
  type PerformanceAction,
  performanceActions,
} from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/constants";
import { batchedFirmwareConfig, batchedRebootConfig } from "@/protoFleet/features/rollout/rollout.fixtures";
import RolloutConfigModal from "@/protoFleet/features/rollout/RolloutConfigModal";
import RolloutControls from "@/protoFleet/features/rollout/RolloutControls";
import type { RolloutPlanConfig } from "@/protoFleet/features/rollout/rolloutTypes";
import { useRolloutConfigModalState } from "@/protoFleet/features/rollout/useRolloutConfigModalState";
import { withMockedMinerSelectionApis } from "@/protoFleet/stories/MockedMinerSelectionApis";
import { ChevronDown, Curtail, Reboot, Settings } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { variants } from "@/shared/components/Button";
import { DatePickerField } from "@/shared/components/DatePicker";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import { PopoverProvider } from "@/shared/components/Popover";
import SegmentedControl from "@/shared/components/SegmentedControl";
import Select from "@/shared/components/Select";

/**
 * Contextual ("in-situ") **config** surfaces: launching a rollout for a process
 * from the real bulk-actions selection bar, and the config surface each process
 * opens. Paired with the "In Progress" bucket, which shows a rollout once it is
 * live. Backed by the same shipped primitives the product uses.
 */
const meta = {
  title: "Proto Fleet/Rollout/In Situ/Config",
  parameters: {
    layout: "fullscreen",
  },
  decorators: [withMockedMinerSelectionApis],
} satisfies Meta;

export default meta;

type Story = StoryObj;

const noop = () => undefined;

// ---- Bulk actions: the dark selection ActionBar → each action's config surface
// The real ActionBar + BulkActionsWidget (the widget that hosts miner bulk
// actions today), with the rollout processes as its actions. Each opens the
// config surface that process uses:
//   - firmware grafts the rollout controls onto the shipped "Add firmware
//     payload" modal;
//   - reboot — no bespoke product modal — uses the generic RolloutConfigModal
//     (with no "Apply to": the scope is already fixed by the selection);
//   - curtail opens the shipped full-screen CurtailmentStartModal.

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

// A realistic library — several models, multiple versions each — so the picker
// is exercised at the scale a live fleet would have, not a two-row happy path.
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

// The two ways to supply a payload are mutually exclusive: picking one demotes
// (hides) the other, rather than stacking an "Upload" affordance beneath an
// always-present file list.
type PayloadMethod = "existing" | "upload";

const payloadMethodSegments = [
  { key: "existing", title: "Choose existing" },
  { key: "upload", title: "Upload new" },
];

/**
 * Firmware's config surface: the existing "Add firmware payload" modal (its
 * file picker + "Upload new file" path) with the rollout framework's controls
 * composed below — a Storybook composition validating the integrated surface
 * without editing the shipped component or its `onConfirm` contract.
 *
 * Payload input: a scalable `Select` (collapses to one row, scrolls when
 * opened) instead of an unbounded inline list, no file pre-selected, and a
 * `SegmentedControl` (the shipped DeliveryPicker pattern) that switches between
 * choosing an existing file and uploading a new one so the two never compete.
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
      divider={false}
      testId="firmware-rollout-modal"
      buttons={
        hasPayload
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

// Curtail opens the shipped full-screen config UI. The scope comes from the
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
  const selectedMiners = useMemo(() => Array.from({ length: 222 }, (_, i) => `M-${i}`), []);
  const [openModal, setOpenModal] = useState<OpenModal>(null);
  const [currentAction, setCurrentAction] = useState<SupportedAction | null>(null);

  // Reboot has no bespoke product modal, so it uses the generic
  // RolloutConfigModal — one config-state hook kept at the top level.
  const rebootState = useRolloutConfigModalState(batchedRebootConfig);

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
        modal with rollout controls; Reboot opens the generic rollout config modal; Curtail opens the full-screen
        curtailment configuration.
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
