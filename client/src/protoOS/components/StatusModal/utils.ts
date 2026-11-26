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
import { isPlantAlarmError } from "@/protoOS/store/hooks/useMinerStatus";
import {
  projectPsuReadings,
  psuFaultMessage,
  psuFreshness,
  psuHealth,
  psuReadingNotice,
  psuThermalNotice,
} from "@/protoOS/store/utils/psuFreshness";
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
function getComponentMetrics(type: ComponentType, telemetry: ComponentTelemetry, now: number): ComponentMetric[] {
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
        if (psuTelemetry.observedAt) {
          const freshness = psuFreshness(psuTelemetry, now);
          for (const [label, field, group, Value] of [
            ["Input Power", "inputPower", "electrical", PowerValue],
            ["Output Power", "outputPower", "electrical", PowerValue],
            ["PSU inlet", "inletTemperature", "temperatures", TemperatureValue],
            ["SR heatsink", "srHeatsinkTemperature", "temperatures", TemperatureValue],
            ["PFC heatsink", "pfcHeatsinkTemperature", "temperatures", TemperatureValue],
          ] as const) {
            const value = psuTelemetry[field]?.latest?.value;
            metrics.push({
              label,
              value:
                freshness[group] === "stale"
                  ? "Stale"
                  : freshness[group] !== "current" || value == null || !Number.isFinite(value)
                    ? "Unavailable"
                    : createElement(Value, { value }),
            });
          }
          break;
        }
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
      metadata.manufacturer = {
        label: "Vendor",
        value: (hardware as PsuHardware).manufacturer,
      };
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
      metadata.hardwareRevision = {
        label: "Hardware revision",
        value: (hardware as PsuHardware).hardwareRevision,
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
  now = Date.now(),
): ErrorData[] {
  return errors.map((error) => ({
    componentName: error.component ? name(error.component) : isPlantAlarmError(error) ? "Container alarm" : "Miner",
    message: psuFaultMessage(error, now),
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
  now = Date.now(),
): ComponentStatusModalProps {
  const componentType: ComponentType =
    component.kind === "asic" || component.kind === "plant" ? "other" : component.kind;
  const componentErrors = errors.filter(
    (error) =>
      (error.component?.kind === component.kind && error.component.id === component.id) ||
      error.reportedBy?.some(
        (reporter) => reporter.component.kind === component.kind && reporter.component.id === component.id,
      ),
  );
  const psu = component.kind === "psu" && telemetry ? projectPsuReadings(telemetry as PsuTelemetry, now) : undefined;
  const health =
    component.kind === "psu" ? psuHealth(psu, now) : telemetry && "health" in telemetry ? telemetry.health : undefined;
  const freshness = psu?.observedAt ? psuFreshness(psu, now) : undefined;
  const notice = freshness
    ? [
        psuReadingNotice("Electrical readings", freshness.electrical),
        psuReadingNotice("Temperatures", freshness.temperatures),
        psuThermalNotice(telemetry as PsuTelemetry | undefined, now, freshness.thermalState),
      ]
        .filter(Boolean)
        .join(". ")
    : undefined;
  const metadata = getComponentMetadata(componentType, hardware);
  if (psu?.observedAt) {
    for (const field of Object.values(metadata)) field.value = field.value || "Unavailable";
  }
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
    notice,
    errors: transformErrorsForModal(componentErrors, () => name, undefined, now),
    metrics: getComponentMetrics(componentType, psu ?? telemetry, now),
    metadata,
  };
}
