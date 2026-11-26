import type { MetricUnit } from "../contract";

/** An absent or invalid reading clears the previous value. Zero is a valid reading. */
export const sensorReading = (value: unknown, units: MetricUnit) => ({
  latest: { value: typeof value === "number" && Number.isFinite(value) ? value : null, units },
});
