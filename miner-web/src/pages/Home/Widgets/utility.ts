export const getIntensity = (value?: number | string, max?: number | string) => {
  if (!value || !max) return 0;
  return (+value * 10) / +max;
};
