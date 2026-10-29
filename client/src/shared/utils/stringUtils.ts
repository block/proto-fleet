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

  const oneDecimalPlaces = numberValue.toFixed(1);

  return separateByCommas(oneDecimalPlaces);
};

export const padLeft = (value: number, length: number) => {
  return value.toString().padStart(length, "0");
};

/**
 * Formats a power value in kW with adaptive units.
 * Values >= 1 kW are shown in kW (1 decimal place).
 * Values < 1 kW are converted to W (rounded to nearest integer).
 */
export const formatPowerKW = (kw: number): { value: string; unit: string } => {
  if (kw >= 1) {
    return { value: separateByCommas(kw.toFixed(1)), unit: "kW" };
  }
  return { value: separateByCommas((kw * 1000).toFixed(1)), unit: "W" };
};

export const stripLeadingSlash = (str: string) => {
  return str.startsWith("/") ? str.substring(1) : str;
};

export const convertToSentenceCase = (str: string) => {
  return str
    .split(/[.?!]\s*/) // Split on periods, question marks, or exclamation points followed by optional spaces
    .map((sentence, index, array) => {
      const separator = str.match(/[.?!]\s*/g)?.[index] || ". ";
      return sentence.charAt(0).toUpperCase() + sentence.slice(1) + (index < array.length - 1 ? separator.trim() : "");
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
