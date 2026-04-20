export const statuses = {
  error: "error",
  warning: "warning",
  normal: "normal",
  inactive: "inactive",
  pending: "pending",
  sleeping: "sleeping",
} as const;

export type StatusCircleStatus = keyof typeof statuses;

export const variants = {
  primary: "primary",
  simple: "simple",
} as const;
