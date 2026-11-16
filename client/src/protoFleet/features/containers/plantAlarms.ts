import { timestampDate } from "@bufbuild/protobuf/wkt";
import type { Container, ContainerPlant } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { PlantAlarmAction } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { type ErrorMessage, Severity } from "@/protoFleet/api/generated/errors/v1/errors_pb";

// Same rule equipment controls use: an observation older than two minutes, or
// stamped in the future, is not evidence of the controller's current state.
const FRESH_MS = 120_000;
const SKEW_MS = 5000;
// The controller may act a moment after the command, so an observation this close to completion
// that still shows the buzzer or alarms is not yet evidence that the command left them.
export const READBACK_SETTLE_MS = 6000;

/** Plant alarm errors carry this component id prefix; the suffix is the controller's alarm id. */
export const PLANT_ALARM_COMPONENT_PREFIX = "plant-alarm:";

export type AlarmActionName = "mute" | "reset";
export const alarmActionEnum = { mute: PlantAlarmAction.MUTE, reset: PlantAlarmAction.RESET } as const;

export const isFreshObservation = (at: ContainerPlant["observedAt"], stale = false) => {
  if (!at || stale) return false;
  const age = Date.now() - timestampDate(at).getTime();
  return age >= -SKEW_MS && age <= FRESH_MS;
};

/** The panel is only worth showing when the controller reports alarm state or advertises alarm actions. */
export function hasPlantAlarmSurface(container: Container) {
  const plant = container.plant;
  const controls = container.controls;
  return (
    plant?.alarmActive !== undefined ||
    plant?.criticalAlarmActive !== undefined ||
    plant?.buzzerActive !== undefined ||
    controls?.alarmMuteSupported !== undefined ||
    controls?.alarmResetSupported !== undefined
  );
}

/** True when current evidence shows an active alarm or a sounding buzzer. Absent flags never count as clear. */
export function alarmEvidenceActive(plant: ContainerPlant | undefined) {
  return plant?.alarmActive === true || plant?.criticalAlarmActive === true || plant?.buzzerActive === true;
}

/**
 * Capability needs a fresh controls observation that advertises the action.
 * `unknown` means Fleet cannot tell right now; `unsupported` means the
 * controller said it cannot do this.
 */
export function alarmActionSupport(container: Container | undefined, action: AlarmActionName) {
  const controls = container?.controls;
  if (!controls || !isFreshObservation(controls.observedAt, controls.stale)) return "unknown" as const;
  const flag = action === "mute" ? controls.alarmMuteSupported : controls.alarmResetSupported;
  return flag === true ? ("supported" as const) : flag === false ? ("unsupported" as const) : ("unknown" as const);
}

export interface AlarmOutcome {
  text: string;
  tone: "success" | "warning";
}

/**
 * Judge an action from a plant observation newer than `after`: the later of the
 * command's recorded completion and the plant observation shown when it was
 * sent. Undefined means keep waiting for a later observation.
 */
export function readbackOutcome(
  action: AlarmActionName,
  plant: ContainerPlant | undefined,
  after: number | undefined,
): AlarmOutcome | undefined {
  if (!plant?.observedAt || plant.stale || after === undefined) return undefined;
  const observedAt = timestampDate(plant.observedAt).getTime();
  if (observedAt <= after) return undefined;
  if (action === "mute") {
    if (plant.buzzerActive === undefined) {
      return {
        text: "Mute sent. This controller does not report buzzer state, so the result cannot be confirmed.",
        tone: "warning",
      };
    }
    if (!plant.buzzerActive) {
      return { text: "Alarm buzzer silenced. Alarms stay active until their cause clears.", tone: "success" };
    }
    if (observedAt - after < READBACK_SETTLE_MS) return undefined;
    return { text: "Mute sent, but the controller still reports the buzzer sounding.", tone: "warning" };
  }
  if (plant.alarmActive === true || plant.criticalAlarmActive === true) {
    if (observedAt - after < READBACK_SETTLE_MS) return undefined;
    return {
      text: "Reset sent, but the controller still reports active alarms. An alarm whose cause is still present stays active.",
      tone: "warning",
    };
  }
  // Both flags must be reported clear: an unreported critical flag is unknown, not clear.
  if (plant.alarmActive === false && plant.criticalAlarmActive === false)
    return { text: "Alarms cleared.", tone: "success" };
  return {
    text: "Reset sent. This controller does not report alarm state, so the result cannot be confirmed.",
    tone: "warning",
  };
}

export interface PlantAlarmItem {
  id: string;
  name: string;
  severity: Severity;
  /** Last time the controller confirmed this alarm; for carried alarms this is not the latest poll. */
  confirmedAt: Date | undefined;
  /** The plugin carried this alarm because its source was silent, not because the latest poll listed it. */
  retained: boolean;
}

const severityRank: Record<number, number> = {
  [Severity.CRITICAL]: 0,
  [Severity.MAJOR]: 1,
  [Severity.MINOR]: 2,
  [Severity.INFO]: 3,
};

export const severityLabel = (severity: Severity) =>
  ({
    [Severity.CRITICAL]: "Critical",
    [Severity.MAJOR]: "Major",
    [Severity.MINOR]: "Minor",
    [Severity.INFO]: "Info",
  })[severity as number] ?? "Unspecified";

/** Individual alarms are open device errors whose component id starts with `plant-alarm:`. */
export function plantAlarmItems(errors: ErrorMessage[]): PlantAlarmItem[] {
  return errors
    .filter((error) => error.componentId?.startsWith(PLANT_ALARM_COMPONENT_PREFIX) && !error.closedAt)
    .map((error) => {
      const retained = error.vendorAttributes.ct1_evidence === "retained";
      const carried = Date.parse(error.vendorAttributes.ct1_last_confirmed_at ?? "");
      const lastSeen = error.lastSeenAt ? timestampDate(error.lastSeenAt) : undefined;
      return {
        id: error.errorId,
        name: error.causeSummary || error.summary || error.componentId!.slice(PLANT_ALARM_COMPONENT_PREFIX.length),
        severity: error.severity,
        retained,
        confirmedAt: retained && Number.isFinite(carried) ? new Date(carried) : lastSeen,
      };
    })
    .sort((a, b) => (severityRank[a.severity] ?? 4) - (severityRank[b.severity] ?? 4) || a.name.localeCompare(b.name));
}
