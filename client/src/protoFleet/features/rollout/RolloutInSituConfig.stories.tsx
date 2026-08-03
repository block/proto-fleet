import { type ReactElement, useMemo, useState } from "react";
import clsx from "clsx";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { FileDropZone } from "@/protoFleet/components/FirmwareUpload";
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
import Button, { sizes, variants } from "@/shared/components/Button";
import { DatePickerField } from "@/shared/components/DatePicker";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import { PopoverProvider } from "@/shared/components/Popover";
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
  name: "Bulk actions (launch a process from the selection bar)",
  render: () => <BulkActionsStory />,
};
