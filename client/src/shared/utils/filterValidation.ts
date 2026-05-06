import { isValidCidr, isValidIpv4, isValidIpv6 } from "./networkDiscovery";

export type NumericRangeValue = {
  min?: number;
  max?: number;
};

export type NumericRangeBounds = {
  min: number;
  max: number;
  unit: string;
};

export type NumericRangeErrors = {
  min?: string;
  max?: string;
  cross?: string;
};

const isFiniteNumber = (n: number | undefined): n is number => typeof n === "number" && Number.isFinite(n);

/**
 * Validates a numeric range against per-field logical bounds.
 * Returns one error string per offending field; empty object = valid.
 * Bounds are treated as inclusive: a value equal to bounds.min/bounds.max passes.
 */
export const validateNumericRange = (value: NumericRangeValue, bounds: NumericRangeBounds): NumericRangeErrors => {
  const errors: NumericRangeErrors = {};

  if (value.min !== undefined) {
    if (!isFiniteNumber(value.min)) {
      errors.min = "Enter a finite number";
    } else if (value.min < bounds.min) {
      errors.min = `Minimum is ${bounds.min} ${bounds.unit}`;
    } else if (value.min > bounds.max) {
      errors.min = `Maximum is ${bounds.max} ${bounds.unit}`;
    }
  }

  if (value.max !== undefined) {
    if (!isFiniteNumber(value.max)) {
      errors.max = "Enter a finite number";
    } else if (value.max < bounds.min) {
      errors.max = `Minimum is ${bounds.min} ${bounds.unit}`;
    } else if (value.max > bounds.max) {
      errors.max = `Maximum is ${bounds.max} ${bounds.unit}`;
    }
  }

  if (isFiniteNumber(value.min) && isFiniteNumber(value.max) && !errors.min && !errors.max && value.min > value.max) {
    errors.cross = "Min must not exceed Max";
  }

  return errors;
};

const parseCidrLine = (line: string): { ip: string; mask: number } | null => {
  const slashIndex = line.lastIndexOf("/");
  if (slashIndex <= 0 || slashIndex === line.length - 1) return null;

  const ip = line.slice(0, slashIndex);
  const maskStr = line.slice(slashIndex + 1);
  if (!/^\d+$/.test(maskStr)) return null;

  return { ip, mask: Number(maskStr) };
};

const isValidIpv6Cidr = (value: string): boolean => {
  const parsed = parseCidrLine(value);
  if (!parsed) return false;

  return isValidIpv6(parsed.ip) && parsed.mask >= 0 && parsed.mask <= 128;
};

/**
 * Returns null if the line is a valid IPv4/IPv6 CIDR or bare IP address
 * (treated as /32 or /128). Returns a human-readable error string otherwise.
 * Link-local and scoped IPv6 are rejected to match discovery/pairing support.
 */
export const validateCidrLine = (line: string): string | null => {
  const trimmed = line.trim();
  if (trimmed === "") return "Empty value";

  if (trimmed.includes("/")) {
    return isValidCidr(trimmed) || isValidIpv6Cidr(trimmed)
      ? null
      : "Not a valid CIDR (e.g. 255.255.255.0/24 or 2001:db8::/64)";
  }

  return isValidIpv4(trimmed) || isValidIpv6(trimmed) ? null : "Not a valid IP address or CIDR";
};

/**
 * Normalizes a CIDR or bare IP to canonical network form, mirroring the
 * server's parseCIDR semantics. IPv4 inputs are canonicalized client-side;
 * IPv6 bare IPs become /128, while IPv6 CIDRs are left as-is for the server
 * to canonicalize. Assumes the input has already passed validateCidrLine; on
 * bad input it returns the trimmed value unchanged.
 */
export const normalizeCidrLine = (line: string): string => {
  const trimmed = line.trim();
  if (trimmed === "") return trimmed;

  if (!trimmed.includes("/")) {
    if (isValidIpv4(trimmed)) return `${trimmed}/32`;
    if (isValidIpv6(trimmed)) return `${trimmed}/128`;
    return trimmed;
  }

  if (isValidIpv6Cidr(trimmed)) return trimmed;

  const [ip, maskStr] = trimmed.split("/");
  const mask = Number(maskStr);
  if (!isValidIpv4(ip) || !Number.isInteger(mask) || mask < 0 || mask > 32) {
    return trimmed;
  }

  // Mask host bits to canonical network address.
  const octets = ip.split(".").map(Number);
  const ipInt = ((octets[0] << 24) | (octets[1] << 16) | (octets[2] << 8) | octets[3]) >>> 0;
  const maskInt = mask === 0 ? 0 : (0xffffffff << (32 - mask)) >>> 0;
  const network = ipInt & maskInt;
  const networkOctets = [(network >>> 24) & 0xff, (network >>> 16) & 0xff, (network >>> 8) & 0xff, network & 0xff];
  return `${networkOctets.join(".")}/${mask}`;
};
