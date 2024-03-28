export const getTickValue = (value: string | number, marginValue: number = 0) => {
  return Math.round((+value - marginValue) * 100) / 100;
};
