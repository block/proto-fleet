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

export const parseManualTargets = (input: string) => {
  const entries = parseIpList(input);
  const targets: ManualDiscoveryTargets = { ipAddresses: [], subnets: [], ipRanges: [] };
  const invalidEntries: string[] = [];
  const categorizedInvalidEntries: CategorizedInvalidEntries = { ipAddresses: [], ipRanges: [], subnets: [] };

  entries.forEach((entry) => {
    const looksLikeIpRange = SHORT_RANGE_REGEX.test(entry) || FULL_RANGE_REGEX.test(entry);
    if (looksLikeIpRange) {
      const range = parseIpRange(entry);
      if (range) {
        targets.ipRanges.push(range);
      } else {
        invalidEntries.push(entry);
        categorizedInvalidEntries.ipRanges.push(entry);
      }
      return;
    }

    if (isValidCidr(entry)) {
      targets.subnets.push(entry);
      return;
    }

    const looksLikeCidr = CIDR_REGEX.test(entry);
    if (looksLikeCidr) {
      invalidEntries.push(entry);
      categorizedInvalidEntries.subnets.push(entry);
      return;
    }

    const looksLikeIpv4WithOptionalDot = IPV4_LIKE_REGEX.test(entry);
    if (looksLikeIpv4WithOptionalDot) {
      const normalizedEntry = entry.endsWith(".") ? entry.slice(0, -1) : entry;
      if (isValidIpv4(normalizedEntry)) {
        targets.ipAddresses.push(normalizedEntry);
      } else {
        invalidEntries.push(entry);
        categorizedInvalidEntries.ipAddresses.push(entry);
      }
      return;
    }

    // Check for IPv6 CIDR (contains colon and slash) — categorize as invalid subnet
    if (entry.includes(":") && entry.includes("/")) {
      invalidEntries.push(entry);
      categorizedInvalidEntries.subnets.push(entry);
      return;
    }

    // Check for bare IPv6 address (contains colon, no slash)
    if (entry.includes(":")) {
      if (isValidIpv6(entry)) {
        targets.ipAddresses.push(entry);
      } else {
        invalidEntries.push(entry);
        categorizedInvalidEntries.ipAddresses.push(entry);
      }
      return;
    }

    if (isValidHostname(entry)) {
      targets.ipAddresses.push(entry);
      return;
    }

    invalidEntries.push(entry);
    categorizedInvalidEntries.ipAddresses.push(entry);
  });

  return { targets, invalidEntries, categorizedInvalidEntries };
};
