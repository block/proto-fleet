import { logTypes } from "./constants";
import { LogInfo, logType } from "./types";
import { downloadBlob } from "@/shared/utils/utility";

const formatLog = (log: string, logType: logType): LogInfo => {
  const info = log.split(logType);
  const prefix = info[0];

  // Try mcdd timestamp format: "... mcdd[N]: 2024-06-14 16:01:58.470952 "
  const mcddTimestamp = prefix.split(": ")?.[1]?.trim();
  // Fall back to syslog header: first three whitespace-separated tokens ("Feb 25 08:02:55")
  const rawTimestamp = mcddTimestamp || prefix.trim().split(/\s+/).slice(0, 3).join(" ");
  const timestamp = rawTimestamp?.split(".")?.[0] || rawTimestamp;
  const message = info[1];

  return {
    logType,
    timestamp: timestamp && message ? timestamp : null,
    message: timestamp && message ? message : log,
  };
};

export const formatLogs = (logs: string[]) => {
  return logs.map((log) => {
    const isWarning = log.includes(logTypes.warn);
    const isError = log.includes(logTypes.error);
    const isInfo = log.includes(logTypes.info);
    const isDebug = log.includes(logTypes.debug);

    let info: LogInfo = { timestamp: null, logType: null, message: log };
    if (isError) {
      info = formatLog(log, logTypes.error);
    } else if (isWarning) {
      info = formatLog(log, logTypes.warn);
    } else if (isDebug) {
      info = formatLog(log, logTypes.debug);
    } else if (isInfo) {
      info = formatLog(log, logTypes.info);
    }

    return {
      ...info,
    };
  });
};

export const getErrorWarningCount = (logs: string[]) => {
  let error = 0;
  let warning = 0;
  logs.forEach((log) => {
    if (log.includes(logTypes.error)) {
      error++;
    } else if (log.includes(logTypes.warn)) {
      warning++;
    }
  });
  return { error, warning };
};

export const downloadLogs = (items: string[], filename: string) => {
  const csvContent = items.join("\r\n");
  const blob = new Blob([csvContent], { type: "text/csv;charset=utf-8;" });
  downloadBlob(blob, filename);
};

export const formatLogType = (logType: logType | null) => {
  return logType?.split("|")[1].trim();
};

export const CSV_HEADERS = "Time,Type,Message";

/**
 * Format logs into CSV rows with proper quoting and escaping
 * Follows RFC 4180 CSV format specification
 */
export const formatLogsToCSV = (logs: string[]): string[] => {
  const formattedLogs = formatLogs(logs);
  return [
    CSV_HEADERS,
    ...formattedLogs.map(
      (log) => `${log.timestamp},${formatLogType(log.logType)},"${log.message.replace(/"/g, '""')}"`,
    ),
  ];
};
