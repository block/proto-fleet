import type { FrigateUpdateRecord, FrigateUpdateStep } from "../adapters/frigateServerUpdate";

/**
 * Recovery records for local Frigate software updates, one localStorage key per job, as with hashboard firmware
 * records: each tab writes and removes only its own job's record, and the controllers decide which Start wins.
 */
const keyPrefix = (deviceId: string) => `proto-os:frigate-update:${encodeURIComponent(deviceId)}:`;

/** Whether a `storage` event's key is one of this device's records. A null key means storage was cleared. */
export const isFrigateUpdateKey = (deviceId: string, key: string | null) =>
  key === null || key.startsWith(keyPrefix(deviceId));

const steps = new Set<FrigateUpdateStep>(["staging", "staged", "starting", "started"]);

const isRecord = (value: FrigateUpdateRecord | null): value is FrigateUpdateRecord =>
  !!value?.jobId &&
  !!value.idempotencyKey &&
  typeof value.targetVersion === "string" &&
  typeof value.packageName === "string" &&
  steps.has(value.step) &&
  Array.isArray(value.instanceIds) &&
  value.instanceIds.length > 0 &&
  value.instanceIds.every((id) => typeof id === "string") &&
  !!value.fingerprints &&
  typeof value.fingerprints === "object" &&
  Object.values(value.fingerprints).every((fingerprint) => typeof fingerprint === "string");

/** Every saved record for this device, in a stable order. Unreadable or malformed entries are not tracked updates. */
export const readFrigateUpdates = (deviceId: string): FrigateUpdateRecord[] => {
  const prefix = keyPrefix(deviceId);
  const records: FrigateUpdateRecord[] = [];
  try {
    for (let index = 0; index < localStorage.length; index++) {
      const key = localStorage.key(index);
      if (!key?.startsWith(prefix)) continue;
      try {
        const value = JSON.parse(localStorage.getItem(key) || "null") as FrigateUpdateRecord | null;
        if (isRecord(value) && value.jobId === key.slice(prefix.length)) records.push(value);
      } catch {
        /* A malformed entry is skipped; the others are still read. */
      }
    }
  } catch {
    /* Inaccessible storage is handled before staging: saving the record fails, which refuses the update. */
  }
  return records.sort((a, b) => (a.jobId < b.jobId ? -1 : 1));
};

/** The record to show: one update is presented at a time. */
export const readFrigateUpdate = (deviceId: string): FrigateUpdateRecord | undefined => readFrigateUpdates(deviceId)[0];

/** Throws when storage is unavailable, so the caller can refuse the update rather than lose recovery information. */
export const saveFrigateUpdate = (deviceId: string, record: FrigateUpdateRecord) =>
  localStorage.setItem(keyPrefix(deviceId) + record.jobId, JSON.stringify(record));

export const removeFrigateUpdate = (deviceId: string, jobId: string) =>
  localStorage.removeItem(keyPrefix(deviceId) + jobId);
