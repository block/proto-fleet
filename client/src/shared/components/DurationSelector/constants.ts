export const durations = ["1h", "12h", "24h", "48h", "5d"] as const;

export type Duration = (typeof durations)[number];
