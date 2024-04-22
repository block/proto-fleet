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
