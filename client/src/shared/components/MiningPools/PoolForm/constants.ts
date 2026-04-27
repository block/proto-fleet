export const poolNameValidationErrors = {
  required: "A Pool Name is required.",
} as const;

export const urlValidationErrors = {
  required: "A Pool URL is required to connect to this pool.",
  duplicate: "This Pool URL and Username combination is already configured.",
  unknownScheme:
    "Pool URL must start with stratum+tcp://, stratum+ssl://, stratum+ws:// (V1) or stratum2+tcp:// (V2).",
} as const;

// Accepted URL prefixes — mirror of the server's ValidatePoolRequest.url
// CEL rule. Surfacing the same check client-side fails fast in the form
// instead of waiting for the server to reject on Save.
const acceptedSchemes = [
  "stratum+tcp://",
  "stratum+ssl://",
  "stratum+ws://",
  "stratum2+tcp://",
] as const;

export const validateURLScheme = (url: string): string | undefined => {
  const lower = url.trim().toLowerCase();
  if (!lower) return undefined; // empty handled by required rule
  if (acceptedSchemes.some((prefix) => lower.startsWith(prefix))) return undefined;
  return urlValidationErrors.unknownScheme;
};

export const usernameValidationErrors = {
  required: "A Username is required to connect to this pool.",
  separator: "Fleet-level pool usernames can’t include periods (.). Set worker names on each miner instead.",
} as const;

export const fleetUsernameHelperText = "Worker name will be appended to this username when applied to miners.";
