import type { GetCohortFirmwareVersionHistoryResponse } from "@/protoFleet/api/generated/cohort/v1/cohort_pb";

const UNKNOWN_VERSION = "";
const MAX_VISIBLE_VERSIONS = 5;
const VERSION_COLORS = [
  "var(--color-extended-navy-fill)",
  "var(--color-extended-teal-fill)",
  "var(--color-extended-forest-fill)",
  "var(--color-extended-purple-fill)",
  "var(--color-extended-pink-fill)",
  "var(--color-extended-sky-fill)",
] as const;
const UNKNOWN_COLOR = "var(--color-core-primary-10)";
const OTHER_COLOR = "var(--color-core-primary-50)";

export type FirmwareSeries = {
  key: string;
  label: string;
  versions: string[];
  color: string;
};

export const firmwareColor = (version: string) => {
  let hash = 0;
  for (const character of version) hash = (hash * 31 + character.charCodeAt(0)) >>> 0;
  return VERSION_COLORS[hash % VERSION_COLORS.length];
};

export const buildFirmwareSeries = (history: GetCohortFirmwareVersionHistoryResponse): FirmwareSeries[] => {
  const maxCounts = new Map<string, number>();
  for (const point of history.points) {
    for (const version of point.versions) {
      maxCounts.set(
        version.firmwareVersion,
        Math.max(maxCounts.get(version.firmwareVersion) ?? 0, version.deviceCount),
      );
    }
  }

  const knownVersions = [...maxCounts.entries()]
    .filter(([version]) => version !== UNKNOWN_VERSION)
    .sort(([leftVersion, leftCount], [rightVersion, rightCount]) => {
      return rightCount - leftCount || leftVersion.localeCompare(rightVersion);
    })
    .map(([version]) => version);
  const visibleVersions = knownVersions.slice(0, MAX_VISIBLE_VERSIONS);
  const hiddenVersions = knownVersions.slice(MAX_VISIBLE_VERSIONS);
  const series: FirmwareSeries[] = visibleVersions.map((version, index) => ({
    key: `firmware-${index}`,
    label: version,
    versions: [version],
    color: firmwareColor(version),
  }));

  if (maxCounts.has(UNKNOWN_VERSION)) {
    series.push({ key: "firmware-unknown", label: "Unknown", versions: [UNKNOWN_VERSION], color: UNKNOWN_COLOR });
  }
  if (hiddenVersions.length > 0) {
    series.push({ key: "firmware-other", label: "Other", versions: hiddenVersions, color: OTHER_COLOR });
  }
  return series;
};
