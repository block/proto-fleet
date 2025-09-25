import { padLeft } from "@/shared/utils/stringUtils";

export const deepClone = (obj: any) => {
  const stringify = JSON.stringify(obj, (_, value) =>
    typeof value === "bigint" ? Number(value) : value,
  );
  if (!stringify) {
    return obj;
  }
  return JSON.parse(stringify);
};

export const debounce = (
  callback: (...args: any) => void,
  delay: number = 500,
) => {
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
export const getRandomFloat = (
  min: number,
  max: number,
  precision: number = 100,
) => {
  return (
    (Math.floor(
      Math.random() * (max * precision - min * precision) + 1 * precision,
    ) +
      min * precision) /
    (1 * precision)
  );
};

export const convertMegahashSecToTerahashSec = (value: number = 0) =>
  value / 1000000;
export const convertGigahashSecToTerahashSec = (value: number = 0) =>
  value / 1000;
export const convertWtoKW = (value: number = 0) => value / 1000;

export const formatHashrateWithUnit = (value: number = 0) => {
  if (value > 1000) {
    return {
      value: value / 1000,
      unit: "PH/S",
    };
  }
  return {
    value: value,
    unit: "TH/S",
  };
};

export const convertCtoF = (value: number = 0) => (value * 9) / 5 + 32;

export const getAsicTempValue = (
  avgAsicTemp: number | undefined,
  isFahrenheit: boolean,
) => {
  if (!avgAsicTemp) return "N/A";
  return isFahrenheit ? convertCtoF(avgAsicTemp) : avgAsicTemp;
};

const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ";

export const getRowLabel = (row: number) => {
  return alphabet.charAt(row);
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
