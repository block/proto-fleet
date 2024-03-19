// adds a comma separator for every 3 digits
export const addCommas = (value?: number) => {
  return value?.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ",") || value;
};

// first match for stratum v1 url formats from :// to :
// second match for stratum v2 url formats from :// to /
// else return the original value
export const getPoolUrlDisplay = (value?: string) => {
  return (
    value?.match(/:\/\/(.*):/)?.[1] || value?.match(/:\/\/(.*)\//)?.[1] || value
  );
};

// add a space between each 4 characters
const addSpacing = (value?: string) => {
  return value?.match(/.{1,4}/g)?.join(" ");
};

export const getSerialNumbersDisplay = (value?: string[]) => {
  return value?.map((item) => addSpacing(item));
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
