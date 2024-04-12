export const getIntensity = (
  value?: number | string | null,
  max?: number | string
) => {
  if (!value || !max) return 0;
  return Math.round((+value * 10) / +max);
};

export const separateByCommas = (value: string) => {
  const [integer, decimal] = value.split(".");
  const commaSeparatedInteger = integer.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  if (decimal) {
    return `${commaSeparatedInteger}.${decimal}`;
  }
  return commaSeparatedInteger;
}

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
