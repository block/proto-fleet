import { ErrorLevel } from "./constants";
import { ErrorListResponse, NotificationError } from "@/protoOS/api/types";

import { getRowLabel } from "@/shared/utils/utility";

export const isError = (error_level: NotificationError["error_level"]) =>
  error_level === ErrorLevel.error;

export const isWarning = (error_level: NotificationError["error_level"]) =>
  error_level === ErrorLevel.warning;

const isHashboardErrorCode = (error_code: NotificationError["error_code"]) =>
  /hashboard/i.test(error_code || "");

const isAsicErrorCode = (error_code: NotificationError["error_code"]) =>
  /asic/i.test(error_code || "");

const isFanErrorCode = (error_code: NotificationError["error_code"]) =>
  /fan/i.test(error_code || "");

const isPSUErrorCode = (error_code: NotificationError["error_code"]) =>
  /psu/i.test(error_code || "");

const isControlBoardErrorCode = (error_code: NotificationError["error_code"]) =>
  /controlboard/i.test(error_code || "");

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

// Comprehensive title/ subtitle that descibes all errors
// This displayed on the MinerStatusModal and ErrorCallout
export const getStatusErrorTitle = (errors: ErrorListResponse) => {
  let title = "Your miner is not functioning properly";
  let subtitle = "";

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

  // issues on more than one component
  if (errTypes.length > 1) {
    title = "Multiple issues detected";
    subtitle = "Repair now to prevent downtime.";

    // multiple issues on 1 component
  } else if (relevantErrors.length > 1) {
    const component =
      errorsByType[errTypes[0] as keyof typeof errorsByType].name;
    title = `Multiple ${component} issues detected`;
    subtitle = "Repair now to prevent downtime.";

    // exactly one issue
  } else if (relevantErrors.length === 1) {
    switch (relevantErrors[0].error_code) {
      case "AsicOverheat":
        title = "Your miner's ASICs are overheating";
        subtitle =
          "Repair now to prevent reduced hashrate and board shutdowns.";
        break;
      case "AsicOverVoltage":
        title = "Your miner's ASIC voltage is excessive";
        subtitle =
          "Repair now to prevent reduced hashrate and board shutdowns.";
        break;
      case "AsicFailure":
        title = "Your miner's ASICs are malfunctioning";
        subtitle =
          "Repair now to prevent reduced hashrate and board shutdowns.";
        break;
      case "FanSlow":
        title = "Your miner's fan is running slowly";
        subtitle = "Repair now to prevent overheating.";
        break;
      case "HashboardOverCurrent":
        title = "Your miner's hashboard is drawing too much current";
        subtitle = "Repair now to prevent overheating.";
        break;
      case "HashboardOverheat":
        title = "Your miner's hashboard is overheating";
        subtitle =
          "Repair now to prevent reduced hashrate and board shutdowns.";
        break;
      case "HashboardOverVoltage":
        title = "Your miner's hashboard voltage to high";
        subtitle = "Repair now to prevent overheating.";
        break;
      case "HashboardPowerLost":
        title = "Your miner's hashboard has lost power";
        subtitle = "Repair now to prevent downtime.";
        break;
      case "HashboardUnderVoltage":
        title = "Your miner's hashboard voltage is too low";
        subtitle = "Repair now to prevent downtime.";
        break;
      case "HashboardUsbConnectionLost":
        title = "Your miner's hashboard has lost USB connection";
        subtitle = "Repair now to prevent downtime.";
        break;
      case "PsuHardwareFault":
        title = "Your miner's power supply has failed";
        subtitle = "Repair now to prevent downtime.";
        break;
      case "FanNotSpinning":
        title = "Your miner's fan has stopped spinning";
        subtitle = "Repair now to prevent overheating.";
        break;
      case "PsuCommsLost":
        title = "Your miner has lost communication with a power supply";
        subtitle = "Repair now to prevent downtime.";
        break;
      case "MixedHashboardTypesInBay":
        title = "Your miner has incompatible hashboards in the same bay";
        subtitle = "Repair now to prevent downtime.";
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
          const fanNum = fanIssues[0].component_index || "";
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

  const details = JSON.parse(error.details || "{}")[error.error_code];
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
    // split error code by capital letters as a fallback
    let message = error.error_code.match(/[A-Z][a-z]+/g)?.join(" ");
    const details = JSON.parse(error.details || "{}")[error.error_code];
    switch (error.error_code) {
      case "AsicOverheat":
        message = `Slot ${details.hb_slot} Hashboard's ASIC (${getRowLabel(details.asic_row)}${details.asic_col}) is overheating at ${details.temperature}°C`;
        break;
      case "AsicOverVoltage":
        message = `Slot ${details.hb_slot} Hashboard's ASIC (${getRowLabel(details.asic_row)}${details.asic_col}) is drawing too much voltage at ${details.voltage}mV`;
        break;
      case "AsicFailure":
        message = `Slot ${details.hb_slot} Hashboard's ASIC (${getRowLabel(details.asic_row)}${details.asic_col}) experienced an unspecified failure`;
        break;
      case "FanSlow":
        message = `Fan ${details.fan_id} in bay ${details.fan_bay_index} is running slow. Target fan speed: ${details.fan_rpm_target}%, Actual RPM: ${details.fan_rpm_tach}`;
        break;
      case "FanNotSpinning":
        message = `Fan ${details.fan_id} in bay ${details.fan_bay_index} is not spinning. Target fan speed: ${details.fan_rpm_target}%, Actual RPM: ${details.fan_rpm_tach}`;
        break;
      case "HashboardOverCurrent":
        message = `Slot ${details.hb_slot} Hashboard is drawing too much current at ${details.current}A`;
        break;
      case "HashboardOverheat":
        message = `Slot ${details.hb_slot} Hashboard is overheating at ${details.temperature}°C`;
        break;
      case "HashboardOverVoltage":
        message = `Slot ${details.hb_slot} Hashboard is drawing too much voltage at ${details.voltage}mV`;
        break;
      case "HashboardPowerLost":
        message = `Slot ${details.hb_slot} Hashboard has lost power`;
        break;
      case "HashboardUnderVoltage":
        message = `Slot ${details.hb_slot} Hashboard does not have enough power at ${details.voltage}mV`;
        break;
      case "HashboardUsbConnectionLost":
        message = `Slot ${details.hb_slot} Hashboard has lost USB connection. Serial number: ${details.hb_sn}`;
        break;
      case "PsuHardwareFault":
        message = `Power supply in bay ${details.psu_bay_index} (ID: ${details.psu_index}) has a hardware fault: ${details.fault?.message ?? details.fault?.fault_type}. Serial number: ${details.psu_sn}`;
        break;
      case "PsuCommsLost":
        message = `Communication lost with power supply in bay ${details.psu_bay_index} (ID: ${details.psu_index}). Serial number: ${details.psu_sn}`;
        break;
      case "MixedHashboardTypesInBay": {
        const hashboardTypes = (
          details.hashboards?.map((hb: { hb_type: string }) => hb.hb_type) ?? []
        ).join(", ");
        message = `Incompatible hashboard types detected in the same bay: ${hashboardTypes}`;
        break;
      }
    }
    return message;
  }
  return error?.message || "Unknown error";
};
