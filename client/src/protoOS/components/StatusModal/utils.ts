import { createElement } from "react";
import type { ComponentHardware, ComponentTelemetry } from "./hooks";
import type {
  ComponentRef,
  ControlBoardHardware,
  FanTelemetry,
  HashboardHardware,
  HashboardTelemetry,
  MinerError,
  PsuHardware,
  PsuTelemetry,
} from "@/protoOS/store/contract";
import EfficiencyValue from "@/shared/components/EfficiencyValue";
import HashRateValue from "@/shared/components/HashRateValue";
import PowerValue from "@/shared/components/PowerValue";
import type {
  ComponentMetadata,
  ComponentMetric,
  ComponentStatusModalProps,
  ComponentType,
  ErrorData,
} from "@/shared/components/StatusModal/types";
import TemperatureValue from "@/shared/components/TemperatureValue";

/**
 * Get the display title for a component type
 */
export const getComponentTitle = (type: ComponentType): string => {
  switch (type) {
    case "fan":
      return "Fan status";
    case "hashboard":
      return "Hashboard status";
    case "psu":
      return "PSU status";
    case "controlBoard":
      return "Control board status";
    case "tank":
      return "Tank status";
    case "other":
      return "Needs attention";
  }
};

/**
 * Get component metrics from telemetry data
 */
function getComponentMetrics(type: ComponentType, telemetry: ComponentTelemetry): ComponentMetric[] {
  const metrics: ComponentMetric[] = [];

  switch (type) {
    case "fan":
      if (telemetry) {
        const rpm = (telemetry as FanTelemetry).rpm?.latest?.value;
        const pwm = (telemetry as FanTelemetry).speedPercent?.latest?.value;

        metrics.push({
          label: pwm == null ? "PWM unavailable" : `${pwm}% PWM`,
          value: rpm == null ? "—" : `${rpm} RPM`,
        });
      }
      break;

    case "psu":
      if (telemetry) {
        const psuTelemetry = telemetry as PsuTelemetry;
        metrics.push(
          {
            label: "Input Power",
            value: createElement(PowerValue, {
              value: psuTelemetry.inputPower?.latest?.value ?? null,
            }),
          },
          {
            label: "Output Power",
            value: createElement(PowerValue, {
              value: psuTelemetry.outputPower?.latest?.value ?? null,
            }),
          },
          {
            label: "Average Temp",
            value: createElement(TemperatureValue, {
              value: psuTelemetry.averageTemperature?.latest?.value ?? null,
            }),
          },
          {
            label: "Max Temp",
            value: createElement(TemperatureValue, {
              value: psuTelemetry.hotspotTemperature?.latest?.value ?? null,
            }),
          },
        );
      }
      break;

    case "hashboard":
      if (telemetry) {
        metrics.push(
          {
            label: "Hashrate",
            value: createElement(HashRateValue, {
              value: (telemetry as HashboardTelemetry).hashrate?.latest?.value ?? null,
            }),
          },
          {
            label: "Power",
            value: createElement(PowerValue, {
              value: (telemetry as HashboardTelemetry).power?.latest?.value ?? null,
            }),
          },
          {
            label: "ASIC Avg Temp",
            value: createElement(TemperatureValue, {
              value: (telemetry as HashboardTelemetry).averageAsicTemperature?.latest?.value ?? null,
            }),
          },
          {
            label: "ASIC High Temp",
            value: createElement(TemperatureValue, {
              value: (telemetry as HashboardTelemetry).maximumAsicTemperature?.latest?.value ?? null,
            }),
          },
          {
            label: "Efficiency",
            value: createElement(EfficiencyValue, {
              value: (telemetry as HashboardTelemetry).efficiency?.latest?.value ?? null,
            }),
          },
        );
      }
      break;
  }

  return metrics;
}

/**
 * Get component metadata from hardware data
 */
function getComponentMetadata(type: ComponentType, hardware: ComponentHardware): ComponentMetadata {
  const metadata: ComponentMetadata = {};

  if (!hardware) return metadata;

  switch (type) {
    case "psu":
      metadata.serialNumber = {
        label: "Serial Number",
        value: (hardware as PsuHardware).serialNumber,
      };
      metadata.model = {
        label: "Model",
        value: (hardware as PsuHardware).model,
      };
      metadata.firmwareVersion = {
        label: "Firmware Version",
        value: (hardware as PsuHardware).firmware?.application?.version,
      };
      break;

    case "hashboard":
      metadata.serialNumber = {
        label: (hardware as HashboardHardware).identity.kind === "mac" ? "MAC Address" : "Serial Number",
        value: (hardware as HashboardHardware).identity.value,
      };
      metadata.model = {
        label: "Model",
        value: (hardware as HashboardHardware).boardModel,
      };
      metadata.firmwareVersion = {
        label: "Firmware Version",
        value: (hardware as HashboardHardware).firmware?.version,
      };
      metadata.asicCount = {
        label: "ASIC Count",
        value: (hardware as HashboardHardware).miningAsicCount,
      };
      break;

    case "controlBoard":
      metadata.serialNumber = {
        label: "Serial Number",
        value: (hardware as ControlBoardHardware).serialNumber,
      };
      break;
  }

  return metadata;
}

/** Error navigation carries the API-normalized identity, including its owner. */
export function transformErrorsForModal(
  errors: MinerError[],
  name: (component: ComponentRef) => string,
  onErrorClick?: (component: ComponentRef) => void,
): ErrorData[] {
  return errors.map((error) => ({
    componentName: error.component ? name(error.component) : "Miner",
    message: error.message,
    timestamp: error.timestampMs === undefined ? undefined : error.timestampMs / 1000,
    onClick: error.component && onErrorClick ? () => onErrorClick(error.component!) : undefined,
  }));
}

export function buildComponentStatusProps(
  component: ComponentRef,
  name: string,
  errors: MinerError[],
  telemetry: ComponentTelemetry,
  hardware: ComponentHardware,
): ComponentStatusModalProps {
  const componentType: ComponentType =
    component.kind === "asic" || component.kind === "plant" ? "other" : component.kind;
  const componentErrors = errors.filter(
    (error) => error.component?.kind === component.kind && error.component.id === component.id,
  );
  const health = telemetry && "health" in telemetry ? telemetry.health : undefined;
  const status =
    health === "unknown"
      ? "Status unknown"
      : health === "offline"
        ? "Offline"
        : health === "fault"
          ? "Fault"
          : health === "degraded"
            ? "Degraded"
            : !telemetry
              ? "Readings unavailable"
              : undefined;
  return {
    summary: status ? `${name} · ${status}` : name,
    componentType,
    health,
    errors: transformErrorsForModal(componentErrors, () => name),
    metrics: getComponentMetrics(componentType, telemetry),
    metadata: getComponentMetadata(componentType, hardware),
  };
}
