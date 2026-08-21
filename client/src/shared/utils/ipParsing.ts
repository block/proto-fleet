export type ManualDiscoveryTargets = {
  ipAddresses: string[];
  subnets: string[];
  ipRanges: { startIp: string; endIp: string }[];
};

export type CategorizedInvalidEntries = {
  ipAddresses: string[];
  ipRanges: string[];
  subnets: string[];
};

export const parseIpList = (input: string): string[] =>
  input
    .split(/[\n,]+/)
    .map((addr) => addr.trim())
    .filter((addr) => addr !== "");

export const isValidIpv4 = (value: string) => {
  const parts = value.split(".");
  if (parts.length !== 4) return false;
  return parts.every((part) => {
    if (!/^\d{1,3}$/.test(part)) return false;
    // Reject leading-zero octets ("010"): ambiguous, and Go's netip.ParseAddr
    // (used server-side for ip_cidrs / ip_ranges) rejects them — so the client
    // must too, or it accepts inputs the server later fails.
    if (part.length > 1 && part[0] === "0") return false;
    const num = Number(part);
    return num >= 0 && num <= 255;
  });
};

export const isValidIpv6 = (value: string): boolean => {
  if (!value.includes(":")) return false;
  // Reject scoped addresses (%zone) and the full link-local range (fe80::/10,
  // i.e. fe80:-febf:) — the backend cannot use them without interface scope.
  if (value.includes("%")) return false;
  const lower = value.toLowerCase();
  if (/^fe[89ab][0-9a-f]:/.test(lower)) return false;
  try {
    // URL constructor validates and normalizes IPv6 addresses.
    // It will throw for invalid addresses.
    new URL(`http://[${value}]`);
    return true;
  } catch {
    return false;
  }
};

export const isValidHostname = (value: string) => {
  const hostname = value.endsWith(".") ? value.slice(0, -1) : value;
  if (!hostname || hostname.length > 253) return false;
  const labels = hostname.split(".");
  return labels.every((label) => /^[a-z0-9]([a-z0-9-_]*[a-z0-9])?$/i.test(label) && label.length <= 63);
};

// Pack IPv4 octets into a 32-bit unsigned integer for range comparisons.
export const ipv4ToInt = (value: string) =>
  value
    .split(".")
    .map((part) => Number(part))
    .reduce((acc, part) => ((acc << 8) + part) >>> 0, 0);

const SHORT_RANGE_REGEX = /^(\d{1,3}(?:\.\d{1,3}){3})\s*-\s*(\d{1,3})$/;
const FULL_RANGE_REGEX = /^(\d{1,3}(?:\.\d{1,3}){3})\s*-\s*(\d{1,3}(?:\.\d{1,3}){3})$/;
const CIDR_REGEX = /^(\d{1,3}(?:\.\d{1,3}){3})\/(\d{1,2})$/;
const IPV4_LIKE_REGEX = /^\d{1,3}(?:\.\d{1,3}){3}\.?$/;

// True when a token uses the discovery IP-range syntax (short `.10-21` or full
// `.10-.20`), regardless of whether the endpoints are valid. Callers use this
// to route a token to parseIpRange before other IP/CIDR checks.
export const looksLikeIpRange = (value: string): boolean =>
  SHORT_RANGE_REGEX.test(value) || FULL_RANGE_REGEX.test(value);

export const parseIpRange = (value: string) => {
  const shortRangeMatch = value.match(SHORT_RANGE_REGEX);
  if (shortRangeMatch) {
    const startIp = shortRangeMatch[1];
    const endOctet = Number(shortRangeMatch[2]);
    if (!isValidIpv4(startIp) || endOctet < 0 || endOctet > 255) return null;
    const startParts = startIp.split(".");
    const startOctet = Number(startParts[3]);
    if (endOctet < startOctet) return null;
    const endIp = `${startParts[0]}.${startParts[1]}.${startParts[2]}.${endOctet}`;
    return { startIp, endIp };
  }

  const fullRangeMatch = value.match(FULL_RANGE_REGEX);
  if (fullRangeMatch) {
    const startIp = fullRangeMatch[1];
    const endIp = fullRangeMatch[2];
    if (!isValidIpv4(startIp) || !isValidIpv4(endIp)) return null;
    if (ipv4ToInt(endIp) < ipv4ToInt(startIp)) return null;
    return { startIp, endIp };
  }

  return null;
};

export const isValidCidr = (value: string) => {
  const match = value.match(CIDR_REGEX);
  if (!match) return false;
  const ip = match[1];
  const mask = Number(match[2]);
  return isValidIpv4(ip) && mask >= 0 && mask <= 32;
};

const parseCidrLine = (line: string): { ip: string; mask: number } | null => {
  const slashIndex = line.lastIndexOf("/");
  if (slashIndex <= 0 || slashIndex === line.length - 1) return null;

  const ip = line.slice(0, slashIndex);
  const maskStr = line.slice(slashIndex + 1);
  if (!/^\d+$/.test(maskStr)) return null;

  return { ip, mask: Number(maskStr) };
};

export const isValidIpv6Cidr = (value: string): boolean => {
  const parsed = parseCidrLine(value);
  if (!parsed) return false;

  return isValidIpv6(parsed.ip) && parsed.mask >= 0 && parsed.mask <= 128;
};

// The single classification of one free-form token, shared by every subnet / IP
// input surface (onboarding discovery + fleet/rack filters). Consumers project
// the result onto their own shape: discovery buckets into scan targets (see
// parseManualTargets), filters map onto MinerListFilter fields (see
// classifySubnetLine). `looked` records what an invalid token was attempting so
// callers can bucket/report it without re-running the dispatch. Ranges are
// IPv4-only (grammar); CIDRs may be IPv4 or IPv6 — the family is derivable from
// whether `value` contains ":".
export type IpEntry =
  | { kind: "ipv4"; value: string }
  | { kind: "ipv6"; value: string }
  | { kind: "cidr"; value: string }
  | { kind: "range"; startIp: string; endIp: string }
  | { kind: "hostname"; value: string }
  | { kind: "invalid"; value: string; looked: "range" | "cidr" | "ipv4" | "ipv6" | "unknown"; reason: string };

export const categorizeIpEntry = (raw: string): IpEntry => {
  const trimmed = raw.trim();
  if (trimmed === "") return { kind: "invalid", value: trimmed, looked: "unknown", reason: "Empty value" };

  if (looksLikeIpRange(trimmed)) {
    const range = parseIpRange(trimmed);
    return range
      ? { kind: "range", startIp: range.startIp, endIp: range.endIp }
      : { kind: "invalid", value: trimmed, looked: "range", reason: "Not a valid IP range (e.g. 10.0.0.10-10.0.0.20)" };
  }

  if (trimmed.includes("/")) {
    return isValidCidr(trimmed) || isValidIpv6Cidr(trimmed)
      ? { kind: "cidr", value: trimmed }
      : {
          kind: "invalid",
          value: trimmed,
          looked: "cidr",
          reason: "Not a valid CIDR (e.g. 255.255.255.0/24 or 2001:db8::/64)",
        };
  }

  if (IPV4_LIKE_REGEX.test(trimmed)) {
    const candidate = trimmed.endsWith(".") ? trimmed.slice(0, -1) : trimmed;
    return isValidIpv4(candidate)
      ? { kind: "ipv4", value: candidate }
      : { kind: "invalid", value: trimmed, looked: "ipv4", reason: "Not a valid IP address, range, or CIDR" };
  }

  if (trimmed.includes(":")) {
    return isValidIpv6(trimmed)
      ? { kind: "ipv6", value: trimmed }
      : { kind: "invalid", value: trimmed, looked: "ipv6", reason: "Not a valid IP address, range, or CIDR" };
  }

  if (isValidHostname(trimmed)) return { kind: "hostname", value: trimmed };

  return { kind: "invalid", value: trimmed, looked: "unknown", reason: "Not a valid IP address, range, or CIDR" };
};

// Discovery's projection of categorizeIpEntry: split the textarea blob and
// bucket each token into scan targets, keeping invalid tokens for the
// validation dialog. Discovery scans by address/CIDR/range and resolves
// hostnames via DNS, so hostnames are valid targets — but it cannot scan an
// IPv6 CIDR, so those are rejected even though the shared core accepts them for
// filters.
export const parseManualTargets = (input: string) => {
  const targets: ManualDiscoveryTargets = { ipAddresses: [], subnets: [], ipRanges: [] };
  const invalidEntries: string[] = [];
  const categorizedInvalidEntries: CategorizedInvalidEntries = { ipAddresses: [], ipRanges: [], subnets: [] };

  parseIpList(input).forEach((entry) => {
    const parsed = categorizeIpEntry(entry);
    switch (parsed.kind) {
      case "range":
        targets.ipRanges.push({ startIp: parsed.startIp, endIp: parsed.endIp });
        return;
      case "ipv4":
      case "ipv6":
      case "hostname":
        targets.ipAddresses.push(parsed.value);
        return;
      case "cidr":
        // Discovery can't scan an IPv6 CIDR — treat it as an invalid subnet.
        if (parsed.value.includes(":")) {
          invalidEntries.push(parsed.value);
          categorizedInvalidEntries.subnets.push(parsed.value);
        } else {
          targets.subnets.push(parsed.value);
        }
        return;
      case "invalid": {
        invalidEntries.push(parsed.value);
        const bucket = parsed.looked === "range" ? "ipRanges" : parsed.looked === "cidr" ? "subnets" : "ipAddresses";
        categorizedInvalidEntries[bucket].push(parsed.value);
        return;
      }
    }
  });

  return { targets, invalidEntries, categorizedInvalidEntries };
};
