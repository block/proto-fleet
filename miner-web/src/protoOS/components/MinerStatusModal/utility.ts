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

export const getErrorTitle = (errors: ErrorListResponse) => {
  let title = "Your miner is not functioning properly";
  if (errors.length === 1) {
    switch (errors[0].error_code) {
      case "AsicOverheat":
        title = "Your miner's ASICs are overheating";
        break;
      case "AsicOverVoltage":
        title = "Your miner's ASIC voltage is excessive";
        break;
      case "AsicFailure":
        title = "Your miner's ASICs are malfunctioning";
        break;
      case "FanSlow":
        title = "Your miner's fan is running slowly";
        break;
      case "HashboardOverCurrent":
        title = "Your miner's hashboard is drawing too much current";
        break;
      case "HashboardOverheat":
        title = "Your miner's hashboard is overheating";
        break;
      case "HashboardOverVoltage":
        title = "Your miner's hashboard voltage is too high";
        break;
      case "HashboardPowerLost":
        title = "Your miner's hashboard has lost power";
        break;
      case "HashboardUnderVoltage":
        title = "Your miner's hashboard voltage is too low";
        break;
      case "HashboardUsbConnectionLost":
        title = "Your miner's hashboard has lost USB connection";
        break;
      case "PoolConnectionLost":
        title = "Your miner has lost connection to the pool";
        break;
      case "NoPoolConfigured":
        title = "No mining pools configured";
        break;
    }
  }
  return title;
};

export const getErrorMessage = (error?: NotificationError) => {
  if (error?.error_code) {
    // split error code by capital letters as a fallback
    let message = error.error_code.match(/[A-Z][a-z]+/g)?.join(" ");
    const details = JSON.parse(error.details || "{}")[error.error_code];
    switch (error.error_code) {
      case "AsicOverheat":
        message = `Port ${details.port} Hashboard's ASIC (${getRowLabel(details.asic_row)}${details.asic_col}) is overheating at ${details.temperature}°C`;
        break;
      case "AsicOverVoltage":
        message = `Port ${details.port} Hashboard's ASIC (${getRowLabel(details.asic_row)}${details.asic_col}) is drawing too much voltage at ${details.voltage}mV`;
        break;
      case "AsicFailure":
        message = `Port ${details.port} Hashboard's ASIC (${getRowLabel(details.asic_row)}${details.asic_col}) experienced an unspecified failure`;
        break;
      case "FanSlow":
        message = `Fan is running slow. Target fan speed: ${details.fan_rpm_target}%, Actual RPM: ${details.fan_rpm_tach}`;
        break;
      case "HashboardOverCurrent":
        message = `Port ${details.port} Hashboard is drawing too much current at ${details.current}A`;
        break;
      case "HashboardOverheat":
        message = `Port ${details.port} Hashboard is overheating at ${details.temperature}°C`;
        break;
      case "HashboardOverVoltage":
        message = `Port ${details.port} Hashboard is drawing too much voltage at ${details.voltage}mV`;
        break;
      case "HashboardPowerLost":
        message = `Port ${details.port} Hashboard has lost power`;
        break;
      case "HashboardUnderVoltage":
        message = `Port ${details.port} Hashboard does not have enough power at ${details.voltage}mV`;
        break;
      case "HashboardUsbConnectionLost":
        message = `Port ${details.port} Hashboard has lost USB connection. Serial number: ${details.serial_number}`;
        break;
      case "PoolConnectionLost":
        message = `Your miner has lost connection to pool ${details.pool_url}`;
        break;
      case "NoPoolConfigured":
        message = "No mining pools configured";
        break;
    }
    return message;
  }
  return error?.message || "Unknown error";
};
