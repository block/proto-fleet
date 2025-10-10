import { ErrorLevel } from "./constants";
import {
  ErrorListResponse,
  NotificationError,
} from "@/protoOS/api/generatedApi";

const ERROR_CODE = {
  PSU_FAILURE_TO_START: {
    code: "00:0001",
    description: () => "Failure to start",
  },
  PSU_OVERCURRENT: { code: "00:0002", description: () => "Overcurrent" },
  PSU_OVERPOWER: { code: "00:0003", description: () => "Overpower" },
  PSU_OVERVOLTAGE: { code: "00:0004", description: () => "Overvoltage" },
  PSU_UNDERVOLTAGE: { code: "00:0005", description: () => "Undervoltage" },
  PSU_COMMUNICATION_ERROR: {
    code: "00:0006",
    description: (details: any) =>
      `Communication lost with power supply in bay ${details?.psu_bay_index} (ID: ${details?.psu_index}). Serial number: ${details?.psu_sn}`,
  },
  PSU_INPUT_POWER_ERROR: {
    code: "00:0007",
    description: () => "PSU Input Power Error",
  },
  PSU_OVERTEMPERATURE: {
    code: "00:0008",
    description: () => "Overtemperature",
  },
  PSU_INCONSISTENT_READINGS: {
    code: "00:0009",
    description: () => "Inconsistent Readings",
  },
  PSU_NOT_DETECTED: { code: "00:0010", description: () => "Not Detected" },
  PSU_FATAL_ERROR: {
    code: "00:0011",
    description: (details: any) =>
      `Power supply in bay ${details?.psu_bay_index} (ID: ${details?.psu_index}) has a hardware fault: ${details?.fault?.message ?? details?.fault?.fault_type}. Serial number: ${details?.psu_sn}`,
  },
  PSU_UNDERTEMPERATURE: {
    code: "00:0012",
    description: () => "Undertemperature",
  },
  PSU_RECOVERY: {
    code: "00:0014",
    description: (details: any) =>
      `Power supply in bay ${details?.psu_bay_index} (ID: ${details?.psu_index}) is recovering from overtemperature. Serial number: ${details?.psu_sn}`,
  },

  FAN_SLOW: {
    code: "01:0001",
    description: (details: any) =>
      `Fan ${details?.fan_id} in bay ${details?.fan_bay_index} is running slow. Target fan speed: ${details?.fan_pwm_target_pct}%, Actual RPM: ${details?.fan_rpm_tach}`,
  },
  FAN_NOT_SPINNING: {
    code: "01:0002",
    description: (details: any) =>
      `Fan ${details?.fan_id} in bay ${details?.fan_bay_index} is not spinning. Target fan speed: ${details?.fan_pwm_target_pct}%, Actual RPM: ${details?.fan_rpm_tach}`,
  },
  FAN_IMMERSION_MODE: {
    code: "01:0003",
    description: (details: any) =>
      `Fan ${details?.fan_id} in bay ${details?.fan_bay_index} is connected in immersion mode`,
  },
  INSUFFICIENT_COOLING: {
    code: "01:0004",
    description: (details: any) => {
      const requiredFans = details?.required_fans || [];
      const failedFans = details?.failed_fans || [];
      const requiredText =
        requiredFans.length === 1
          ? `Required fan: ${requiredFans[0]}`
          : `Required fans: [${requiredFans.join(", ")}]`;
      const failedText =
        failedFans.length === 1
          ? `Failed fan: ${failedFans[0]}`
          : `Failed fans: [${failedFans.join(", ")}]`;
      return `Bay ${details?.bay_index} has insufficient cooling. ${requiredText}. ${failedText}`;
    },
  },

  IO_MODULE_MISMATCH: {
    code: "02:0001",
    description: () => "Mismatch IO module",
  },

  CB_DHCP_FAILURE: { code: "03:0001", description: () => "DHCP failure" },
  CB_USB_ENUMERATION_FAILURE: {
    code: "03:0002",
    description: () => "USB enumeration failure",
  },
  CB_USB_DROPOUTS: { code: "03:0003", description: () => "USB dropouts" },
  CB_TELEMETRY_FAILURE: {
    code: "03:0004",
    description: () => "Telemetry failure",
  },
  CB_INCORRECT_MCDD_CONFIG: {
    code: "03:0005",
    description: () => "Incorrect MCDD config",
  },
  CB_POOL_PROTOCOL_FAILURE: {
    code: "03:0006",
    description: () => "Pool protocol failure",
  },
  CB_KERNEL_PANIC: { code: "03:0007", description: () => "Kernel panic/crash" },
  CB_HIGH_CPU_USAGE: { code: "03:0008", description: () => "High CPU usage" },
  CB_OVERHEATING: { code: "03:0009", description: () => "Overheating" },
  CB_ETHERNET_LINK_DOWN: {
    code: "03:0010",
    description: () => "Ethernet link down",
  },
  CB_UNABLE_TO_CONNECT_MEMFAULT: {
    code: "03:0013",
    description: () => "Unable to connect to memfault",
  },
  CB_FIRMWARE_UPDATE: { code: "03:0014", description: () => "Firmware update" },
  CB_UNSUPPORTED_HASHBOARD_CONFIG: {
    code: "03:0015",
    description: (details: any) => {
      const hashboardTypes = (
        details?.hashboards?.map((hb: { hb_type: string }) => hb.hb_type) ?? []
      ).join(", ");
      return `Incompatible hashboard types detected in the same bay: ${hashboardTypes}`;
    },
  },
  CB_LOW_HASHRATE: { code: "03:0016", description: () => "Low Hashrate" },
  CB_INTERNAL_COMMUNICATION_FAILURE: {
    code: "03:0017",
    description: () => "Internal Communication Failure",
  },

  ASIC_OVERHEATING: {
    code: "04:0001",
    description: (details: any) =>
      `Slot ${details?.hb_slot} Hashboard's ASIC (${details?.asic_index}) is overheating at ${details?.temperature}°C`,
  },
  ASIC_OVERVOLTAGE: {
    code: "04:0002",
    description: (details: any) =>
      `Slot ${details?.hb_slot} Hashboard's ASIC (${details?.asic_index}) is drawing too much voltage at ${details?.voltage}V`,
  },
  ASIC_UNDERVOLTAGE: {
    code: "04:0003",
    description: () => "ASIC under voltage",
  },
  ASIC_ECC_ERRORS: { code: "04:0004", description: () => "ASIC ECC errors" },
  ASIC_ENUMERATION_ERROR: {
    code: "04:0005",
    description: (details: any) =>
      `Slot ${details?.hb_slot} Hashboard's ASIC (${details?.asic_index}) experienced an unspecified failure`,
  },
  ASIC_UNDERTEMPERATURE: {
    code: "04:0014",
    description: (details: any) =>
      `Slot ${details?.hb_slot} Hashboard's ASIC (${details?.asic_index}) is outside of operating temperature, ambient too cold at ${details?.temperature}°C`,
  },
  ASIC_NOT_MINING: {
    code: "04:0016",
    description: (details: any) => {
      const asics = details?.asics || [];
      const asicList =
        asics.length > 1
          ? `ASICs [${asics.join(", ")}]`
          : `ASIC ${asics[0] || ""}`;
      return `Slot ${details?.hb_slot} Hashboard's ${asicList} ${asics.length > 1 ? "are" : "is"} not mining. Serial number: ${details?.hb_sn}`;
    },
  },

  HB_OVERHEATING: {
    code: "04:0006",
    description: (details: any) =>
      `Slot ${details?.hb_slot} Hashboard is overheating at ${details?.temperature}°C`,
  },
  HB_OVERVOLTAGE: {
    code: "04:0007",
    description: (details: any) =>
      `Slot ${details?.hb_slot} Hashboard is drawing too much voltage at ${details?.voltage}V`,
  },
  HB_UNDERVOLTAGE: {
    code: "04:0008",
    description: (details: any) =>
      `Slot ${details?.hb_slot} Hashboard does not have enough power at ${details?.voltage}V`,
  },
  HB_OVERCURRENT: {
    code: "04:0009",
    description: (details: any) =>
      `Slot ${details?.hb_slot} Hashboard is drawing too much current at ${details?.current}A`,
  },
  HB_FIRMWARE_UPDATE: { code: "04:0010", description: () => "Firmware update" },
  HB_SOFTWARE_ERROR: { code: "04:0012", description: () => "Software error" },
  HB_UNKNOWN_ERROR: { code: "04:0013", description: () => "Unknown error" },
  HB_POWER_LOST: {
    code: "04:0011",
    description: (details: any) =>
      `Slot ${details?.hb_slot} Hashboard has lost power`,
  },
  HB_USB_ERROR: {
    code: "04:0015",
    description: (details: any) =>
      `Slot ${details?.hb_slot} Hashboard has lost USB connection. Serial number: ${details?.hb_sn}`,
  },
  HB_RECOVERY: {
    code: "04:0017",
    description: (details: any) =>
      `Slot ${details?.hb_slot} Hashboard is recovering from overtemperature. Serial number: ${details?.hb_sn}`,
  },
} as const;

const ERROR_CODE_DESCRIPTIONS = new Map<string, (details?: any) => string>(
  Object.values(ERROR_CODE).map((entry) => [entry.code, entry.description]),
);

const COMPONENT_TYPES = {
  PSU: "00",
  FAN: "01",
  IO_MODULE: "02",
  CONTROL_BOARD: "03",
  HASHBOARD: "04",
} as const;

// ASIC error codes (subset of hashboard errors)
const ASIC_ERROR_CODES = [
  ERROR_CODE.ASIC_OVERHEATING.code,
  ERROR_CODE.ASIC_OVERVOLTAGE.code,
  ERROR_CODE.ASIC_UNDERVOLTAGE.code,
  ERROR_CODE.ASIC_ECC_ERRORS.code,
  ERROR_CODE.ASIC_ENUMERATION_ERROR.code,
  ERROR_CODE.ASIC_UNDERTEMPERATURE.code,
  ERROR_CODE.ASIC_NOT_MINING.code,
] as const;

const isStandardErrorCode = (errorCode: string): boolean => {
  return /^\d{2}:\d{4}$/.test(errorCode);
};

const normalizeErrorCode = (errorCode: string): string => {
  if (isStandardErrorCode(errorCode)) {
    return errorCode;
  }
  return (
    LEGACY_ERROR_CODES_TO_ERRORS[
      errorCode as keyof typeof LEGACY_ERROR_CODES_TO_ERRORS
    ]?.code || errorCode
  );
};

const getComponentType = (errorCode: string): string => {
  const normalized = normalizeErrorCode(errorCode);
  return normalized.split(":")[0] || "";
};

const LEGACY_ERROR_CODES_TO_ERRORS = {
  AsicOverheat: ERROR_CODE.ASIC_OVERHEATING,
  AsicOverVoltage: ERROR_CODE.ASIC_OVERVOLTAGE,
  AsicUnderVoltage: ERROR_CODE.ASIC_UNDERVOLTAGE,
  AsicFailure: ERROR_CODE.ASIC_ENUMERATION_ERROR,
  HashboardOverheat: ERROR_CODE.HB_OVERHEATING,
  HashboardOverVoltage: ERROR_CODE.HB_OVERVOLTAGE,
  HashboardUnderVoltage: ERROR_CODE.HB_UNDERVOLTAGE,
  HashboardOverCurrent: ERROR_CODE.HB_OVERCURRENT,
  HashboardPowerLost: ERROR_CODE.HB_POWER_LOST,
  HashboardUsbConnectionLost: ERROR_CODE.HB_USB_ERROR,

  FanSlow: ERROR_CODE.FAN_SLOW,
  FanNotSpinning: ERROR_CODE.FAN_NOT_SPINNING,

  PsuHardwareFault: ERROR_CODE.PSU_FATAL_ERROR,
  PsuCommsLost: ERROR_CODE.PSU_COMMUNICATION_ERROR,

  MixedHashboardTypesInBay: ERROR_CODE.CB_UNSUPPORTED_HASHBOARD_CONFIG,
  ControlboardFailure: ERROR_CODE.CB_KERNEL_PANIC,
  ControlboardIssue: ERROR_CODE.CB_HIGH_CPU_USAGE,
} as const;

const getErrorDescription = (errorCode: string, details?: any): string => {
  const descriptionFn = ERROR_CODE_DESCRIPTIONS.get(errorCode);
  if (descriptionFn) {
    return descriptionFn(details);
  }
  return "Unknown error";
};

export const isError = (error_level: NotificationError["error_level"]) =>
  error_level === ErrorLevel.error;

export const isWarning = (error_level: NotificationError["error_level"]) =>
  error_level === ErrorLevel.warning;

const isHashboardErrorCode = (error_code: NotificationError["error_code"]) => {
  if (!error_code) return false;
  const componentType = getComponentType(error_code);
  return componentType === COMPONENT_TYPES.HASHBOARD;
};

const isAsicErrorCode = (error_code: NotificationError["error_code"]) => {
  if (!error_code) return false;
  const normalized = normalizeErrorCode(error_code);
  return ASIC_ERROR_CODES.includes(normalized as any);
};

const isFanErrorCode = (error_code: NotificationError["error_code"]) => {
  if (!error_code) return false;
  const componentType = getComponentType(error_code);
  return componentType === COMPONENT_TYPES.FAN;
};

const isPSUErrorCode = (error_code: NotificationError["error_code"]) => {
  if (!error_code) return false;
  const componentType = getComponentType(error_code);
  return componentType === COMPONENT_TYPES.PSU;
};

const isControlBoardErrorCode = (
  error_code: NotificationError["error_code"],
) => {
  if (!error_code) return false;
  const componentType = getComponentType(error_code);
  return (
    componentType === COMPONENT_TYPES.CONTROL_BOARD ||
    componentType === COMPONENT_TYPES.IO_MODULE
  );
};

export const isHashboardError = (error: NotificationError) =>
  isHashboardErrorCode(error.error_code) && isError(error.error_level);

export const isHashboardWarning = (error: NotificationError) =>
  isHashboardErrorCode(error.error_code) && isWarning(error.error_level);

export const isAsicError = (error: NotificationError) =>
  isAsicErrorCode(error.error_code) && isError(error.error_level);

export const isAsicWarning = (error: NotificationError) =>
  isAsicErrorCode(error.error_code) && isWarning(error.error_level);

export const isFanError = (error: NotificationError) =>
  isFanErrorCode(error.error_code) && isError(error.error_level);

export const isFanWarning = (error: NotificationError) =>
  isFanErrorCode(error.error_code) && isWarning(error.error_level);

export const isPSUWarning = (error: NotificationError) =>
  isPSUErrorCode(error.error_code) && isWarning(error.error_level);

export const isPSUError = (error: NotificationError) =>
  isPSUErrorCode(error.error_code) && isError(error.error_level);

export const isControlBoardWarning = (error: NotificationError) =>
  isControlBoardErrorCode(error.error_code) && isWarning(error.error_level);

export const isControlBoardError = (error: NotificationError) =>
  isControlBoardErrorCode(error.error_code) && isError(error.error_level);

export const getStatusErrorTitle = (errors: ErrorListResponse) => {
  const errorsByType = {
    hashboard: { errors: errors.filter(isHashboardError), name: "hashboard" },
    psu: { errors: errors.filter(isPSUError), name: "PSU" },
    fan: { errors: errors.filter(isFanError), name: "fan" },
    controlBoard: {
      errors: errors.filter(isControlBoardError),
      name: "Control board",
    },
  };

  const relevantErrors = Object.values(errorsByType).flatMap(
    (category) => category.errors,
  );

  const errTypes = Object.keys(errorsByType).filter(
    (key) => errorsByType[key as keyof typeof errorsByType].errors.length > 0,
  );

  let title = "Your miner is not functioning properly";
  let subtitle = "";

  // issues on more than one component
  if (errTypes.length === 0) {
    title = "All systems are operational";
    subtitle = "";
  } else if (errTypes.length > 1) {
    title = "Multiple issues detected";
    subtitle = "Repair now to prevent downtime.";

    // multiple issues on 1 component
  } else if (relevantErrors.length > 1) {
    const component =
      errorsByType[errTypes[0] as keyof typeof errorsByType]?.name;
    title = `Multiple ${component} issues detected`;
    subtitle = "Repair now to prevent downtime.";

    // exactly one issue
  } else if (relevantErrors.length === 1) {
    const errorCode = relevantErrors[0].error_code;
    const normalizedCode = normalizeErrorCode(errorCode || "");

    switch (normalizedCode) {
      // ASIC Errors
      case ERROR_CODE.ASIC_OVERHEATING.code:
        title = "Your miner's ASICs are overheating";
        subtitle =
          "Repair now to prevent reduced hashrate and board shutdowns.";
        break;
      case ERROR_CODE.ASIC_OVERVOLTAGE.code:
        title = "Your miner's ASIC voltage is excessive";
        subtitle =
          "Repair now to prevent reduced hashrate and board shutdowns.";
        break;
      case ERROR_CODE.ASIC_UNDERVOLTAGE.code:
        title = "Your miner's ASIC voltage is too low";
        subtitle =
          "Repair now to prevent reduced hashrate and board shutdowns.";
        break;
      case ERROR_CODE.ASIC_ECC_ERRORS.code:
        title = "Your miner's ASICs have ECC errors";
        subtitle =
          "Repair now to prevent reduced hashrate and board shutdowns.";
        break;
      case ERROR_CODE.ASIC_ENUMERATION_ERROR.code:
        title = "Your miner's ASICs are malfunctioning";
        subtitle =
          "Repair now to prevent reduced hashrate and board shutdowns.";
        break;
      case ERROR_CODE.ASIC_UNDERTEMPERATURE.code:
        title = "Your miner's ASICs are too cold";
        subtitle =
          "Check environment conditions to prevent performance issues.";
        break;
      case ERROR_CODE.ASIC_NOT_MINING.code:
        title = "Your miner's ASICs are not mining";
        subtitle =
          "Repair now to prevent reduced hashrate and board shutdowns.";
        break;

      // Hashboard Errors
      case ERROR_CODE.HB_OVERHEATING.code:
        title = "Your miner's hashboard is overheating";
        subtitle =
          "Repair now to prevent reduced hashrate and board shutdowns.";
        break;
      case ERROR_CODE.HB_OVERVOLTAGE.code:
        title = "Your miner's hashboard voltage is too high";
        subtitle = "Repair now to prevent overheating.";
        break;
      case ERROR_CODE.HB_UNDERVOLTAGE.code:
        title = "Your miner's hashboard voltage is too low";
        subtitle = "Repair now to prevent downtime.";
        break;
      case ERROR_CODE.HB_OVERCURRENT.code:
        title = "Your miner's hashboard is drawing too much current";
        subtitle = "Repair now to prevent overheating.";
        break;
      case ERROR_CODE.HB_USB_ERROR.code:
        title = "Your miner's hashboard has lost USB connection";
        subtitle = "Repair now to prevent downtime.";
        break;
      case ERROR_CODE.HB_FIRMWARE_UPDATE.code:
      case ERROR_CODE.HB_SOFTWARE_ERROR.code:
      case ERROR_CODE.HB_UNKNOWN_ERROR.code:
        title = "Your miner's hashboard has an error";
        subtitle = "Repair now to prevent downtime.";
        break;
      case ERROR_CODE.HB_POWER_LOST.code:
        title = "Your miner's hashboard has lost power";
        subtitle = "Repair now to prevent downtime.";
        break;
      case ERROR_CODE.HB_RECOVERY.code:
        title = "Your miner's hashboard is recovering";
        subtitle =
          "Repair now to prevent reduced hashrate and board shutdowns.";
        break;

      // Fan Errors
      case ERROR_CODE.FAN_SLOW.code:
        title = "Your miner's fan is running slowly";
        subtitle = "Repair now to prevent overheating.";
        break;
      case ERROR_CODE.FAN_NOT_SPINNING.code:
        title = "Your miner's fan has stopped spinning";
        subtitle = "Repair now to prevent overheating.";
        break;
      case ERROR_CODE.FAN_IMMERSION_MODE.code:
        title = "Your miner's fan is connected in immersion mode";
        subtitle = "Check cooling configuration.";
        break;
      case ERROR_CODE.INSUFFICIENT_COOLING.code:
        title = "Your miner has insufficient cooling";
        subtitle = "Repair now to prevent overheating.";
        break;

      // PSU Errors
      case ERROR_CODE.PSU_FAILURE_TO_START.code:
        title = "Your miner's power supply failed to start";
        subtitle = "Repair now to prevent downtime.";
        break;
      case ERROR_CODE.PSU_OVERCURRENT.code:
        title = "Your miner's power supply has an overcurrent fault";
        subtitle = "Repair now to prevent downtime.";
        break;
      case ERROR_CODE.PSU_OVERPOWER.code:
        title = "Your miner's power supply has an overpower fault";
        subtitle = "Repair now to prevent downtime.";
        break;
      case ERROR_CODE.PSU_OVERVOLTAGE.code:
        title = "Your miner's power supply has an overvoltage fault";
        subtitle = "Repair now to prevent downtime.";
        break;
      case ERROR_CODE.PSU_UNDERVOLTAGE.code:
        title = "Your miner's power supply has an undervoltage fault";
        subtitle = "Repair now to prevent downtime.";
        break;
      case ERROR_CODE.PSU_COMMUNICATION_ERROR.code:
        title = "Your miner has lost communication with a power supply";
        subtitle = "Repair now to prevent downtime.";
        break;
      case ERROR_CODE.PSU_INPUT_POWER_ERROR.code:
        title = "Your miner's power supply has an input power error";
        subtitle = "Repair now to prevent downtime.";
        break;
      case ERROR_CODE.PSU_OVERTEMPERATURE.code:
        title = "Your miner's power supply is overheating";
        subtitle = "Repair now to prevent downtime.";
        break;
      case ERROR_CODE.PSU_INCONSISTENT_READINGS.code:
        title = "Your miner's power supply has inconsistent readings";
        subtitle = "Repair now to prevent downtime.";
        break;
      case ERROR_CODE.PSU_NOT_DETECTED.code:
        title = "Your miner's power supply is not detected";
        subtitle = "Repair now to prevent downtime.";
        break;
      case ERROR_CODE.PSU_FATAL_ERROR.code:
        title = "Your miner's power supply has failed";
        subtitle = "Repair now to prevent downtime.";
        break;
      case ERROR_CODE.PSU_UNDERTEMPERATURE.code:
        title = "Your miner's power supply is too cold";
        subtitle = "Check environment conditions.";
        break;
      case ERROR_CODE.PSU_RECOVERY.code:
        title = "Your miner's power supply is recovering";
        subtitle = "Hashboards in affected bay may be impacted.";
        break;

      // Control Board Errors
      case ERROR_CODE.CB_DHCP_FAILURE.code:
        title = "Your miner has DHCP network issues";
        subtitle = "Check network configuration.";
        break;
      case ERROR_CODE.CB_POOL_PROTOCOL_FAILURE.code:
        title = "Your miner has pool connection issues";
        subtitle = "Check pool configuration.";
        break;
      case ERROR_CODE.CB_OVERHEATING.code:
        title = "Your miner's control board is overheating";
        subtitle = "Repair now to prevent shutdowns.";
        break;
      case ERROR_CODE.CB_LOW_HASHRATE.code:
        title = "Your miner's hashrate is too low";
        subtitle = "Check hardware and pool connection.";
        break;
      case ERROR_CODE.CB_UNSUPPORTED_HASHBOARD_CONFIG.code:
        title = "Your miner has incompatible hashboards in the same bay";
        subtitle = "Repair now to prevent downtime.";
        break;
      case ERROR_CODE.IO_MODULE_MISMATCH.code:
        title = "Your miner has incompatible IO modules";
        subtitle = "Repair now to prevent downtime.";
        break;
      default:
        title = "Your miner has an unknown error";
        subtitle = "Contact support for assistance.";
        break;
    }
  }

  return { title, subtitle };
};

// This the more condensed version of the Status Title that gets displayed in the PageHeader
export const getStatusSummary = (
  hashboardIssues: NotificationError[],
  psuIssues: NotificationError[],
  fanIssues: NotificationError[],
  controlBoardIssues: NotificationError[],
) => {
  const issueTypes = [
    hashboardIssues,
    psuIssues,
    fanIssues,
    controlBoardIssues,
  ].filter((errs) => errs.length > 0).length;

  switch (issueTypes) {
    case 0:
      return null;
    case 1:
      switch (true) {
        case hashboardIssues.length > 1:
          return "Multiple hashboard Issues";
        case hashboardIssues.length === 1: {
          const hashboardNum = hashboardIssues[0].hashboard_index || "";
          return `Hashboard ${hashboardNum} issue`;
        }
        case fanIssues.length > 1:
          return "Multiple fan Issues";
        case fanIssues.length === 1: {
          const [{ error_code: errorCode = "", details: rawDetails = "{}" }] =
            fanIssues;
          const fanNum = safeParseJSON(rawDetails)[errorCode]?.fan_id ?? "";
          return `Fan ${fanNum} issue`;
        }
        case psuIssues.length > 1:
          return "Multiple psu Issues";
        case psuIssues.length === 1: {
          const psuNum = psuIssues[0].component_index || "";
          return `PSU ${psuNum} issue`;
        }
        case controlBoardIssues.length > 1:
          return "Multiple control board Issues";
        case controlBoardIssues.length === 1:
          return "Control board issue";
        default:
          return null;
      }
    default:
      return "Multiple Issues";
  }
};

// This displayed on individual issues in the MinerStatusModal
export const getErrorTitle = (error: NotificationError) => {
  if (!error?.error_code) {
    return error?.message || "Unknown error";
  }

  const details = getErrorDetails(error);
  if (isAsicError(error) || isHashboardError(error)) {
    return `Hashboard ${details.hb_slot || ""}`;
  } else if (isFanError(error)) {
    return `Fan ${details.fan_id || ""}`;
  } else if (isPSUError(error)) {
    return `PSU ${details.psu_bay_index || ""}`;
  } else if (isControlBoardError(error)) {
    return "Control Board";
  } else {
    return error?.message || "Unknown error";
  }
};

// This displayed on individual issues in the MinerStatusModal
export const getErrorMessage = (error?: NotificationError) => {
  if (error?.error_code) {
    const normalizedCode = normalizeErrorCode(error.error_code);
    const details = getErrorDetails(error);

    if (isStandardErrorCode(normalizedCode)) {
      return getErrorDescription(normalizedCode, details);
    }
  }
  return error?.message || "Unknown error";
};

function safeParseJSON(str?: string) {
  try {
    return str ? JSON.parse(str) : {};
  } catch {
    return {};
  }
}

function getErrorDetails(error: NotificationError) {
  const parsedDetails = safeParseJSON(error.details);

  if (error.error_code && parsedDetails[error.error_code]) {
    return parsedDetails[error.error_code];
  }

  // Fallback: return the first available details object
  const keys = Object.keys(parsedDetails);
  return keys.length > 0 ? parsedDetails[keys[0]] : {};
}
