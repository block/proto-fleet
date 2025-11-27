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
