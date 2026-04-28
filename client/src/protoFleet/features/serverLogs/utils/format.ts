import { type Timestamp } from "@bufbuild/protobuf/wkt";

import { type LogAttr } from "@/protoFleet/api/generated/serverlog/v1/serverlog_pb";

/**
 * Renders a protobuf Timestamp as HH:MM:SS.mmm in the user's local time
 * zone. We deliberately drop the date — the log viewer shows recent
 * entries (last few minutes typically), so the date is rarely useful and
 * just steals horizontal space. The full Date is available in the detail
 * modal's `formatTimestampFull`.
 */
export function formatTimestamp(ts: Timestamp): string {
  const date = timestampToDate(ts);
  const hh = String(date.getHours()).padStart(2, "0");
  const mm = String(date.getMinutes()).padStart(2, "0");
  const ss = String(date.getSeconds()).padStart(2, "0");
  const ms = String(date.getMilliseconds()).padStart(3, "0");
  return `${hh}:${mm}:${ss}.${ms}`;
}

/** ISO-8601 with millisecond precision, in the local time zone. */
export function formatTimestampFull(ts: Timestamp): string {
  return timestampToDate(ts).toISOString();
}

/**
 * Renders attrs into a compact preview suffix shown inline with the
 * message (e.g. `{user_id=42, count=3}`). Returns "" for empty input so
 * callers can conditionally append.
 */
export function summarizeAttrs(attrs: LogAttr[]): string {
  if (!attrs.length) return "";
  // Limit to keep rows from wrapping — the full set is in the detail view.
  const MAX = 4;
  const parts = attrs.slice(0, MAX).map((a) => `${a.key}=${a.value}`);
  if (attrs.length > MAX) parts.push(`+${attrs.length - MAX} more`);
  return `{${parts.join(", ")}}`;
}

/**
 * protobuf-es Timestamps are { seconds: bigint, nanos: number }. Convert
 * to a JS Date by clamping ns precision to ms (which is all Date supports).
 */
function timestampToDate(ts: Timestamp): Date {
  // bigint seconds * 1000 may exceed Number.MAX_SAFE_INTEGER for far-future
  // values, but slog timestamps are all current-clock and well within range.
  const millis = Number(ts.seconds) * 1000 + Math.floor(ts.nanos / 1_000_000);
  return new Date(millis);
}
