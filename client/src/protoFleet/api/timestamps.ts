import type { Timestamp } from "@bufbuild/protobuf/wkt";

export function timestampToIsoString(timestamp?: Timestamp): string | undefined {
  if (!timestamp) {
    return undefined;
  }

  const date = new Date(Number(timestamp.seconds) * 1000 + Math.floor(timestamp.nanos / 1_000_000));
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString();
}
