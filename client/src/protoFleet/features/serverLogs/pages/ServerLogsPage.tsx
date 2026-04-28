import { useCallback, useState } from "react";
import { timestampDate } from "@bufbuild/protobuf/wkt";

import { serverLogClient } from "@/protoFleet/api/clients";
import { type LogEntry, LogLevel } from "@/protoFleet/api/generated/serverlog/v1/serverlog_pb";
import { type LogsResponseLogs } from "@/protoOS/api/generatedApi";
import Logs from "@/protoOS/pages/MinerLogs/Logs";
import Header from "@/shared/components/Header";
import { usePoll } from "@/shared/hooks/usePoll";

// How often the page polls for new entries. 5s mirrors the cadence of
// other live-tailing views in the app and keeps server load trivial.
const POLL_INTERVAL_MS = 5000;

// Per-poll cap; the server's ring buffer is bounded at 1000 today, so
// asking for 1000 is a safe ceiling that gets us the full buffer when
// it's full and just what's there when it isn't.
const POLL_LIMIT = 1000;

// Hard upper bound used by the CSV export ("Export" button), which is
// served by fetchMaxLogs. The proto's validate rule caps `limit` at 5000.
const MAX_LIMIT = 5000;

/**
 * Level token strings exactly matching MinerLogs/constants.ts so the
 * upstream `formatLogs()` parser recognizes them via `string.split()`.
 *
 * The trailing space on INFO and WARN is significant — it preserves the
 * 5-char column width the regex relies on. UNSPECIFIED falls through to
 * INFO so an unset enum doesn't render as an unparseable line.
 */
const LEVEL_TOKEN: Record<LogLevel, string> = {
  [LogLevel.UNSPECIFIED]: "INFO ",
  [LogLevel.DEBUG]: "DEBUG",
  [LogLevel.INFO]: "INFO ",
  [LogLevel.WARN]: "WARN ",
  [LogLevel.ERROR]: "ERROR",
};

/**
 * Convert a single LogEntry from the wire into the line string format
 * MinerLogs/Logs.tsx already knows how to parse and render. The shape is:
 *
 *   `proto-fleet: <ts> | <LEVEL> | <source> <message> [k=v ...]`
 *
 * The leading `proto-fleet:` mimics the syslog-style program prefix
 * (mcdd in the miner case) so the parser's `prefix.split(": ")[1]` path
 * cleanly extracts the timestamp.
 */
function entryToLine(entry: LogEntry): string {
  const ts = entry.time ? timestampDate(entry.time).toISOString().replace("T", " ") : "";
  const level = LEVEL_TOKEN[entry.level] ?? "INFO ";
  const source = entry.source || "fleetd";
  const attrSuffix = entry.attrs.length ? " " + entry.attrs.map((a) => `${a.key}=${a.value}`).join(" ") : "";
  return `proto-fleet: ${ts} | ${level} | ${source} ${entry.message}${attrSuffix}`;
}

/**
 * ServerLogsPage tails fleetd's slog ring buffer and renders it through
 * the existing MinerLogs presentational component, so the protoFleet
 * server-logs view shares the same visual language and behavior (filters,
 * search, follow-tail, CSV export) as the per-miner logs page.
 *
 * The shape mismatch (Connect-RPC structured records vs. the miner's
 * string lines) is bridged in `entryToLine` above.
 */
const ServerLogsPage = () => {
  const [logsData, setLogsData] = useState<LogsResponseLogs>();

  const fetchLogs = useCallback(async (limit: number): Promise<LogsResponseLogs | undefined> => {
    try {
      const response = await serverLogClient.listServerLogs({
        minLevel: LogLevel.UNSPECIFIED,
        searchText: "",
        sinceId: 0n,
        limit,
      });
      const data: LogsResponseLogs = {
        content: response.entries.map(entryToLine),
        lines: response.entries.length,
      };
      setLogsData(data);
      return data;
    } catch (err) {
      // Swallow here — Logs.tsx interprets `undefined` logsData as a
      // loading state and the next poll will retry. Surfacing the error
      // to the user would require restructuring the upstream component
      // and we're explicitly reusing it as-is.
      console.error("Failed to fetch server logs", err);
      return undefined;
    }
  }, []);

  const fetchMaxLogs = useCallback(() => fetchLogs(MAX_LIMIT), [fetchLogs]);

  usePoll({
    fetchData: async () => {
      await fetchLogs(POLL_LIMIT);
    },
    poll: true,
    pollIntervalMs: POLL_INTERVAL_MS,
  });

  return (
    <>
      {/*
        Sticky "Server Logs" page heading. The height (100px on phone,
        60px on laptop) is deliberately matched to the offset that
        Logs.tsx uses for its sticky search bar — `sticky top-[100px]
        laptop:top-[60px]`. Without a sibling heading of that height,
        the search bar sticks below an empty band where log lines scroll
        through visibly. This div fills that band with the page title
        and a solid `bg-surface-base` so nothing bleeds through. z-20
        keeps it above the search bar (z-10) when both are pinned.
      */}
      <div
        className={
          "sticky top-0 z-20 flex h-[100px] items-end bg-surface-base px-4 pb-3 " +
          "laptop:h-[60px] laptop:items-center laptop:px-6 laptop:pb-0"
        }
      >
        <Header title="Server Logs" titleSize="text-heading-300" />
      </div>
      <Logs logsData={logsData} fetchMaxLogs={fetchMaxLogs} />
    </>
  );
};

export default ServerLogsPage;
