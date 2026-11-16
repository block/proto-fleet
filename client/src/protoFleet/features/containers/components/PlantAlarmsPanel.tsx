import { useState } from "react";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import type { AlarmNotice } from "../hooks/usePlantAlarmAction";
import {
  type AlarmActionName,
  alarmActionSupport,
  alarmEvidenceActive,
  isFreshObservation,
  type PlantAlarmItem,
  severityLabel,
} from "../plantAlarms";
import type { Container } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { Severity } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import Button, { sizes, variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { getRelativeTimeFromEpoch } from "@/shared/utils/datetime";

export interface PlantAlarmsPanelProps {
  container: Container;
  /** Open plant alarms from the errors API. `failed` means the list could not be read. */
  alarms: PlantAlarmItem[];
  alarmsFailed?: boolean;
  /** The container could not be refreshed, so what is shown may be old. */
  refreshFailed?: boolean;
  command: {
    busy: boolean;
    status?: string;
    notice?: AlarmNotice;
    submit: (action: AlarmActionName) => void;
    dismiss: () => void;
  };
}

const dialogs: Record<AlarmActionName, { title: string; summary: string; confirm: string }> = {
  mute: {
    title: "Mute alarm buzzer?",
    summary: "Silences the alarm buzzer. Alarms stay active until their cause clears.",
    confirm: "Mute buzzer",
  },
  reset: {
    title: "Reset alarms?",
    summary: "Asks the controller to clear latched alarms. An alarm whose cause is still present stays active.",
    confirm: "Reset alarms",
  },
};
const unsupported: Record<AlarmActionName, string> = {
  mute: "This controller can't mute alarms from Fleet.",
  reset: "This controller clears alarms on its own once their cause clears.",
};
const severityColor: Record<number, string> = {
  [Severity.CRITICAL]: "text-intent-critical-text",
  [Severity.MAJOR]: "text-intent-critical-text",
  [Severity.MINOR]: "text-intent-warning-text",
};
const toneColor = {
  success: "text-intent-success-text",
  warning: "text-intent-warning-text",
  error: "text-intent-critical-text",
} as const;

const flag = (value: boolean | undefined, on: string, off: string) =>
  value === undefined ? "Not reported" : value ? on : off;
const ago = (date: Date) => getRelativeTimeFromEpoch(date.getTime()).toLowerCase();

function Reading({ label, value, alert }: { label: string; value: string; alert?: boolean }) {
  return (
    <div className="min-w-0">
      <dt className="text-200 text-text-primary-70">{label}</dt>
      <dd className={alert ? "text-emphasis-300 text-intent-critical-text" : "text-emphasis-300 text-text-primary"}>
        {value}
      </dd>
    </div>
  );
}

/**
 * Plant alarm evidence first, then the actions the controller advertises. The
 * parent owns data loading and the command; nothing here talks to the API.
 */
export default function PlantAlarmsPanel({
  container,
  alarms,
  alarmsFailed,
  refreshFailed,
  command,
}: PlantAlarmsPanelProps) {
  const [confirming, setConfirming] = useState<AlarmActionName>();
  const plant = container.plant;
  const observed = plant?.observedAt ? timestampDate(plant.observedAt) : undefined;
  const stale = !plant || plant.stale || !isFreshObservation(plant.observedAt) || !!refreshFailed;
  const evidenceActive = alarmEvidenceActive(plant);
  const canControl = container.canControlEquipment === true;
  const supports = { mute: alarmActionSupport(container, "mute"), reset: alarmActionSupport(container, "reset") };
  const offered = (["mute", "reset"] as const).filter((action) => supports[action] !== "unsupported");
  const showActions = evidenceActive && canControl;
  const disabledReason = stale
    ? "Actions are unavailable while the alarm state is out of date."
    : offered.some((action) => supports[action] === "unknown")
      ? "Actions are unavailable until the controller reports its alarm controls."
      : undefined;
  const notice = command.notice;

  return (
    <section
      aria-label="Container alarms"
      data-testid="plant-alarms"
      className="flex min-w-0 flex-col gap-4 rounded-xl bg-surface-overlay p-6"
    >
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="text-emphasis-300 text-text-primary">Container alarms</h2>
        <p className={stale ? "text-200 text-intent-warning-text" : "text-200 text-text-primary-70"}>
          {observed ? `${stale ? "Out of date. Last observed" : "Observed"} ${ago(observed)}` : "No alarm reading yet"}
        </p>
      </div>
      <dl className="grid grid-cols-1 gap-4 tablet:grid-cols-3">
        <Reading
          label="Alarm"
          value={flag(plant?.alarmActive, "Active", "Clear")}
          alert={plant?.alarmActive === true}
        />
        <Reading
          label="Critical alarm"
          value={flag(plant?.criticalAlarmActive, "Active", "Clear")}
          alert={plant?.criticalAlarmActive === true}
        />
        <Reading
          label="Buzzer"
          value={flag(plant?.buzzerActive, "Sounding", "Silent")}
          alert={plant?.buzzerActive === true}
        />
      </dl>
      {alarmsFailed ? (
        <p className="text-200 text-text-primary-70">Alarm details unavailable</p>
      ) : alarms.length ? (
        <ul aria-label="Active alarms" className="flex flex-col gap-2">
          {alarms.map((alarm) => (
            <li key={alarm.id} className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
              <span className="text-300 text-text-primary">{alarm.name}</span>
              <span className="text-200 text-text-primary-70">
                <span className={severityColor[alarm.severity] ?? "text-text-primary-70"}>
                  {severityLabel(alarm.severity)}
                </span>
                {alarm.confirmedAt
                  ? alarm.retained
                    ? ` · Last confirmed ${ago(alarm.confirmedAt)}`
                    : ` · Last seen ${ago(alarm.confirmedAt)}`
                  : null}
              </span>
            </li>
          ))}
        </ul>
      ) : null}
      {showActions ? (
        <div className="flex flex-col gap-2">
          {offered.length ? (
            <div className="flex flex-wrap gap-2">
              {offered.map((action) => (
                <Button
                  key={action}
                  text={action === "mute" ? "Mute buzzer" : "Reset alarms"}
                  variant={variants.secondary}
                  size={sizes.compact}
                  disabled={command.busy || supports[action] !== "supported" || stale}
                  onClick={() => setConfirming(action)}
                />
              ))}
            </div>
          ) : null}
          {disabledReason ? <p className="text-200 text-text-primary-70">{disabledReason}</p> : null}
          {(["mute", "reset"] as const)
            .filter((action) => supports[action] === "unsupported")
            .map((action) => (
              <p key={action} className="text-200 text-text-primary-70">
                {unsupported[action]}
              </p>
            ))}
        </div>
      ) : evidenceActive && !canControl ? (
        <p className="text-200 text-text-primary-70">
          Muting or resetting alarms needs permission to control equipment.
        </p>
      ) : null}
      {command.status ? (
        <p role="status" className="flex items-center gap-2 text-200 text-text-primary-70">
          <ProgressCircular size={14} indeterminate />
          {command.status}
        </p>
      ) : null}
      {notice ? (
        <div className="flex items-start justify-between gap-3">
          <p role={notice.tone === "error" ? "alert" : "status"} className={`text-200 ${toneColor[notice.tone]}`}>
            {notice.text}
          </p>
          <Button text="Dismiss" variant={variants.textOnly} onClick={command.dismiss} />
        </div>
      ) : null}
      {confirming ? (
        <Dialog
          open
          title={dialogs[confirming].title}
          subtitle={dialogs[confirming].summary}
          testId={`plant-alarm-${confirming}-dialog`}
          manageFocus
          onDismiss={() => setConfirming(undefined)}
          buttons={[
            { text: "Cancel", variant: variants.secondary, onClick: () => setConfirming(undefined) },
            {
              text: dialogs[confirming].confirm,
              variant: variants.primary,
              onClick: () => {
                const action = confirming;
                setConfirming(undefined);
                command.submit(action);
              },
            },
          ]}
        >
          <p className="text-300 text-text-primary-70">
            {confirming === "mute" ? "Muting" : "Resetting"} does not fix or clear a hardware fault.
          </p>
        </Dialog>
      ) : null}
    </section>
  );
}
