export const poolNameValidationErrors = {
  required: "A Pool Name is required.",
} as const;

export const urlValidationErrors = {
  required: "A Pool URL is required to connect to this pool.",
  duplicate: "This Pool URL and Username combination is already configured.",
  unknownScheme: "Pool URL must start with stratum+tcp://, stratum+ssl://, stratum+ws:// (V1) or stratum2+tcp:// (V2).",
  standaloneUnknownScheme: "Pool URL must start with stratum+tcp:// (V1) or stratum2+tcp:// (V2).",
  v2MissingPort:
    "Stratum V2 URLs require an explicit port, e.g. stratum2+tcp://pool.example.com:3336/<authority_pubkey>.",
  v2MissingPubkey:
    "Stratum V2 URLs require the pool's authority pubkey as a path component, e.g. stratum2+tcp://pool.example.com:3336/<authority_pubkey>. Find it in your pool operator's V2 docs.",
  v2PathNotSupported:
    "Enter the Stratum V2 pool host only, e.g. stratum2+tcp://pool.example.com:3336. Add the authority public key in its separate field.",
} as const;

export const v2AuthorityKeyValidationErrors = {
  required: "An authority public key is required for remote Stratum V2 pools.",
} as const;

interface PoolURLValidationOptions {
  standaloneV2AuthorityKey?: boolean;
}

// Mirror of the Fleet server's pools.v1 CEL rule so Fleet forms fail fast.
const legacyAcceptedSchemes = ["stratum+tcp://", "stratum+ssl://", "stratum+ws://", "stratum2+tcp://"] as const;
const standaloneAcceptedSchemes = ["stratum+tcp://", "stratum2+tcp://"] as const;
const sv2Prefix = "stratum2+tcp://";

export const isStratumV2URL = (url: string) => url.trim().toLowerCase().startsWith(sv2Prefix);

export const requiresStandaloneV2AuthorityKey = (url: string) => {
  const trimmed = url.trim();
  if (!isStratumV2URL(trimmed)) return false;

  try {
    const hostname = new URL(trimmed).hostname.replace(/^\[(.*)\]$/, "$1").toLowerCase();
    return hostname !== "localhost" && hostname !== "::1" && !hostname.startsWith("127.");
  } catch {
    return true;
  }
};

export const getV2AuthorityKeyValidationError = (url: string, authorityKey?: string) => {
  if (requiresStandaloneV2AuthorityKey(url) && !authorityKey?.trim()) {
    return v2AuthorityKeyValidationErrors.required;
  }
  return undefined;
};

const validateStandaloneV2URL = (afterScheme: string) => {
  const slashIdx = afterScheme.indexOf("/");
  return slashIdx >= 0 && afterScheme.slice(slashIdx + 1) ? urlValidationErrors.v2PathNotSupported : undefined;
};

const validateLegacyV2URL = (afterScheme: string) => {
  const slashIdx = afterScheme.indexOf("/");
  const hostPort = slashIdx >= 0 ? afterScheme.slice(0, slashIdx) : afterScheme;
  if (!/:\d+$/.test(hostPort)) return urlValidationErrors.v2MissingPort;
  if (slashIdx < 0 || slashIdx === afterScheme.length - 1) return urlValidationErrors.v2MissingPubkey;
  return undefined;
};

export const validateURLScheme = (
  url: string,
  { standaloneV2AuthorityKey = false }: PoolURLValidationOptions = {},
): string | undefined => {
  const trimmed = url.trim();
  if (!trimmed) return undefined;
  const lower = trimmed.toLowerCase();
  const acceptedSchemes = standaloneV2AuthorityKey ? standaloneAcceptedSchemes : legacyAcceptedSchemes;
  if (!acceptedSchemes.some((prefix) => lower.startsWith(prefix))) {
    return standaloneV2AuthorityKey ? urlValidationErrors.standaloneUnknownScheme : urlValidationErrors.unknownScheme;
  }
  if (lower.startsWith(sv2Prefix)) {
    const afterScheme = trimmed.slice(sv2Prefix.length);
    return standaloneV2AuthorityKey ? validateStandaloneV2URL(afterScheme) : validateLegacyV2URL(afterScheme);
  }
  return undefined;
};

export const usernameValidationErrors = {
  required: "A Username is required to connect to this pool.",
  separator: "Fleet-level pool usernames can’t include periods (.). Set worker names on each miner instead.",
} as const;

export const fleetUsernameHelperText = "Worker name will be appended to this username when applied to miners.";
