export const STATUSES = {
  queued: "queued",
  loading: "loading",
  success: "success",
  error: "error",
} as const;

export const ACTIONS = {
  push: "push",
  update: "update",
  remove: "remove",
  clear: "clear",
} as const;

export const defaultTtl = 4000;
