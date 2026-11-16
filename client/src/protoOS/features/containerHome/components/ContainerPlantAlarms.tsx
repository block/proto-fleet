import { useEffect, useState } from "react";
import {
  acknowledgedOutcome,
  OUTCOME_TONES,
  outcomeMessage,
  type PlantAlarmOutcome,
  setPlantAlarmOutcome,
  useNow,
  usePlantAlarmReadback,
} from "../hooks/usePlantAlarmOutcome";
import {
  ALARM_READBACK_WINDOW_MS,
  type AlarmAction,
  alarmActionAvailability,
  hasAlarmEvidence,
  PLANT_OBSERVATION_STALE_MS,
} from "../utils/plantAlarms";
import { ALARM_MUTE_OUTCOME_UNKNOWN, ALARM_RESET_OUTCOME_UNKNOWN } from "@/protoOS/api/adapters/alarmMessages";
import type { AdapterRequestOptions, ContainerSettings } from "@/protoOS/api/adapters/types";
import { useTelemetry } from "@/protoOS/api/hooks/useTelemetry";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import {
  formatAge,
  observationState,
} from "@/protoOS/features/diagnostic/components/DiagnosticView/controllerObservation";
import type { PlantAlarm } from "@/protoOS/store/contract";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import useMinerStore from "@/protoOS/store/useMinerStore";
import Button, { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import StatusCircle, { type StatusCircleProps } from "@/shared/components/StatusCircle";
import { withDeadline } from "@/shared/utils/withDeadline";

const WRITE_TIMEOUT_MS = 30_000;
const READBACK_POLL_MS = 3_000;
const READBACK_LEVELS = ["miner" as const];

const COPY: Record<AlarmAction, { button: string; title: string; subtitle: string; unknown: string; sent: string }> = {
  mute: {
    button: "Mute buzzer",
    title: "Mute alarm buzzer?",
    subtitle:
      "Silences the alarm buzzer. Alarms stay active until their cause clears. Muting does not fix or clear a hardware fault.",
    unknown: ALARM_MUTE_OUTCOME_UNKNOWN,
    sent: "Mute",
  },
  reset: {
    button: "Reset alarms",
    title: "Reset alarms?",
    subtitle:
      "Asks the controller to clear latched alarms. An alarm whose cause is still present stays active. Resetting does not fix or clear a hardware fault.",
    unknown: ALARM_RESET_OUTCOME_UNKNOWN,
    sent: "Reset",
  },
};

const SEVERITY: Record<PlantAlarm["severity"], { label: string; status: StatusCircleProps["status"] }> = {
  critical: { label: "Critical", status: "error" },
  error: { label: "Error", status: "error" },
  warning: { label: "Warning", status: "warning" },
  info: { label: "Info", status: "pending" },
  unknown: { label: "Severity unknown", status: "inactive" },
};

// Plain proto3-style flags: absent means the controller did not report them.
const flag = (value: boolean | undefined, on: string, off: string) =>
  value === undefined ? "Unknown" : value ? on : off;

const failure = (action: AlarmAction, error: unknown): Extract<PlantAlarmOutcome, { phase: "failed" }> => {
  const failed = error as
    { message?: string; writeOutcome?: "refused" | "unknown"; status?: number; error?: { code?: number } } | undefined;
  if (failed?.writeOutcome === "refused" && failed.message) {
    return { phase: "failed", action, kind: "refused", message: failed.message };
  }
  // The request never reached the controller: authentication failed before any command was processed.
  if (failed?.status === 401 || failed?.error?.code === 16) {
    return {
      phase: "failed",
      action,
      kind: "refused",
      message: "Sign in again before trying again. The alarm state was not changed.",
    };
  }
  // Anything else (timeout, dropped connection) may have reached the PLC.
  return { phase: "failed", action, kind: "unknown", message: COPY[action].unknown };
};

interface ContainerPlantAlarmsProps {
  settings: Pick<ContainerSettings, "alarmMute" | "alarmReset">;
  /** True while a command is being sent or confirmed, so the host can keep itself open. */
  onBusyChange?: (busy: boolean) => void;
}

export default function ContainerPlantAlarms({ settings, onBusyChange }: ContainerPlantAlarmsProps) {
  const { adapter, isFleetHosted } = useMinerHosting();
  const authRetry = useAuthRetry();
  const nowMs = useNow();
  const [confirming, setConfirming] = useState<{ action: AlarmAction; generation: number }>();
  // The outcome outlives this section, and an in-flight write is never aborted, so its result is
  // still recorded after the modal closes.
  const { generation: currentGeneration, outcome, readback, plant } = usePlantAlarmReadback(nowMs);
  const waiting = readback?.state === "waiting";
  // Re-read quickly while the result is open, instead of waiting for the next Home poll.
  useTelemetry({
    level: READBACK_LEVELS,
    enabled: outcome?.phase === "sent" && nowMs - outcome.acknowledgedAtMs < ALARM_READBACK_WINDOW_MS,
    poll: true,
    pollIntervalMs: READBACK_POLL_MS,
  });
  const sendingAction = outcome?.phase === "sending" ? outcome.action : undefined;
  const sending = sendingAction !== undefined;
  const busy = sending || confirming !== undefined;
  useEffect(() => onBusyChange?.(busy), [busy, onBusyChange]);

  const send = async (action: AlarmAction, generation: number) => {
    if (useMinerStore.getState().connectionGeneration !== generation) return;
    const write = action === "mute" ? adapter?.muteContainerAlarm : adapter?.resetContainerAlarm;
    if (!adapter || !write) return;
    const controller = new AbortController();
    setPlantAlarmOutcome(generation, { phase: "sending", action });
    const isCurrent = () => useMinerStore.getState().connectionGeneration === generation;
    let failed: unknown;
    let accepted = false;
    try {
      // One command. Only an authentication rejection, which the controller never processed, is
      // re-sent after the token refresh; a timeout or any other failure is an unknown outcome.
      await withDeadline(
        () =>
          authRetry({
            signal: controller.signal,
            shouldRetry: () => isCurrent() && !controller.signal.aborted,
            request: (params: AdapterRequestOptions) => write.call(adapter, params),
            onSuccess: () => {
              accepted = true;
            },
            onError: (error) => {
              failed = error;
            },
          }),
        controller,
        WRITE_TIMEOUT_MS,
      );
    } catch (error) {
      failed = error;
    }
    if (!isCurrent()) return;
    setPlantAlarmOutcome(
      generation,
      // The readback baseline is taken at acknowledgement, not when the operator confirmed.
      accepted && !failed ? acknowledgedOutcome(action) : failure(action, failed),
    );
  };

  // Results stay until dismissed; only "no newer reading yet" can still be resolved by a reading.
  const result = outcomeMessage(outcome, readback);
  const evidence = hasAlarmEvidence(plant);
  const observed = observationState(
    { freshness: "fresh", observedAtMs: plant?.observedAtMs },
    nowMs,
    PLANT_OBSERVATION_STALE_MS,
  );
  const freshness =
    plant?.observedAtMs === undefined
      ? "Observation time not reported"
      : observed === "unknown"
        ? "Observation time unreliable"
        : `Observed ${formatAge(plant.observedAtMs, nowMs)}${observed === "stale" ? " · Stale" : ""}`;
  const alarms = plant?.alarms ?? [];
  const actions = (["mute", "reset"] as const).map((action) => ({
    action,
    availability: alarmActionAvailability(action, { settings, plant, adapter, isFleetHosted }),
  }));
  // Reasons sit under the whole button row, so a reason shared by both actions shows once.
  const unavailableReasons = [
    ...new Set(actions.flatMap(({ availability }) => (availability.available ? [] : [availability.reason]))),
  ];

  return (
    <section aria-label="Container alarms" className="space-y-4">
      <div>
        <h2 className="text-heading-100">Container alarms</h2>
        <p className="text-200 text-text-primary-70" data-observation={observed}>
          {freshness}
        </p>
      </div>
      <dl className="grid grid-cols-3 gap-4">
        {[
          ["Alarm", flag(plant?.alarmActive, "Active", "Clear")],
          ["Critical alarm", flag(plant?.criticalAlarmActive, "Active", "Clear")],
          ["Buzzer", flag(plant?.buzzerActive, "Sounding", "Silent")],
        ].map(([label, value]) => (
          <div key={label}>
            <dt className="text-200 text-text-primary-70">{label}</dt>
            <dd className="text-emphasis-300">{value}</dd>
          </div>
        ))}
      </dl>
      {alarms.length > 0 ? (
        <ul className="space-y-2">
          {alarms.map((alarm, index) => (
            <li
              key={`${alarm.id ?? alarm.name}:${alarm.componentId ?? ""}:${index}`}
              className="flex items-start gap-2"
            >
              <StatusCircle status={SEVERITY[alarm.severity].status} variant="simple" width="w-2" removeMargin />
              <span>
                <span className="text-emphasis-300">{alarm.name}</span>
                <span className="block text-200 text-text-primary-70">
                  {[SEVERITY[alarm.severity].label, alarm.componentId].filter(Boolean).join(" · ")}
                </span>
              </span>
            </li>
          ))}
        </ul>
      ) : plant?.alarmActive === true || plant?.criticalAlarmActive === true ? (
        <p className="text-200 text-text-primary-70">The controller reports an active alarm but does not list it.</p>
      ) : (
        <p className="text-200 text-text-primary-70">
          {plant?.alarmActive === false ? "No active alarms." : "Alarm state not reported."}
        </p>
      )}
      {evidence ? (
        <div className="space-y-2">
          <div className="flex flex-wrap gap-2">
            {actions.map(({ action, availability }) => (
              <Button
                key={action}
                variant="secondary"
                size="compact"
                text={COPY[action].button}
                loading={sendingAction === action}
                disabled={!availability.available || sending || waiting}
                onClick={() => setConfirming({ action, generation: useMinerStore.getState().connectionGeneration })}
              />
            ))}
          </div>
          {unavailableReasons.map((reason) => (
            <p key={reason} className="text-200 text-text-primary-70">
              {reason}
            </p>
          ))}
        </div>
      ) : null}
      {outcome?.phase === "sent" && waiting ? (
        <p role="status" className="text-200 text-text-primary-70">
          Sent; waiting for the controller to report…
        </p>
      ) : null}
      {result ? (
        <div className="flex items-start justify-between gap-4">
          <p role={result.tone === "neutral" ? "status" : "alert"} className={`text-200 ${OUTCOME_TONES[result.tone]}`}>
            {result.message}
          </p>
          <Button
            variant="textOnly"
            text="Dismiss"
            onClick={() => setPlantAlarmOutcome(currentGeneration, undefined)}
          />
        </div>
      ) : null}
      {confirming ? (
        <Dialog
          title={COPY[confirming.action].title}
          subtitle={COPY[confirming.action].subtitle}
          manageFocus
          preventScroll
          onDismiss={() => setConfirming(undefined)}
          buttons={[
            { text: "Cancel", variant: variants.secondary, onClick: () => setConfirming(undefined) },
            {
              text: COPY[confirming.action].button,
              variant: variants.primary,
              onClick: () => {
                const { action, generation } = confirming;
                setConfirming(undefined);
                void send(action, generation);
              },
            },
          ]}
        />
      ) : null}
    </section>
  );
}
