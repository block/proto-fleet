import type { ReactElement } from "react";

import RolloutControls from "./RolloutControls";
import type { RolloutPlanConfig } from "./rolloutTypes";
import TargetSelectButton from "@/protoFleet/components/TargetSelectButton";
import { variants } from "@/shared/components/Button";
import { DatePickerField } from "@/shared/components/DatePicker";
import Modal from "@/shared/components/Modal";
import Select from "@/shared/components/Select";

interface RolloutConfigModalProps {
  /** Modal title, e.g. "Update firmware". */
  title: string;
  /** Supporting line under the title, e.g. "Antminer S21 (5.0.2 → 5.1.0)". */
  description?: string;
  config: RolloutPlanConfig;
  onConfigChange: (next: RolloutPlanConfig) => void;
  onDismiss: () => void;
  onSubmit: () => void;
  /** Scope target rows (Apply to). Wired by the host flow; here they're the
   * real `TargetSelectButton`s with their current selection labels. */
  scopeTargets: Array<{ label: string; value: string; onClick: () => void }>;
  startDate?: Date;
  onStartDateChange?: (date: Date) => void;
  startTime?: string;
  onStartTimeChange?: (time: string) => void;
  timezoneLabel?: string;
  timeOptions?: Array<{ value: string; label: string }>;
}

const DEFAULT_TIME_OPTIONS = [
  { value: "14:00", label: "2:00 PM" },
  { value: "18:00", label: "6:00 PM" },
  { value: "22:00", label: "10:00 PM" },
];

function SectionTitle({ children }: { children: string }): ReactElement {
  return <div className="text-emphasis-300 text-text-primary">{children}</div>;
}

/**
 * The bulk-action config surface as a real modal: Apply-to scope + the Rollout
 * controls + Date-and-time schedule, in the shared `Modal`. The primary CTA
 * lives in the top bar (curtailment/schedule pattern), the body scrolls under
 * a sticky header, and dismiss is via the header close / Escape / click-outside.
 *
 * Controlled — owns no plan state; the host flow supplies config + scope and
 * handles submit, mirroring how ScheduleModal / CurtailmentStartModal work.
 */
function RolloutConfigModal({
  title,
  description,
  config,
  onConfigChange,
  onDismiss,
  onSubmit,
  scopeTargets,
  startDate,
  onStartDateChange,
  startTime = DEFAULT_TIME_OPTIONS[0].value,
  onStartTimeChange,
  timezoneLabel = "Times shown in America/Denver (MDT)",
  timeOptions = DEFAULT_TIME_OPTIONS,
}: RolloutConfigModalProps): ReactElement {
  const isScheduled = config.scheduleType === "scheduleForLater";

  return (
    <Modal
      size="large"
      title={title}
      description={description}
      onDismiss={onDismiss}
      testId="rollout-config-modal"
      // CTAs in the top bar; keep the modal open on click so the host controls
      // dismissal after the submit resolves.
      buttons={[
        {
          text: isScheduled ? "Schedule rollout" : "Start rollout",
          variant: variants.primary,
          onClick: onSubmit,
          dismissModalOnClick: false,
        },
      ]}
    >
      <div className="flex flex-col gap-8">
        <section className="grid gap-3">
          <SectionTitle>Apply to</SectionTitle>
          <div className="grid divide-y divide-border-5">
            {scopeTargets.map((target) => (
              <TargetSelectButton
                key={target.label}
                label={target.label}
                value={target.value}
                onClick={target.onClick}
              />
            ))}
          </div>
        </section>

        <RolloutControls config={config} onChange={onConfigChange} />

        <section className="grid gap-3">
          <SectionTitle>Date and time</SectionTitle>
          <Select
            id="rollout-schedule-type"
            label="Type"
            options={[
              { value: "startNow", label: "Start now" },
              { value: "scheduleForLater", label: "Schedule for later" },
            ]}
            value={config.scheduleType}
            onChange={(value) =>
              onConfigChange({ ...config, scheduleType: value as RolloutPlanConfig["scheduleType"] })
            }
            forceBelow
          />
          {isScheduled ? (
            <div className="grid gap-3 tablet:grid-cols-2">
              <DatePickerField
                id="rollout-start-date"
                label="Start date"
                labelPlacement="floating"
                selectedDate={startDate}
                onSelectedDateChange={onStartDateChange}
              />
              <Select
                id="rollout-start-time"
                label="Time"
                options={timeOptions}
                value={startTime}
                onChange={(value) => onStartTimeChange?.(value)}
                forceBelow
              />
            </div>
          ) : null}
          <div className="text-200 text-text-primary-70">{timezoneLabel}</div>
        </section>
      </div>
    </Modal>
  );
}

export default RolloutConfigModal;
