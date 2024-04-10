export const getIntensity = (
  value?: number | string | null,
  max?: number | string
) => {
  if (!value || !max) return 0;
  return Math.round((+value * 10) / +max);
};

export const getDisplayValue = (value?: number | string | null) => {
  if (value === undefined || value === null) return;

  const numberValue = +value;
  if (typeof numberValue !== "number") return value;

  return numberValue.toFixed(2);
};
