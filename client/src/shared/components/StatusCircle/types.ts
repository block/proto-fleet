import { statuses, variants } from "./constants";

export type StatusCircleProps = {
  status: keyof typeof statuses;
  width?: string;
  variant?: keyof typeof variants;
  removeMargin?: boolean;
  isSelected?: boolean;
  testId?: string;
};
