export interface MappingAttempt {
  token: string;
  runId?: string;
}

const keyPrefix = (deviceId: string) => `proto-os:feed-mapping:${encodeURIComponent(deviceId)}:`;
export const isMappingAttemptKey = (deviceId: string, key: string | null) =>
  key === null || key.startsWith(keyPrefix(deviceId));

/** Each Start owns its key. Concurrent starts cannot overwrite or remove each other's recovery data. */
export function readMappingAttempts(deviceId: string): MappingAttempt[] {
  const prefix = keyPrefix(deviceId);
  const attempts: MappingAttempt[] = [];
  for (let index = 0; index < localStorage.length; index++) {
    const key = localStorage.key(index);
    if (!key?.startsWith(prefix)) continue;
    // A failed storage read is not a deletion. Callers report it and block new starts.
    const stored = localStorage.getItem(key);
    try {
      const value = JSON.parse(stored || "null");
      if (
        value &&
        typeof value.token === "string" &&
        value.token.length > 0 &&
        value.token === key.slice(prefix.length) &&
        (value.runId === undefined || (typeof value.runId === "string" && value.runId.length > 0))
      )
        attempts.push(value);
    } catch {
      // A malformed record must not hide the other attempts.
    }
  }
  // Recover known run results before attempts whose Start outcome remains unknown.
  return attempts.sort((a, b) => Number(!a.runId) - Number(!b.runId) || a.token.localeCompare(b.token));
}

export function readMappingAttempt(deviceId: string, preferredToken?: string): MappingAttempt | undefined {
  const attempts = readMappingAttempts(deviceId);
  return attempts.find((attempt) => attempt.token === preferredToken) ?? attempts[0];
}

export const saveMappingAttempt = (deviceId: string, attempt: MappingAttempt) =>
  localStorage.setItem(keyPrefix(deviceId) + attempt.token, JSON.stringify(attempt));

export const removeMappingAttempt = (deviceId: string, token: string) =>
  localStorage.removeItem(keyPrefix(deviceId) + token);
