export const deepClone = (obj: any) => {
  const stringify = JSON.stringify(obj);
  if (!stringify) {
    return obj;
  }
  return JSON.parse(stringify);
};

export const debounce = (callback: (...args: any) => void) => {
  let timeoutId: ReturnType<typeof setTimeout> | undefined;
  return (...args: any) => {
    const context = this;
    if (timeoutId) clearTimeout(timeoutId);
    timeoutId = setTimeout(() => {
      timeoutId = undefined;
      callback.apply(context, args);
    }, 500);
  };
};

export const getRandomInt = (min: number, max: number) => {
  return Math.floor(Math.random() * (max - min + 1) + min);
};

// precision is used for the number of decimal places, e.g. 100 for 2 decimal places
export const getRandomFloat = (min: number, max: number, precision: number = 100) => {
  return (
    (Math.floor(
      Math.random() * (max * precision - min * precision) + 1 * precision
    ) +
      min * precision) /
    (1 * precision)
  );
};

export const convertMhSToThS = (value: number = 0) => value / 1000000;
