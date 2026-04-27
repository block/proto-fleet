export const poolNameValidationErrors = {
  required: "A Pool Name is required.",
} as const;

export const urlValidationErrors = {
  required: "A Pool URL is required to connect to this pool.",
  duplicate: "This Pool URL and Username combination is already configured.",
  unknownScheme:
    "Pool URL must start with stratum+tcp:// (Stratum V1) or stratum2+tcp:// (Stratum V2). Plain TCP only in v1; TLS / WebSocket variants are not supported by the dispatch path.",
} as const;

export const usernameValidationErrors = {
  required: "A Username is required to connect to this pool.",
  separator: "Fleet-level pool usernames can’t include periods (.). Set worker names on each miner instead.",
} as const;

export const fleetUsernameHelperText = "Worker name will be appended to this username when applied to miners.";

// Accepted URL prefixes — mirror of server's pools.v1 CEL rule. Any URL
// not starting with one of these gets rejected by the server, so we
// surface the same check client-side to fail fast in the form. Plain TCP
// only in v1: SSL/WS schemes are intentionally excluded because the
// dispatch path uses bare net.Dial and would silently fail to negotiate
// TLS.
const acceptedSchemes = ["stratum+tcp://", "stratum2+tcp://"] as const;

export const validateURLScheme = (url: string): string | undefined => {
  const lower = url.trim().toLowerCase();
  if (!lower) return undefined; // absence handled by required rule
  if (acceptedSchemes.some((prefix) => lower.startsWith(prefix))) return undefined;
  return urlValidationErrors.unknownScheme;
};
