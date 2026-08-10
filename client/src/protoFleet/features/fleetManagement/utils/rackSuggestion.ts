import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

export const RACK_SUGGESTION_MIN_MINERS = 8;
export const RACK_SUGGESTION_MAX_MINERS = 144;

type IPv4Parts = [number, number, number, number];

type CandidateMiner = {
  miner: MinerStateSnapshot;
  ipNumber: number;
  prefix: string;
};

export type RackCreationSuggestion = {
  count: number;
  minerIds: string[];
  ipRangeLabel: string;
  ipRangeFilter: string;
  modelSummary?: string;
  dismissalKey: string;
};

const parseIPv4 = (value: string): IPv4Parts | undefined => {
  const parts = value.split(".");
  if (parts.length !== 4) return undefined;
  const nums = parts.map((part) => {
    if (!/^\d{1,3}$/.test(part)) return Number.NaN;
    return Number(part);
  });
  if (nums.some((n) => !Number.isInteger(n) || n < 0 || n > 255)) return undefined;
  return nums as IPv4Parts;
};

const ipv4ToNumber = ([a, b, c, d]: IPv4Parts): number => ((a << 24) >>> 0) + (b << 16) + (c << 8) + d;

const numberToIPv4 = (value: number): string =>
  [value >>> 24, (value >>> 16) & 255, (value >>> 8) & 255, value & 255].join(".");

const buildCandidate = (miner: MinerStateSnapshot): CandidateMiner | undefined => {
  const parts = parseIPv4(miner.ipAddress.trim());
  if (!parts || !miner.deviceIdentifier) return undefined;
  return {
    miner,
    ipNumber: ipv4ToNumber(parts),
    prefix: `${parts[0]}.${parts[1]}.${parts[2]}`,
  };
};

const modelSummaryFor = (miners: MinerStateSnapshot[]): string | undefined => {
  const counts = new Map<string, number>();
  for (const miner of miners) {
    const label = miner.model.trim() || miner.manufacturer.trim() || miner.driverName.trim();
    if (!label) continue;
    counts.set(label, (counts.get(label) ?? 0) + 1);
  }
  if (counts.size === 0) return undefined;

  const [topLabel, topCount] = Array.from(counts.entries()).sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))[0];
  if (topCount === miners.length) return `All ${topLabel}.`;
  if (topCount >= Math.ceil(miners.length * 0.6)) return `Mostly ${topLabel}.`;
  return `${counts.size} models.`;
};

const toSuggestion = (group: CandidateMiner[]): RackCreationSuggestion | undefined => {
  if (group.length < RACK_SUGGESTION_MIN_MINERS || group.length > RACK_SUGGESTION_MAX_MINERS) return undefined;

  const sorted = [...group].sort(
    (a, b) => a.ipNumber - b.ipNumber || a.miner.deviceIdentifier.localeCompare(b.miner.deviceIdentifier),
  );
  const first = sorted[0];
  const last = sorted[sorted.length - 1];
  const firstIP = numberToIPv4(first.ipNumber);
  const lastIP = numberToIPv4(last.ipNumber);
  const ipRangeFilter = firstIP === lastIP ? firstIP : `${firstIP}-${lastIP}`;

  return {
    count: sorted.length,
    minerIds: sorted.map((candidate) => candidate.miner.deviceIdentifier),
    ipRangeLabel: ipRangeFilter,
    ipRangeFilter,
    modelSummary: modelSummaryFor(sorted.map((candidate) => candidate.miner)),
    dismissalKey: `rack:${firstIP}:${lastIP}:${sorted.length}`,
  };
};

export const buildRackCreationSuggestion = (
  miners: MinerStateSnapshot[],
  maxIPGap: number = 4,
): RackCreationSuggestion | undefined => {
  const byPrefix = new Map<string, CandidateMiner[]>();
  for (const miner of miners) {
    const candidate = buildCandidate(miner);
    if (!candidate) continue;
    const bucket = byPrefix.get(candidate.prefix) ?? [];
    bucket.push(candidate);
    byPrefix.set(candidate.prefix, bucket);
  }

  const groups: CandidateMiner[][] = [];
  for (const bucket of byPrefix.values()) {
    const sorted = [...bucket].sort((a, b) => a.ipNumber - b.ipNumber);
    let current: CandidateMiner[] = [];
    for (const candidate of sorted) {
      const previous = current[current.length - 1];
      if (previous && candidate.ipNumber - previous.ipNumber > maxIPGap) {
        groups.push(current);
        current = [];
      }
      current.push(candidate);
    }
    if (current.length > 0) groups.push(current);
  }

  return groups
    .map(toSuggestion)
    .filter((suggestion): suggestion is RackCreationSuggestion => suggestion !== undefined)
    .sort((a, b) => b.count - a.count || a.ipRangeLabel.localeCompare(b.ipRangeLabel))[0];
};
