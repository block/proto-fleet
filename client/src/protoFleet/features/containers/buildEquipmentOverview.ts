import type { ContainerToggleControl } from "./components/ContainerControls";
import type { ContainerFan } from "./components/ContainerOverviewLayout";
import type { Container } from "@/protoFleet/api/generated/containers/v1/containers_pb";

const controlNames: Record<string, string> = {
  dry_cooler: "Dry cooler",
  dry_cooler_auto: "Dry cooler auto mode",
  tank_led_light: "Tank lights",
  coolant_pump: "Coolant pump",
  air_conditioner_1: "Air conditioner 1",
  air_conditioner_2: "Air conditioner 2",
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

export function buildEquipmentOverview(container: Container, failed = false) {
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
    for (const control of observation.controls) {
      const label = controlNames[control.name];
      if (!label) continue;
      const setpoint = observation.setpoints.find((s) => s.name === setpointNames[control.name]);
      controls.push({
        id: control.name,
        label,
        on: stale ? undefined : control.enabled,
        stateLabel: stale ? "Stale" : undefined,
        metric:
          !stale && setpoint?.value !== undefined ? `${setpoint.value} ${setpoint.unit} setpoint`.trim() : undefined,
      });
    }
    if (observation.mainCircuitBreakerSupported !== undefined || observation.mainCircuitBreakerClosed !== undefined) {
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
            : observation.mainCircuitBreakerSupported === false
              ? "Unsupported"
              : "Unknown",
      });
    }
  }
  const controlsNotice = !observation
    ? "Equipment states unavailable."
    : stale
      ? "Equipment states are stale."
      : observation.supported === false
        ? "Equipment controls are unsupported by this controller."
        : controls.length === 0
          ? "No equipment states reported."
          : undefined;
  return { fans, controls, controlsNotice };
}
