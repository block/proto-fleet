import { createElement } from "react";
import type { ComponentHardware, ComponentTelemetry } from "./hooks";
import { getComponentDisplayName } from "@/protoOS/hooks/status/useComponentDisplayName";
import type {
  ControlBoardHardwareData,
  ErrorSource,
  FanTelemetryData,
  HashboardHardwareData,
  HashboardTelemetryData,
  MinerError,
  PsuHardwareData,
  PsuTelemetryData,
} from "@/protoOS/store/types";
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
    case "other":
      return "Needs attention";
  }
};

/**
 * Maps error source to ComponentType
 */
export function mapErrorSourceToComponentType(source: ErrorSource): ComponentType | null {
  const mapping: Record<ErrorSource, ComponentType | null> = {
    HASHBOARD: "hashboard",
    PSU: "psu",
    FAN: "fan",
    RIG: "controlBoard",
  };

  return mapping[source] || null;
}

/**
 * Transforms MinerError to ErrorData format for shared/components/StatusModal
 */
function transformMinerErrorToError(error: MinerError): ErrorData {
  return {
    componentName: getComponentDisplayName(error.source, error.slot),
    message: error.message,
    timestamp: error.timestamp,
  };
}

/**
 * Get component metrics from telemetry data
 */
function getComponentMetrics(type: ComponentType, telemetry: ComponentTelemetry): ComponentMetric[] {
  const metrics: ComponentMetric[] = [];

  switch (type) {
    case "fan":
      if (telemetry) {
        const rpm = (telemetry as FanTelemetryData).rpm?.latest?.value || 0;
        const pwm = (telemetry as FanTelemetryData).percentage?.latest?.value || 0;

        metrics.push({
          label: `${pwm}% PWM`,
          value: `${rpm} RPM`,
        });
      }
      break;

    case "psu":
      if (telemetry) {
        const psuTelemetry = telemetry as PsuTelemetryData;
        metrics.push(
          {
            label: "Input Power",
            value: createElement(PowerValue, {
              value: psuTelemetry.inputPower?.latest?.value,
            }),
          },
          {
            label: "Output Power",
            value: createElement(PowerValue, {
              value: psuTelemetry.outputPower?.latest?.value,
            }),
          },
          {
            label: "Average Temp",
            value: createElement(TemperatureValue, {
              value: psuTelemetry.temperatureAverage?.latest?.value,
            }),
          },
          {
            label: "Max Temp",
            value: createElement(TemperatureValue, {
              value: psuTelemetry.temperatureHotspot?.latest?.value,
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
              value: (telemetry as HashboardTelemetryData).hashrate?.latest?.value || 0,
            }),
          },
          {
            label: "Power",
            value: createElement(PowerValue, {
              value: (telemetry as HashboardTelemetryData).power?.latest?.value,
            }),
          },
          {
            label: "ASIC Avg Temp",
            value: createElement(TemperatureValue, {
              value: (telemetry as HashboardTelemetryData).avgAsicTemp?.latest?.value || 0,
            }),
          },
          {
            label: "ASIC High Temp",
            value: createElement(TemperatureValue, {
              value: (telemetry as HashboardTelemetryData).maxAsicTemp?.latest?.value || 0,
            }),
          },
          {
            label: "Efficiency",
            value: createElement(EfficiencyValue, {
              value: (telemetry as HashboardTelemetryData).efficiency?.latest?.value || 0,
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
        value: (hardware as PsuHardwareData).serial,
      };
      metadata.model = {
        label: "Model",
        value: (hardware as PsuHardwareData).model,
      };
      metadata.firmwareVersion = {
        label: "Firmware Version",
        value: (hardware as PsuHardwareData).firmware?.appVersion,
      };
      break;

    case "hashboard":
      metadata.serialNumber = {
        label: "Serial Number",
        value: (hardware as HashboardHardwareData).serial,
      };
      metadata.model = {
        label: "Model",
        value: (hardware as HashboardHardwareData).board,
      };
      metadata.firmwareVersion = {
        label: "Firmware Version",
        value: (hardware as HashboardHardwareData).firmware?.version,
      };
      metadata.asicCount = {
        label: "ASIC Count",
        value: (hardware as HashboardHardwareData).miningAsicCount,
      };
      metadata.slotLocation = {
        label: "Slot Location",
        value: (hardware as HashboardHardwareData).slot,
      };
      break;

    case "controlBoard":
      metadata.serialNumber = {
        label: "Serial Number",
        value: (hardware as ControlBoardHardwareData).serial,
      };
      break;
  }

  return metadata;
}

/**
 * Transforms miner errors into the format expected by StatusModal
 * Includes onClick handlers for navigating to component details
 * @param minerErrors - Array of miner errors from the store
 * @param onErrorClick - Optional callback when an error row is clicked
 */
export function transformErrorsForModal(
  minerErrors: MinerError[] = [],
  onErrorClick?: (source: ErrorSource, slot?: number) => void,
): ErrorData[] {
  return minerErrors.map((error) => ({
    componentName: getComponentDisplayName(error.source, error.slot),
    message: error.message,
    timestamp: error.timestamp,
    onClick: onErrorClick ? () => onErrorClick(error.source, error.slot) : undefined,
  }));
}

/**
 * Build complete props for ComponentStatusModal
 */
export function buildComponentStatusProps(
  source: ErrorSource,
  slot: number | undefined,
  errors: MinerError[],
  telemetry: ComponentTelemetry,
  hardware: ComponentHardware,
): ComponentStatusModalProps | null {
  const componentType = mapErrorSourceToComponentType(source);
  if (!componentType) return null;

  // Filter errors for this specific component
  const componentErrors = errors.filter((error) => {
    return error.source === source && error.slot === slot;
  });

  // Transform errors to Error format
  const transformedErrors = componentErrors.map(transformMinerErrorToError);

  // Generate summary text
  const componentName = getComponentDisplayName(source, slot);
  const errorCount = transformedErrors.length;
  const summary =
    errorCount === 0
      ? componentName
      : errorCount === 1
        ? `${componentName} has an error`
        : `${componentName} has ${errorCount} errors`;

  return {
    summary,
    componentType,
    errors: transformedErrors,
    metrics: getComponentMetrics(componentType, telemetry),
    metadata: getComponentMetadata(componentType, hardware),
  };
}
