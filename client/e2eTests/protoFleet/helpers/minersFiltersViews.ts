function parseIpv4(ip: string) {
  const normalizedIp = ip.trim();
  const octets = normalizedIp.split(".");

  if (octets.length !== 4) {
    return null;
  }

  const numericOctets = octets.map((octet) => Number(octet));
  const isValidIpv4 = numericOctets.every((octet) => Number.isInteger(octet) && octet >= 0 && octet <= 255);

  if (!isValidIpv4) {
    return null;
  }

  return normalizedIp;
}

export async function getFirstVisibleIpv4MinerIp(minersPage: {
  getMinersCount(): Promise<number>;
  getMinerIpAddressByIndex(index: number): Promise<string>;
}) {
  const minerCount = await minersPage.getMinersCount();

  for (let index = 0; index < minerCount; index++) {
    const ipAddress = await minersPage.getMinerIpAddressByIndex(index);
    const parsedIp = parseIpv4(ipAddress);

    if (parsedIp !== null) {
      return parsedIp;
    }
  }

  throw new Error("Subnet filter coverage requires at least one visible IPv4 miner.");
}

export function toSubnet24(ip: string) {
  const parsedIp = parseIpv4(ip);
  if (parsedIp === null) {
    throw new Error(`Expected a valid IPv4 address, got "${ip}".`);
  }

  const [first, second, third] = parsedIp.split(".");
  return `${first}.${second}.${third}.0/24`;
}

export function formatPowerFilterSummary(min: number | undefined, max: number | undefined) {
  if (min !== undefined && max !== undefined) {
    return `${min} kW - ${max} kW`;
  }

  if (min !== undefined) {
    return `≥ ${min} kW`;
  }

  if (max !== undefined) {
    return `≤ ${max} kW`;
  }

  return "";
}
