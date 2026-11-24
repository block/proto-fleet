import type { HashboardFirmwareAttempt } from "../adapters/hashboardFirmware";

/**
 * Recovery records for tracked hashboard updates, one localStorage key per task.
 *
 * localStorage cannot make a read followed by a write atomic across tabs, so no record is shared: each one is
 * written and removed only under its own task ID. Tabs that start updates at the same moment therefore cannot
 * overwrite or delete each other's record, whatever order their writes land in. The controller, not this browser,
 * decides which Start wins.
 */
const keyPrefix = (deviceId: string) => `proto-os:hashboard-firmware:${encodeURIComponent(deviceId)}:`;

const isAttempt = (value: HashboardFirmwareAttempt | null): value is HashboardFirmwareAttempt =>
  !!value?.taskId &&
  Array.isArray(value.boards) &&
  value.boards.length > 0 &&
  value.boards.every(
    (board) => typeof board.mac === "string" && typeof board.owner === "string" && typeof board.label === "string",
  );

/** Every saved record for this device, in a stable order. Unreadable or malformed entries are not tracked updates. */
export const readAttempts = (deviceId: string): HashboardFirmwareAttempt[] => {
  const prefix = keyPrefix(deviceId);
  const attempts: HashboardFirmwareAttempt[] = [];
  try {
    for (let index = 0; index < localStorage.length; index++) {
      const key = localStorage.key(index);
      if (!key?.startsWith(prefix)) continue;
      try {
        const value = JSON.parse(localStorage.getItem(key) || "null") as HashboardFirmwareAttempt | null;
        if (isAttempt(value) && value.taskId === key.slice(prefix.length)) attempts.push(value);
      } catch {
        /* A malformed entry is skipped; the others are still read. */
      }
    }
  } catch {
    /* Inaccessible storage is handled before starting: saving the record fails, which refuses Start. */
  }
  return attempts.sort((a, b) => (a.taskId < b.taskId ? -1 : 1));
};

/** The record to show: one update is presented at a time, and the rest are picked up as each is released. */
export const readAttempt = (deviceId: string): HashboardFirmwareAttempt | undefined => readAttempts(deviceId)[0];

/** Throws when storage is unavailable, so the caller can refuse Start rather than lose recovery information. */
export const saveAttempt = (deviceId: string, attempt: HashboardFirmwareAttempt) =>
  localStorage.setItem(keyPrefix(deviceId) + attempt.taskId, JSON.stringify(attempt));

export const removeAttempt = (deviceId: string, taskId: string) =>
  localStorage.removeItem(keyPrefix(deviceId) + taskId);
