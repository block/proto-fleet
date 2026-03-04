export const INACTIVE_PLACEHOLDER = "—";

export const positions = {
  top: "top",
  "top left": "top left",
  "top right": "top right",
  bottom: "bottom",
  "bottom left": "bottom left",
  "bottom right": "bottom right",
} as const;

export type Position = keyof typeof positions;

export const selectTypes = {
  checkbox: "checkbox",
  radio: "radio",
} as const;

export type SelectType = keyof typeof selectTypes;
