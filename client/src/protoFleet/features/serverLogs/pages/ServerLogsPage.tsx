import { useCallback, useRef, useState } from "react";
import { timestampDate } from "@bufbuild/protobuf/wkt";

import { serverLogClient } from "@/protoFleet/api/clients";
import { type LogEntry, LogLevel } from "@/protoFleet/api/generated/serverlog/v1/serverlog_pb";
import { Alert } from "@/shared/assets/icons";
import Callout from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import { type LogsData, type StructuredLogEntry } from "@/shared/components/Logs";
import Logs from "@/shared/components/Logs";
import { usePoll } from "@/shared/hooks/usePoll";

const POLL_INTERVAL_MS = 5000;

const POLL_LIMIT = 1000;

const MAX_LIMIT = 5000;

function levelToString(level: LogLevel): StructuredLogEntry["level"] {
  switch (level) {
    case LogLevel.DEBUG:
      return "debug";
    case LogLevel.WARN:
      return "warn";
    case LogLevel.ERROR:
      return "error";
    default:
      return "info";
  }
}

function entryToStructured(entry: LogEntry): StructuredLogEntry {
  const ts = entry.time ? timestampDate(entry.time).toISOString().replace("T", " ").split(".")[0] : null;
  const source = entry.source || "fleetd";
  const attrSuffix = entry.attrs.length ? " " + entry.attrs.map((a) => `${a.key}=${a.value}`).join(" ") : "";
  return {
    id: entry.id.toString(),
    timestamp: ts,
    level: levelToString(entry.level),
    message: `${source} ${entry.message}${attrSuffix}`,
  };
}

const ServerLogsPage = () => {
  const [logsData, setLogsData] = useState<LogsData>();
  const [fetchError, setFetchError] = useState<string | null>(null);

  const sinceIdRef = useRef<bigint>(0n);

  const fetchLogs = useCallback(
    async (limit: number, sinceId: bigint): Promise<{ data: LogsData; latestId: bigint }> => {
      const response = await serverLogClient.listServerLogs({
        minLevel: LogLevel.UNSPECIFIED,
        searchText: "",
        sinceId,
        limit,
      });
      return {
        data: {
          kind: "structured",
          entries: response.entries.map(entryToStructured),
          lines: response.entries.length,
        },
        latestId: response.latestId,
      };
    },
    [],
  );

  const fetchMaxLogs = useCallback(async (): Promise<LogsData | undefined> => {
    try {
      const result = await fetchLogs(MAX_LIMIT, 0n);
      return result.data;
    } catch (err) {
      console.error("Failed to fetch server logs for export", err);
      return undefined;
    }
  }, [fetchLogs]);

  usePoll({
    fetchData: async () => {
      try {
        const result = await fetchLogs(POLL_LIMIT, sinceIdRef.current);
        setFetchError(null);
        if (result.latestId > sinceIdRef.current) {
          sinceIdRef.current = result.latestId;
        }
        setLogsData(result.data);
      } catch (err) {
        console.error("Failed to fetch server logs", err);
        setFetchError(err instanceof Error ? err.message : String(err));
      }
    },
    poll: true,
    pollIntervalMs: POLL_INTERVAL_MS,
  });

  return (
    <>
      <div
        className={
          "sticky top-0 z-20 flex h-[100px] items-end bg-surface-base px-4 pb-3 " +
          "laptop:h-[60px] laptop:items-center laptop:px-6 laptop:pb-0"
        }
      >
        <Header title="Server Logs" titleSize="text-heading-300" />
      </div>
      {fetchError ? (
        <Callout
          className="mx-4 mb-3 laptop:mx-6"
          intent="danger"
          prefixIcon={<Alert />}
          title="Couldn't load server logs"
          subtitle={fetchError}
          testId="server-logs-error"
        />
      ) : null}
      <Logs logsData={logsData} fetchMaxLogs={fetchMaxLogs} downloadFilename="server-logs" />
    </>
  );
};

export default ServerLogsPage;
