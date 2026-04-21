import type { Measurement, MetricUnit } from "../types";
import { getDisplayValue } from "@/shared/utils/stringUtils";
import { convertCtoF, convertFtoC, convertGigahashSecToTerahashSec, convertWtoKW } from "@/shared/utils/utility";

/**
 * Convert units intelligently based on unit type detection
 * @param currentValue - The current value with units
 * @param preferredUnits - The desired units to convert to
 * @returns Converted value or original value if conversion not possible
 */
export function convertValueUnits(
  currentValue: Measurement | undefined,
  preferredUnits: MetricUnit,
): Measurement | undefined {
  if (!currentValue?.value || !currentValue.units) {
    return currentValue;
  }

  const fromUnits = normalizeUnits(currentValue.units);
  const toUnits = normalizeUnits(preferredUnits);

  // If units are the same after normalization, no conversion needed
  if (fromUnits === toUnits) {
    return { ...currentValue, units: preferredUnits };
  }

  const unitType = getUnitType(fromUnits);
  const preferredUnitType = getUnitType(toUnits);

  // Only convert if unit types match
  if (unitType !== preferredUnitType) {
    console.error(
      `Unit conversion failed: Cannot convert ${unitType} units (${currentValue.units}) to ${preferredUnitType} units (${preferredUnits}). Unit types must match.`,
    );
    return currentValue; // Return original if types don't match
  }

  const convertedValue = performUnitConversion(
    currentValue.value as number, // We know it's a number at this point
    fromUnits,
    toUnits,
    unitType,
  );

  if (convertedValue !== null) {
    return { value: convertedValue, units: preferredUnits };
  }

  console.error(
    `Unit conversion failed: No conversion available from ${fromUnits} to ${toUnits} for ${unitType} units.`,
  );
  return currentValue; // Return original if conversion failed
}

/**
 * Normalize unit strings to standardized format
 */
function normalizeUnits(units: MetricUnit): string {
  const normalized = units.toLowerCase().replace(/[º°]/g, "");

  // Temperature normalization
  if (normalized === "c") return "c";
  if (normalized === "f") return "f";

  // Power normalization
  if (normalized === "w") return "w";
  if (normalized === "kw") return "kw";
  if (normalized === "mw") return "mw";

  // Hashrate normalization
  if (normalized === "th/s") return "th/s";
  if (normalized === "gh/s") return "gh/s";
  if (normalized === "mh/s") return "mh/s";

  // Efficiency normalization
  if (normalized === "j/th") return "j/th";

  return normalized;
}

/**
 * Detect the type of unit (temperature, power, hashrate, efficiency, etc.)
 */
function getUnitType(units: string): "temperature" | "power" | "hashrate" | "efficiency" | "percentage" | "unknown" {
  switch (units) {
    case "c":
    case "f":
      return "temperature";

    case "w":
    case "kw":
    case "mw":
      return "power";

    case "th/s":
    case "gh/s":
    case "mh/s":
      return "hashrate";

    case "j/th":
      return "efficiency";

    case "%":
      return "percentage";

    default:
      return "unknown";
  }
}

/**
 * Perform the actual unit conversion based on unit type
 */
function performUnitConversion(value: number, fromUnits: string, toUnits: string, unitType: string): number | null {
  if (unitType === "temperature") {
    if (fromUnits === "c" && toUnits === "f") {
      return convertCtoF(value);
    }
    if (fromUnits === "f" && toUnits === "c") {
      return convertFtoC(value);
    }
  }

  if (unitType === "power") {
    // W to kW
    if (fromUnits === "w" && toUnits === "kw") {
      return convertWtoKW(value);
    }
    // kW to W
    if (fromUnits === "kw" && toUnits === "w") {
      return value * 1000;
    }
    // W to MW
    if (fromUnits === "w" && toUnits === "mw") {
      return value / 1000000;
    }
    // MW to W
    if (fromUnits === "mw" && toUnits === "w") {
      return value * 1000000;
    }
    // kW to MW
    if (fromUnits === "kw" && toUnits === "mw") {
      return value / 1000;
    }
    // MW to kW
    if (fromUnits === "mw" && toUnits === "kw") {
      return value * 1000;
    }
  }

  if (unitType === "hashrate") {
    // GH/s to TH/s
    if (fromUnits === "gh/s" && toUnits === "th/s") {
      return convertGigahashSecToTerahashSec(value);
    }
    // TH/s to GH/s
    if (fromUnits === "th/s" && toUnits === "gh/s") {
      return value * 1000;
    }
    // MH/s to GH/s
    if (fromUnits === "mh/s" && toUnits === "gh/s") {
      return value / 1000;
    }
    // GH/s to MH/s
    if (fromUnits === "gh/s" && toUnits === "mh/s") {
      return value * 1000;
    }
    // MH/s to TH/s
    if (fromUnits === "mh/s" && toUnits === "th/s") {
      return value / 1000000;
    }
    // TH/s to MH/s
    if (fromUnits === "th/s" && toUnits === "mh/s") {
      return value * 1000000;
    }
  }

  // No conversion available
  return null;
}

/**
 * combines value and units in consistent manner
 */
export function formatValue(currentValue?: Measurement, displayUnits?: boolean) {
  // if value doesnt exist return undefined so that we can show skeleton bar
  if (currentValue?.value === undefined) {
    return;
  }

  // if value is explicitly null then return "N/A"
  // because we have recieved data from server and null represents
  // wgere db has bo data.
  if (currentValue?.value === null) {
    return "N/A";
  }

  const unitType = currentValue.units && getUnitType(normalizeUnits(currentValue.units));
  const isTemperature = unitType === "temperature";

  return (
    getDisplayValue(currentValue.value) +
    (currentValue.units && displayUnits ? ` ${isTemperature ? "°" : ""}${currentValue.units}` : "")
  );
}

export function convertAndFormatMeasurement(
  measurement: Measurement | undefined,
  preferredUnits: MetricUnit,
  displayUnits: boolean = true,
): string | undefined {
  if (!measurement) return undefined;

  const converted = convertValueUnits(measurement, preferredUnits);
  return converted ? formatValue(converted, displayUnits) : undefined;
}
