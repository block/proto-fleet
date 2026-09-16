import { type RunInDefaults, validateDefaults } from "./defaults";

export interface RunInAttempt {
  defaults: RunInDefaults;
  taskId: string;
  uncertain: boolean;
}

export const attemptKey = (deviceId: string, moduleId: string) =>
  `proto-os:run-in-attempt:v1:${encodeURIComponent(deviceId)}:${encodeURIComponent(moduleId)}`;

export function readAttempt(key: string): RunInAttempt | undefined {
  try {
    const value = JSON.parse(sessionStorage.getItem(key) ?? "null") as Partial<RunInAttempt> | null;
    if (
      value &&
      validateDefaults(value.defaults) &&
      typeof value.taskId === "string" &&
      /^[0-9a-f]{32}$/.test(value.taskId)
    ) {
      // A previous view may have disappeared while its request was in flight.
      return { defaults: value.defaults, taskId: value.taskId, uncertain: true };
    }
  } catch {
    // Starting a new attempt still requires a successful storage write.
  }
}
