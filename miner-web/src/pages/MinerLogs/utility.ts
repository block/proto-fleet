import { padLeft } from "common/utils/stringUtils";
import { tags } from "./constants";
import { LogInfo } from "./types";

const formatLog = (
  log: string,
  tag: (typeof tags)[keyof typeof tags]
): LogInfo => {
  const info = log.split(tag);
  const timestamp = info[0].split(": ")?.[1];
  const timeWithoutMs = timestamp?.split(".")?.[0] || timestamp;
  const message = info[1];

  return {
    timestamp: timeWithoutMs && message ? timeWithoutMs : null,
    message: timeWithoutMs && message ? message : log,
  };
};

export const formatLogs = (logs: string[]) => {
  return logs.map((log) => {
    const isWarning = log.includes(tags.warn);
    const isError = log.includes(tags.error);
    const isInfo = log.includes(tags.info);
    const isDebug = log.includes(tags.debug);

    let info: LogInfo = { timestamp: null, message: log };
    if (isError) {
      info = formatLog(log, tags.error);
    } else if (isWarning) {
      info = formatLog(log, tags.warn);
    } else if (isDebug) {
      info = formatLog(log, tags.debug);
    } else if (isInfo) {
      info = formatLog(log, tags.info);
    }

    return {
      ...info,
      isDebug,
      isError,
      isInfo,
      isWarning,
    };
  });
};

export const getErrorWarningCount = (logs: string[]) => {
  let error = 0;
  let warning = 0;
  logs.forEach((log) => {
    if (log.includes(tags.error)) {
      error++;
    } else if (log.includes(tags.warn)) {
      warning++;
    }
  });
  return { error, warning };
};

export const getExportLink = (items: string[]) => {
  // Convert Object to JSON
  const jsonObject = JSON.stringify(items)
    .replace(/^\["/, "")
    .replace(/"]$/, "")
    .replace(/","/g, "\n");
  const csvContent = `data:text/csv;charset=utf-8,${jsonObject}`;
  const encodedURI = encodeURI(csvContent);
  return encodedURI;
};

export const getFileName = () => {
  const date = new Date();
  const year = date.getFullYear();
  const month = padLeft(date.getMonth() + 1, 2);
  const day = padLeft(date.getDate(), 2);
  const hours = padLeft(date.getHours(), 2);
  const minutes = padLeft(date.getMinutes(), 2);
  const seconds = padLeft(date.getSeconds(), 2);
  const formattedDate = `${year}-${month}-${day}_${hours}-${minutes}-${seconds}`;
  return `miner-logs-${formattedDate}.csv`;
};
