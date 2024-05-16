// adds a comma separator for every 3 digits
export const addCommas = (value?: number) => {
  return value?.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ",") || value;
};

export const getMacAddressDisplay = (value?: string) => {
  return value?.replace(/\./g, ":");
};

// value is a string in the format of "HH:MM"
export const getStandardTime = (value: string) => {
  const time = value.split(":");
  if (time.length !== 2) return value;

  const timeHours = Number(time[0]);
  const timeMinutes = Number(time[1]);

  const standardHour = timeHours > 12 ? timeHours - 12 : timeHours;
  let hours = timeHours === 0 ? 12 : standardHour;
  const minutes = `0${timeMinutes}`.slice(-2);
  const dayNightIndicator = timeHours >= 12 ? "PM" : "AM";

  return `${hours}:${minutes} ${dayNightIndicator}`;
};

export const separateByCommas = (value: string) => {
  const [integer, decimal] = value.split(".");
  const commaSeparatedInteger = integer.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  if (decimal) {
    return `${commaSeparatedInteger}.${decimal}`;
  }
  return commaSeparatedInteger;
};

export const getDisplayValue = (value?: number | string | null) => {
  if (value === undefined || value === null) return;

  const numberValue = +value;
  if (typeof numberValue !== "number") return value;

  const twoDecimalPlaces = numberValue.toFixed(2);
  const [integer, decimal] = twoDecimalPlaces.split(".");

  if (decimal === "00") return separateByCommas(integer);
  if (decimal[1] === "0") return `${separateByCommas(integer)}.${decimal[0]}`;

  return separateByCommas(twoDecimalPlaces);
};

const getDateFromEpoch = (epoch: number) => {
  const seconds = epoch.toString().length === 10;
  return new Date(seconds ? epoch * 1000 : epoch);
};

const getHoursFromEpoch = (epoch: number) => {
  return `0${getDateFromEpoch(epoch).getHours()}`.slice(-2);
};

const getMinutesFromEpoch = (epoch: number) => {
  return `${getDateFromEpoch(epoch).getMinutes()}0`.slice(0, 2);
};

const getSecondsFromEpoch = (epoch: number) => {
  return `${getDateFromEpoch(epoch).getSeconds()}0`.slice(0, 2);
};

export const getTimeFromEpoch = (epoch?: number) => {
  if (!epoch) return "";
  return `${getHoursFromEpoch(epoch)}:${getMinutesFromEpoch(epoch)}:${getSecondsFromEpoch(epoch)}`;
};
