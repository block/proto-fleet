// adds a comma separator for every 3 digits
export const addCommas = (value?: number) => {
  return value?.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ",") || value;
}

// first match for stratum v1 url formats from :// to :
// second match for stratum v2 url formats from :// to /
// else return the original value
export const getPoolUrlDisplay = (value?: string) => {
  return value?.match(/:\/\/(.*):/)?.[1] || value?.match(/:\/\/(.*)\//)?.[1] || value;
}

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
