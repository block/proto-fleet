import type { ContainerSettings, MinerAdapter } from "@/protoOS/api/adapters/types";
import type { PlantTelemetry } from "@/protoOS/store/contract";

export type AlarmAction = "mute" | "reset";

// Frigate re-polls the plant right after an accepted command, so a newer observation
// normally lands within seconds. Beyond this window the outcome stays unconfirmed.
export const ALARM_READBACK_WINDOW_MS = 120_000;
// A readback that still shows the alarm can be a poll that started before the command
// landed. Only a confirmation is shown at once; a contradiction waits this long.
export const ALARM_READBACK_SETTLE_MS = 6_000;
// The Home poll runs every 15 seconds, so one missed poll is not yet stale.
export const PLANT_OBSERVATION_STALE_MS = 45_000;

export const HOSTED_ALARM_REASON = "Muting or resetting alarms needs a direct connection to the controller.";

/** Evidence that an alarm is active; absent flags are unknown, never clear. */
export const hasActiveAlarm = (plant?: PlantTelemetry) =>
  plant?.alarmActive === true || plant?.criticalAlarmActive === true || (plant?.alarms?.length ?? 0) > 0;

/** Actions are only offered while the controller reports an active alarm or a sounding buzzer. */
export const hasAlarmEvidence = (plant?: PlantTelemetry) => hasActiveAlarm(plant) || plant?.buzzerActive === true;

export type AlarmAvailability = { available: true } | { available: false; reason: string };

export const alarmActionAvailability = (
  action: AlarmAction,
  {
    settings,
    plant,
    adapter,
    isFleetHosted,
  }: {
    settings: Pick<ContainerSettings, "alarmMute" | "alarmReset">;
    plant?: PlantTelemetry;
    adapter?: Pick<MinerAdapter, "muteContainerAlarm" | "resetContainerAlarm"> | null;
    isFleetHosted: boolean;
  },
): AlarmAvailability => {
  if (isFleetHosted) return { available: false, reason: HOSTED_ALARM_REASON };
  if (action === "reset") {
    if (settings.alarmReset !== true) {
      return {
        available: false,
        reason: "This controller does not support resetting alarms. Alarms clear on their own once their cause clears.",
      };
    }
  } else if (settings.alarmMute !== true) {
    return { available: false, reason: "This controller does not support muting alarms." };
  } else if (plant?.alarmMuteSupported === false) {
    // Live health narrows what the controls response advertised.
    return { available: false, reason: "The controller cannot mute alarms right now." };
  }
  const send = action === "mute" ? adapter?.muteContainerAlarm : adapter?.resetContainerAlarm;
  if (!send) return { available: false, reason: "This connection cannot send alarm commands." };
  return { available: true };
};

/**
 * `confirmed` and `unconfirmed` are decided by a reading taken after the command and are final.
 * `unreported` means no such reading arrived in the window; a later reading can still decide it.
 */
export type AlarmReadback =
  | { state: "waiting" }
  | { state: "confirmed"; message: string }
  | { state: "unconfirmed"; message: string }
  | { state: "unreported"; message: string };

const plural = (count: number) => `${count} active alarm${count === 1 ? "" : "s"}`;

/**
 * Reads the outcome of an accepted command from the plant observation, never from the write
 * response. Only a reading from a request started after the acknowledgement counts, and it must
 * be newer, in controller time, than the reading held at the acknowledgement; a reading fetched
 * while the write was in flight cannot confirm it. The browser clock only sets the waiting windows.
 */
export const alarmReadback = ({
  action,
  baselineObservedAtMs,
  afterRequestId,
  readingRequestId,
  acknowledgedAtMs,
  nowMs,
  plant,
}: {
  action: AlarmAction;
  /** Controller time of the plant reading held when the command was acknowledged. */
  baselineObservedAtMs?: number;
  /** The store's request sequence at acknowledgement; readings from requests up to it are excluded. */
  afterRequestId: number;
  /** The request that produced the current plant reading. */
  readingRequestId?: number;
  acknowledgedAtMs: number;
  nowMs: number;
  plant?: PlantTelemetry;
}): AlarmReadback => {
  const observedAtMs = plant?.observedAtMs;
  const newer =
    observedAtMs !== undefined &&
    readingRequestId !== undefined &&
    readingRequestId > afterRequestId &&
    (baselineObservedAtMs === undefined || observedAtMs > baselineObservedAtMs);
  const elapsedMs = nowMs - acknowledgedAtMs;
  if (!newer) {
    return elapsedMs >= ALARM_READBACK_WINDOW_MS
      ? {
          state: "unreported",
          message: `${action === "mute" ? "Mute" : "Reset"} sent, but the controller has not reported since. Check the current alarm state before trying again.`,
        }
      : { state: "waiting" };
  }
  if (action === "mute") {
    if (plant?.buzzerActive === false) {
      return {
        state: "confirmed",
        message: "Buzzer muted. Alarms stay active until their cause clears.",
      };
    }
    if (elapsedMs < ALARM_READBACK_SETTLE_MS) return { state: "waiting" };
    return {
      state: "unconfirmed",
      message:
        plant?.buzzerActive === true
          ? "Mute sent, but the controller still reports the buzzer sounding."
          : "Mute sent. This controller does not report buzzer state, so the result cannot be confirmed.",
    };
  }
  const listed = plant?.alarms?.length ?? 0;
  // Both flags must be reported clear: an unreported critical flag is unknown, not clear.
  if (plant?.alarmActive === false && plant.criticalAlarmActive === false && listed === 0) {
    return { state: "confirmed", message: "Alarms reset. The controller reports no active alarms." };
  }
  if (elapsedMs < ALARM_READBACK_SETTLE_MS) return { state: "waiting" };
  if (hasActiveAlarm(plant)) {
    return {
      state: "unconfirmed",
      message: `Reset sent, but the controller still reports ${listed > 0 ? plural(listed) : "an active alarm"}. An alarm whose cause is still present stays active.`,
    };
  }
  return {
    state: "unconfirmed",
    message: "Reset sent. This controller does not report alarm state, so the result cannot be confirmed.",
  };
};
