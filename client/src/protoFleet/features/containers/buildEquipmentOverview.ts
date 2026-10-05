import { timestampDate } from "@bufbuild/protobuf/wkt";
import type { ContainerToggleControl } from "./components/ContainerControls";
import type { ContainerFan } from "./components/ContainerOverviewLayout";
import type { Container } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { convertCtoF } from "@/shared/utils/telemetryFormat";

const controlNames: Record<string, string> = {
  dry_cooler: "Dry cooler",
  dry_cooler_auto: "Auto mode",
  air_conditioner_1: "AC 1",
  air_conditioner_2: "AC 2",
  tank_led_light: "Tank lights",
  coolant_pump: "Coolant pump",
};
const setpointNames: Record<string, string> = {
  dry_cooler_auto: "dry_cooler_auto_temp_c",
  coolant_pump: "coolant_pump_speed_hz",
};
const directions: Record<string, string> = {
  PLANT_COOLING_FAN_DIRECTION_FORWARD: "Forward",
  PLANT_COOLING_FAN_DIRECTION_REVERSE: "Reverse",
  PLANT_COOLING_FAN_DIRECTION_UNKNOWN: "Direction unknown",
};

export function buildEquipmentOverview(container: Container, failed = false, unit: "C" | "F" = "C") {
  const temperature = (celsius: number) =>
    `${(unit === "F" ? convertCtoF(celsius) : celsius).toLocaleString(undefined, { maximumFractionDigits: 1 })}°${unit}`;
  const fresh = (at: Container["observedAt"]) => {
    if (!at) return false;
    const age = Date.now() - timestampDate(at).getTime();
    return age >= -5000 && age <= 120_000;
  };
  const dryCoolerReadings: string[] = [];
  const plant = container.plant;
  if (
    !failed &&
    plant &&
    !plant.stale &&
    fresh(plant.observedAt) &&
    plant.dryCoolerInletTemperatureC !== undefined &&
    Number.isFinite(plant.dryCoolerInletTemperatureC)
  ) {
    dryCoolerReadings.push(`${temperature(plant.dryCoolerInletTemperatureC)} inlet`);
  }
  // Report an average only when every advertised fan has a current speed.
  // A manual speed setpoint is not evidence of the speed in automatic mode.
  if (
    !failed &&
    container.fans.length &&
    container.fans.every(
      (fan) =>
        !fan.stale &&
        fresh(fan.observedAt) &&
        fan.speedPercent !== undefined &&
        Number.isFinite(fan.speedPercent) &&
        fan.speedPercent >= 0 &&
        fan.speedPercent <= 100,
    )
  ) {
    const average = container.fans.reduce((sum, fan) => sum + fan.speedPercent!, 0) / container.fans.length;
    dryCoolerReadings.push(`${Math.round(average)}% avg fan speed`);
  }
  const fans: ContainerFan[] = container.fans.map((fan, index) => {
    const stale = fan.stale || failed;
    return {
      id: fan.componentId,
      label: `Fan ${fan.componentId.match(/(\d+)$/)?.[1] ?? index + 1}`,
      on: undefined,
      speedPercent: stale ? undefined : fan.speedPercent,
      powerLabel:
        stale || fan.powerW === undefined ? "—" : fan.powerW.toLocaleString(undefined, { maximumFractionDigits: 1 }),
      faulted: !stale && fan.alarmActive === true,
      statusLabel: stale
        ? "Stale"
        : fan.speedPercent === undefined && fan.powerW === undefined
          ? "Readings unavailable"
          : undefined,
      directionLabel: stale || fan.direction === undefined ? undefined : directions[fan.direction],
    };
  });
  const observation = container.controls;
  const stale = observation?.stale || failed;
  const controls: ContainerToggleControl[] = [];
  if (observation && observation.supported !== false) {
    for (const [name, label] of Object.entries(controlNames)) {
      const control = observation.controls.find((control) => control.name === name);
      if (!control) continue;
      const setpoint = observation.setpoints.find((s) => s.name === setpointNames[control.name]);
      controls.push({
        id: control.name,
        label,
        on: stale ? undefined : control.enabled,
        stateLabel: stale ? "Stale" : undefined,
        metric:
          name === "dry_cooler"
            ? dryCoolerReadings.join(" · ") || undefined
            : !stale && setpoint?.value !== undefined && Number.isFinite(setpoint.value)
              ? name === "dry_cooler_auto"
                ? `Target ${temperature(setpoint.value)}`
                : `${setpoint.value} ${setpoint.unit} setpoint`.trim()
              : undefined,
      });
    }
  }
  if (observation?.mainCircuitBreakerSupported === true) {
    controls.push({
      id: "main_circuit_breaker",
      label: "Main circuit breaker",
      on: undefined,
      stateLabel: stale
        ? "Stale"
        : observation.mainCircuitBreakerClosed !== undefined
          ? observation.mainCircuitBreakerClosed
            ? "Closed"
            : "Open"
          : "Unknown",
    });
  }
  const controlsNotice = !observation
    ? "Equipment states unavailable."
    : stale
      ? "Equipment states are stale."
      : observation.supported === false && observation.mainCircuitBreakerSupported !== true
        ? "Equipment controls are unsupported by this controller."
        : controls.length === 0
          ? "No equipment states reported."
          : undefined;
  return { fans, controls, controlsNotice };
}
