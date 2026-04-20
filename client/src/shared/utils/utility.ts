import type { TemperatureUnit } from "@/shared/features/preferences";
import { getDisplayValue, padLeft } from "@/shared/utils/stringUtils";

export const deepClone = (obj: any) => {
  const stringify = JSON.stringify(obj, (_, value) => (typeof value === "bigint" ? Number(value) : value));
  if (!stringify) {
    return obj;
  }
  return JSON.parse(stringify);
};

export const debounce = (callback: (...args: any) => void, delay: number = 500) => {
  let timeoutId: ReturnType<typeof setTimeout> | undefined;

  const cancel = () => {
    if (timeoutId) {
      clearTimeout(timeoutId);
      timeoutId = undefined;
    }
  };

  const debounced = (...args: any) => {
    const context = this;
    if (timeoutId) clearTimeout(timeoutId);
    timeoutId = setTimeout(() => {
      timeoutId = undefined;
      callback.apply(context, args);
    }, delay);
  };

  debounced.cancel = cancel;
  return debounced;
};

export const getRandomInt = (min: number, max: number) => {
  return Math.floor(Math.random() * (max - min + 1) + min);
};

// precision is used for the number of decimal places, e.g. 100 for 2 decimal places
export const getRandomFloat = (min: number, max: number, precision: number = 100) => {
  return (
    (Math.floor(Math.random() * (max * precision - min * precision) + 1 * precision) + min * precision) /
    (1 * precision)
  );
};

export const convertMegahashSecToTerahashSec = (value?: number | null) => (value ?? 0) / 1000000;
export const convertGigahashSecToTerahashSec = (value?: number | null) => (value ?? 0) / 1000;
export const convertWtoKW = (value?: number | null) => (value ?? 0) / 1000;

// Hashrate unit conversion constants
export const TH_TO_PH_THRESHOLD = 1000;
export const TH_TO_PH_DIVISOR = 1000;

export const formatHashrateWithUnit = (value: number = 0) => {
  if (value > TH_TO_PH_THRESHOLD) {
    return {
      value: value / TH_TO_PH_DIVISOR,
      unit: "PH/S",
    };
  }
  return {
    value: value,
    unit: "TH/S",
  };
};

export const convertCtoF = (value: number = 0) => (value * 9) / 5 + 32;
export const convertFtoC = (value: number = 0) => ((value - 32) * 5) / 9;

export const formatTempRange = (minC: number, maxC: number, temperatureUnit: TemperatureUnit): string => {
  const min = temperatureUnit === "F" ? convertCtoF(minC) : minC;
  const max = temperatureUnit === "F" ? convertCtoF(maxC) : maxC;
  return `${getDisplayValue(min)} °${temperatureUnit} – ${getDisplayValue(max)} °${temperatureUnit}`;
};

export const getAsicTempValue = (avgAsicTemp: number | undefined, isFahrenheit: boolean) => {
  if (!avgAsicTemp) return "N/A"; // TODO: why not return undefined, so we can show skeleton, also 0 cound be falsey
  return isFahrenheit ? convertCtoF(avgAsicTemp) : avgAsicTemp;
};

const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ";

export const getRowLabel = (row: number) => {
  return alphabet.charAt(row);
};

/**
 * Triggers a browser file download from a Blob.
 */
export const downloadBlob = (blob: Blob, filename: string) => {
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  link.style.display = "none";
  document.body.appendChild(link);
  try {
    link.click();
  } finally {
    document.body.removeChild(link);
    setTimeout(() => URL.revokeObjectURL(url), 100);
  }
};

export const getFileName = (prefix: string, fileExtension: string = "csv") => {
  const date = new Date();
  const year = date.getFullYear();
  const month = padLeft(date.getMonth() + 1, 2);
  const day = padLeft(date.getDate(), 2);
  const hours = padLeft(date.getHours(), 2);
  const minutes = padLeft(date.getMinutes(), 2);
  const seconds = padLeft(date.getSeconds(), 2);
  const formattedDate = `${year}-${month}-${day}_${hours}-${minutes}-${seconds}`;
  return `${prefix}-${formattedDate}.${fileExtension}`;
};

export const accessTokenExpiryTime = () => {
  // 30 minutes
  return new Date(new Date().getTime() + 30 * 60 * 1000);
};

export const refreshTokenExpiryTime = () => {
  // 15 days
  return new Date(new Date().getTime() + 15 * 24 * 60 * 60 * 1000);
};

/**
 * Copies text to clipboard with fallback for non-secure contexts.
 * Uses navigator.clipboard.writeText() in secure contexts (HTTPS, localhost),
 * falls back to document.execCommand('copy') for HTTP contexts (e.g., local IP addresses).
 */
export const copyToClipboard = async (text: string): Promise<void> => {
  if (navigator.clipboard && window.isSecureContext) {
    return navigator.clipboard.writeText(text);
  }

  const previouslyFocusedElement = document.activeElement as HTMLElement | null;

  const textArea = document.createElement("textarea");
  textArea.value = text;
  textArea.style.position = "fixed";
  textArea.style.left = "-999999px";
  textArea.style.top = "-999999px";
  textArea.style.opacity = "0";
  document.body.appendChild(textArea);
  textArea.focus();
  textArea.select();

  try {
    const successful = document.execCommand("copy");
    if (!successful) {
      throw new Error("Copy command was unsuccessful");
    }
  } finally {
    document.body.removeChild(textArea);
    if (previouslyFocusedElement && previouslyFocusedElement.focus) {
      previouslyFocusedElement.focus();
    }
  }
};
