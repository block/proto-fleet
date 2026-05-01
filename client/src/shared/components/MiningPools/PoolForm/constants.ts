export const poolNameValidationErrors = {
  required: "A Pool Name is required.",
} as const;

export const urlValidationErrors = {
  required: "A Pool URL is required to connect to this pool.",
  duplicate: "This Pool URL and Username combination is already configured.",
  unknownScheme: "Pool URL must start with stratum+tcp://, stratum+ssl://, stratum+ws:// (V1) or stratum2+tcp:// (V2).",
  v2MissingPort:
    "Stratum V2 URLs require an explicit port, e.g. stratum2+tcp://pool.example.com:3336/<authority_pubkey>.",
  v2MissingPubkey:
    "Stratum V2 URLs require the pool's authority pubkey as a path component, e.g. stratum2+tcp://pool.example.com:3336/<authority_pubkey>. Find it in your pool operator's V2 docs.",
} as const;

// Mirror of the server's pools.v1 CEL rule so the form fails fast.
const acceptedSchemes = ["stratum+tcp://", "stratum+ssl://", "stratum+ws://", "stratum2+tcp://"] as const;
const sv2Prefix = "stratum2+tcp://";

export const validateURLScheme = (url: string): string | undefined => {
  const trimmed = url.trim();
  if (!trimmed) return undefined;
  const lower = trimmed.toLowerCase();
  if (!acceptedSchemes.some((prefix) => lower.startsWith(prefix))) {
    return urlValidationErrors.unknownScheme;
  }
  if (lower.startsWith(sv2Prefix)) {
    const afterScheme = trimmed.slice(sv2Prefix.length);
    const slashIdx = afterScheme.indexOf("/");
    const hostPort = slashIdx >= 0 ? afterScheme.slice(0, slashIdx) : afterScheme;
    if (!/:\d+$/.test(hostPort)) return urlValidationErrors.v2MissingPort;
    if (slashIdx < 0 || slashIdx === afterScheme.length - 1) return urlValidationErrors.v2MissingPubkey;
  }
  return undefined;
};

export const usernameValidationErrors = {
  required: "A Username is required to connect to this pool.",
  separator: "Fleet-level pool usernames can’t include periods (.). Set worker names on each miner instead.",
} as const;

export const fleetUsernameHelperText = "Worker name will be appended to this username when applied to miners.";
