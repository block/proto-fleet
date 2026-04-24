export const poolNameValidationErrors = {
  required: "A Pool Name is required.",
} as const;

export const urlValidationErrors = {
  required: "A Pool URL is required to connect to this pool.",
  duplicate: "This Pool URL and Username combination is already configured.",
  unknownScheme:
    "Pool URL must start with stratum+tcp://, stratum+ssl://, stratum+ws:// (V1) or stratum2+tcp://, stratum2+ssl:// (V2).",
} as const;

export const usernameValidationErrors = {
  required: "A Username is required to connect to this pool.",
  separator: "Fleet-level pool usernames can’t include periods (.). Set worker names on each miner instead.",
} as const;

export const fleetUsernameHelperText = "Worker name will be appended to this username when applied to miners.";

// Accepted URL prefixes — mirror of server's pools.v1 CEL rule. Any URL
// not starting with one of these gets rejected by the server, so we
// surface the same check client-side to fail fast in the form.
const acceptedSchemes = [
  "stratum+tcp://",
  "stratum+ssl://",
  "stratum+ws://",
  "stratum2+tcp://",
  "stratum2+ssl://",
] as const;

export const validateURLScheme = (url: string): string | undefined => {
  const lower = url.trim().toLowerCase();
  if (!lower) return undefined; // absence handled by required rule
  if (acceptedSchemes.some((prefix) => lower.startsWith(prefix))) return undefined;
  return urlValidationErrors.unknownScheme;
};
