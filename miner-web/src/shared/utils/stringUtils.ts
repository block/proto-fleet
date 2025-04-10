// adds a comma separator for every 3 digits
export const addCommas = (value?: number) => {
  return value?.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ",") || value;
};

export const getMacAddressDisplay = (value?: string) => {
  return value?.replace(/\./g, ":");
};

export const separateByCommas = (value: string | number) => {
  const [integer, decimal] = value.toString().split(".");
  const commaSeparatedInteger = integer.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  if (decimal) {
    return `${commaSeparatedInteger}.${decimal}`;
  }
  return commaSeparatedInteger;
};

export const getDisplayValue = (value?: number | string | null) => {
  if (value === undefined || value === null) return;

  const numberValue = +value;

  if (isNaN(numberValue)) return value;

  const twoDecimalPlaces = numberValue.toFixed(2);
  const [integer, decimal] = twoDecimalPlaces.split(".");

  if (decimal === "00") return separateByCommas(integer);
  if (decimal[1] === "0") return `${separateByCommas(integer)}.${decimal[0]}`;

  return separateByCommas(twoDecimalPlaces);
};

export const getDateFromEpoch = (epoch?: number) => {
  if (!epoch) return new Date();
  const seconds = epoch.toString().length === 10;
  return new Date(seconds ? epoch * 1000 : epoch);
};

const getHoursFromEpoch = (epoch: number) => {
  return padLeft(getDateFromEpoch(epoch).getHours(), 2);
};

export const getMinutesFromEpoch = (epoch: number) => {
  return padLeft(getDateFromEpoch(epoch).getMinutes(), 2);
};

const getSecondsFromEpoch = (epoch: number) => {
  return padLeft(getDateFromEpoch(epoch).getSeconds(), 2);
};

export const getTimeFromEpoch = (epoch?: number) => {
  if (!epoch) return "";
  return `${getHoursFromEpoch(epoch)}:${getMinutesFromEpoch(epoch)}:${getSecondsFromEpoch(epoch)}`;
};

export const padLeft = (value: number, length: number) => {
  return value.toString().padStart(length, "0");
};

export const getShortYearFromEpoch = (epoch?: number) => {
  if (!epoch) return "";
  return getDateFromEpoch(epoch).getFullYear().toString().slice(-2);
};

export const getMonthFromEpoch = (epoch?: number) => {
  if (!epoch) return "";
  return getDateFromEpoch(epoch).getMonth() + 1;
};

export const getDayFromEpoch = (epoch?: number) => {
  if (!epoch) return "";
  return getDateFromEpoch(epoch).getDate();
};

export const stripLeadingSlash = (str: string) => {
  return str.startsWith("/") ? str.substring(1) : str;
};

export const convertToSentenceCase = (str: string) => {
  return str
    .split(/[.?!]\s*/) // Split on periods, question marks, or exclamation points followed by optional spaces
    .map((sentence, index, array) => {
      const separator = str.match(/[.?!]\s*/g)?.[index] || ". ";
      return (
        sentence.charAt(0).toUpperCase() +
        sentence.slice(1) +
        (index < array.length - 1 ? separator.trim() : "")
      );
    })
    .join(" ")
    .trim(); // Rejoin sentences with their original separators
};

export const convertToTitleCase = (str: string) => {
  return str
    .split(/[\s_]+/)
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(" ");
};
