import { colors } from "./constants";

export const getGradientBarValues = (intensity: number) => {
  let colorValues;
  if (intensity <= 2) {
    colorValues = colors.blue;
  } else if (intensity <= 4) {
    colorValues = colors.green;
  } else if (intensity <= 6) {
    colorValues = colors.orange;
  } else if (intensity <= 8) {
    colorValues = colors.redOrange;
  } else {
    colorValues = colors.red;
  }
  return {
    bgColor: colorValues.bg,
    gradientColor: colorValues.gradient,
    gradientId: colorValues.id,
  };
};
