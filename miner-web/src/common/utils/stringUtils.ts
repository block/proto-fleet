// add a space between each 4 characters
const addSpacing = (value?: string) => {
  return value?.match(/.{1,4}/g)?.join(" ");
};

export const getSerialNumbersDisplay = (value?: (string | undefined)[]) => {
  return value?.map((item) => addSpacing(item));
};

// first match for stratum v1 url formats from :// to :
// second match for stratum v2 url formats from :// to /
// else return the original value
export const getUrlDisplay = (value?: string) => {
  return value?.match(/:\/\/(.*):/)?.[1] || value?.match(/:\/\/(.*)\//)?.[1] || value;
}
