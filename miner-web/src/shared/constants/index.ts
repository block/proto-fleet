export const positions = {
  top: "top",
  "top left": "top left",
  "top right": "top right",
  bottom: "bottom",
  "bottom left": "bottom left",
  "bottom right": "bottom right",
} as const;

export type Position = keyof typeof positions;
