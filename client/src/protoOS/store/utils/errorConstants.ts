import type { ErrorSource } from "../types";

export const ErrorLevel = {
  error: "Error",
  warning: "Warning",
} as const;

// Map error code prefixes to their source component
export const ERROR_CODE_TO_SOURCE: Record<string, ErrorSource> = {
  "00": "PSU",
  "01": "FAN",
  "02": "SYSTEM", // IO Module
  "03": "SYSTEM", // Control Board
  "04": "HASHBOARD", // Note: ASIC errors are a subset, handled separately
};

// Map legacy string error codes to numeric codes
export const LEGACY_TO_NUMERIC_ERROR_CODE: Record<string, string> = {
  // Fan errors
  FanSlow: "01:0001",
  FanNotSpinning: "01:0002",

  // PSU errors
  PsuHardwareFault: "00:0011",
  PsuCommsLost: "00:0006",

  // Hashboard errors
  HashboardOverheat: "04:0006",
  HashboardOverVoltage: "04:0007",
  HashboardUnderVoltage: "04:0008",
  HashboardOverCurrent: "04:0009",
  HashboardPowerLost: "04:0011",
  HashboardUsbConnectionLost: "04:0015",

  // ASIC errors
  AsicOverheat: "04:0001",
  AsicOverVoltage: "04:0002",
  AsicUnderVoltage: "04:0003",
  AsicFailure: "04:0005",

  // Control Board errors
  MixedHashboardTypesInBay: "03:0015",
  ControlboardFailure: "03:0007",
  ControlboardIssue: "03:0008",

  // Add firmware name variants as aliases (firmware uses these names)
  AsicOverTemp: "04:0001", // Alias for AsicOverheat
  HashboardOverTemp: "04:0006", // Alias for HashboardOverheat
};

// ASIC-specific error codes
export const ASIC_ERROR_CODES = [
  "04:0001", // AsicOverheat
  "04:0002", // AsicOverVoltage
  "04:0003", // AsicUnderVoltage
  "04:0004", // AsicEcc
  "04:0005", // AsicFailure/AsicEnumeration
  "04:0014", // AsicUnderTemp
  "04:0016", // AsicNotHashing
];

// Pool-specific error codes
export const POOL_ERROR_CODES = [
  "03:0006", // PoolConnectionLost
];

// Map of error codes to detailed message generators
export const errorMessageGenerators: Record<string, (d: any) => string> = {
  // PSU Errors
  "00:0001": () => "Power supply failure to start",
  "00:0002": () => "Power supply overcurrent detected",
  "00:0003": () => "Power supply overpower detected",
  "00:0004": (d) =>
    `Power supply overvoltage detected: ${d?.voltage || "unknown"}V`,
  "00:0005": (d) =>
    `Power supply undervoltage detected: ${d?.voltage || "unknown"}V`,
  "00:0006": (d) =>
    `Communication lost with power supply ${d?.psu_index !== undefined ? `#${d.psu_index}` : ""}`,
  "00:0007": () => "PSU input power error",
  "00:0008": (d) =>
    `Power supply overtemperature: ${d?.temperature || "unknown"}°C`,
  "00:0012": (d) =>
    `Power supply undertemperature: ${d?.temperature || "unknown"}°C`,
  "00:0009": () => "Power supply has inconsistent readings",
  "00:0010": (d) =>
    `Power supply ${d?.psu_index !== undefined ? `#${d.psu_index}` : ""} not detected`,
  "00:0011": (d) => {
    const fault = d?.hardware_fault || "Unknown hardware fault";
    const recovery = d?.recovery_action || "Replace PSU";
    return `PSU fatal error: ${fault}. ${recovery}`;
  },
  "00:0013": (d) =>
    `Power supply ${d?.psu_index !== undefined ? `#${d.psu_index}` : ""} recovering from fault`,

  // Fan Errors
  "01:0001": (d) =>
    `Fan ${d?.fan_bay_index !== undefined ? `#${d.fan_bay_index}` : d?.fan_id !== undefined ? `#${d.fan_id + 1}` : ""} running slow. Target: ${d?.fan_rpm_target || d?.fan_pwm_target_pct || "unknown"}%, Actual: ${d?.fan_rpm_tach !== undefined ? d.fan_rpm_tach : "unknown"} RPM`,
  "01:0002": (d) =>
    `Fan ${d?.fan_bay_index !== undefined ? `#${d.fan_bay_index}` : d?.fan_id !== undefined ? `#${d.fan_id + 1}` : ""} not spinning. Target: ${d?.fan_rpm_target || d?.fan_pwm_target_pct || "unknown"}%`,
  "01:0003": (d) =>
    `Fan ${d?.fan_bay_index !== undefined ? `#${d.fan_bay_index}` : d?.fan_id !== undefined ? `#${d.fan_id + 1}` : ""} detected in immersion cooling mode`,
  "01:0004": (d) => {
    const required = d?.required_fans?.join(", ") || "unknown";
    const failed = d?.failed_fans?.join(", ") || "unknown";
    return `Insufficient cooling. Required fans: [${required}], Failed fans: [${failed}]`;
  },

  // IO Module Errors
  "02:0001": () => "IO module mismatch detected",

  // Control Board/System Errors
  "03:0001": () => "DHCP failure",
  "03:0002": () => "USB enumeration failure",
  "03:0003": (d) =>
    `USB dropouts detected: ${d?.dropout_count || "multiple"} dropouts`,
  "03:0004": () => "Telemetry failure",
  "03:0005": () => "Incorrect MCDD configuration",
  "03:0007": (d) =>
    `Kernel panic: ${d?.panic_message || "System crash detected"}`,
  "03:0008": (d) => `High CPU usage: ${d?.cpu_usage || "unknown"}%`,
  "03:0009": (d) =>
    `Control board overheating: ${d?.temperature || "unknown"}°C`,
  "03:0010": () => "Ethernet link down",
  "03:0013": () => "Unable to connect to Memfault",
  "03:0014": (d) => `Firmware update: ${d?.version || "in progress"}`,
  "03:0016": (d) => `Low hashrate detected: ${d?.hashrate || "unknown"} TH/s`,
  "03:0017": () => "Internal communication failure",

  // Pool Errors
  "03:0006": (d) => `Pool connection lost: ${d?.pool_url || "unknown URL"}`,

  // Hashboard Errors
  "04:0006": (d) =>
    `Hashboard slot ${d?.hb_slot || "?"} overheating: ${d?.temperature || "unknown"}°C`,
  "04:0007": (d) =>
    `Hashboard slot ${d?.hb_slot || "?"} overvoltage: ${d?.voltage || "unknown"}V`,
  "04:0008": (d) =>
    `Hashboard slot ${d?.hb_slot || "?"} undervoltage: ${d?.voltage || "unknown"}V`,
  "04:0009": (d) =>
    `Hashboard slot ${d?.hb_slot || "?"} overcurrent: ${d?.current || "unknown"}A`,
  "04:0011": (d) => `Hashboard slot ${d?.hb_slot || "?"} power lost`,
  "04:0015": (d) => `Hashboard slot ${d?.hb_slot || "?"} USB connection lost`,
  "04:0010": (d) =>
    `Hashboard slot ${d?.hb_slot || "?"} firmware update: ${d?.version || "in progress"}`,
  "04:0012": (d) =>
    `Hashboard slot ${d?.hb_slot || "?"} software error: ${d?.error || "unknown"}`,
  "04:0013": (d) => `Hashboard slot ${d?.hb_slot || "?"} unknown error`,
  "04:0017": (d) =>
    `Hashboard slot ${d?.hb_slot || "?"} recovering from overtemperature`,

  // ASIC Errors
  "04:0001": (d) =>
    `ASIC ${d?.asic_index || "?"} on hashboard ${d?.hb_slot || "?"} overheating: ${d?.temperature || "unknown"}°C`,
  "04:0002": (d) =>
    `ASIC ${d?.asic_index || "?"} on hashboard ${d?.hb_slot || "?"} overvoltage: ${d?.voltage || "unknown"}V`,
  "04:0003": (d) =>
    `ASIC ${d?.asic_index || "?"} on hashboard ${d?.hb_slot || "?"} undervoltage: ${d?.voltage || "unknown"}V`,
  "04:0004": (d) => `ASIC ECC errors on hashboard ${d?.hb_slot || "?"}`,
  "04:0005": (d) => `ASIC enumeration error on hashboard ${d?.hb_slot || "?"}`,
  "04:0014": (d) =>
    `ASIC ${d?.asic_index || "?"} on hashboard ${d?.hb_slot || "?"} undertemperature: ${d?.temperature || "unknown"}°C`,
  "04:0016": (d) => {
    const asics = d?.asics?.join(", ") || "unknown";
    return `ASICs [${asics}] on hashboard ${d?.hb_slot || "?"} not hashing`;
  },

  // System/Control Board Errors
  "03:0015": (d) => {
    const types =
      d?.hashboards?.map((hb: any) => hb.hb_type).join(", ") || "unknown";
    return `Incompatible hashboard types detected in the same bay: ${types}`;
  },
};
