export const arrayOfWarnings = (numberOfErrors: number) => [
  ...Array(Math.min(numberOfErrors, 8)),
];
