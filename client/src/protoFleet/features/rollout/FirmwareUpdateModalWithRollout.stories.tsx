import { type ReactElement, useState } from "react";
import clsx from "clsx";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { batchedFirmwareConfig } from "@/protoFleet/features/rollout/rollout.fixtures";
import RolloutControls from "@/protoFleet/features/rollout/RolloutControls";
import type { RolloutPlanConfig } from "@/protoFleet/features/rollout/rolloutTypes";
import Button, { variants } from "@/shared/components/Button";
import { DatePickerField } from "@/shared/components/DatePicker";
import Modal from "@/shared/components/Modal";
import Select from "@/shared/components/Select";

/**
 * Option A from the design discussion: show the **existing** "Add firmware
 * payload" modal (`FirmwareUpdateModal`) with the rollout framework's controls
 * composed in — the firmware-file picker it has today, then the Rollout +
 * Date-and-time sections appended below. This is a Storybook composition to
 * validate the integrated surface without editing the shipped component or its
 * `onConfirm` contract; the real wiring is a follow-on PR.
 */
const meta = {
  title: "Proto Fleet/Rollout/Firmware Modal + Rollout",
  parameters: { layout: "fullscreen" },
} satisfies Meta;

export default meta;

type Story = StoryObj;

const noop = () => undefined;

interface FirmwareFile {
  id: string;
  filename: string;
  target: string;
  version: string;
}

const FILES: FirmwareFile[] = [
  { id: "f1", filename: "antminer-s21-5.1.0.tar.gz", target: "Antminer S21", version: "5.1.0" },
  { id: "f2", filename: "antminer-s21-5.0.2.tar.gz", target: "Antminer S21", version: "5.0.2" },
];

function SectionTitle({ children }: { children: string }): ReactElement {
  return <div className="text-emphasis-300 text-text-primary">{children}</div>;
}

function FirmwareModalStory(): ReactElement {
  const [open, setOpen] = useState(true);
  const [fileId, setFileId] = useState<string | null>("f1");
  const [config, setConfig] = useState<RolloutPlanConfig>(batchedFirmwareConfig);
  const [startDate, setStartDate] = useState<Date | undefined>(new Date("2026-08-04T14:00:00"));
  const isScheduled = config.scheduleType === "scheduleForLater";

  return (
    <div className="min-h-screen bg-surface-base p-8">
      <div className="mb-6 flex justify-center">
        <Button variant={variants.primary} text="Bulk actions — Update firmware" onClick={() => setOpen(true)} />
      </div>
      <div className="mx-auto max-w-4xl text-300 text-text-primary-70">
        The existing "Add firmware payload" modal, with the rollout controls composed in.
      </div>
      {open ? (
        <Modal
          title="Add firmware payload"
          onDismiss={() => setOpen(false)}
          divider={false}
          testId="firmware-update-modal-with-rollout"
          buttons={
            fileId
              ? [
                  {
                    text: isScheduled ? "Schedule rollout" : "Start rollout",
                    variant: variants.primary,
                    onClick: noop,
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
                {FILES.map((f) => (
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
                        {f.target} · {f.version}
                      </div>
                    </div>
                  </button>
                ))}
              </div>
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
      ) : null}
    </div>
  );
}

export const AddFirmwarePayloadWithRollout: Story = {
  name: "Add firmware payload + rollout controls",
  render: () => <FirmwareModalStory />,
};
