import { MouseEvent, SyntheticEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";

import { logTypes } from "./constants";
import LogBadges from "./LogBadges";
import { LogInfo, logType } from "./types";
import { downloadLogs, formatLogInfoToCSV, formatLogs, formatLogsToCSV, getErrorWarningCount } from "./utility";
import { DismissTiny } from "@/shared/assets/icons";

import Button, { sizes, variants } from "@/shared/components/Button";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Search from "@/shared/components/Search";
import { useClickOutside } from "@/shared/hooks/useClickOutside";
import { padLeft } from "@/shared/utils/stringUtils";
import { getFileName } from "@/shared/utils/utility";

export interface StructuredLogEntry {
  id: string;
  timestamp: string | null;
  level: "debug" | "info" | "warn" | "error";
  message: string;
}

export type LogsData =
  | {
      kind?: "lines";
      content?: string[];
      lines?: number;
      source?: string;
    }
  | {
      kind: "structured";
      entries: StructuredLogEntry[];
      lines?: number;
      source?: string;
    };

interface LogsProps {
  logsData?: LogsData;
  fetchMaxLogs: () => Promise<LogsData | undefined>;
  downloadFilename?: string;
}

const LEVEL_TO_LOG_TYPE: Record<StructuredLogEntry["level"], logType> = {
  debug: logTypes.debug,
  info: logTypes.info,
  warn: logTypes.warn,
  error: logTypes.error,
};

const entryToLogInfo = (entry: StructuredLogEntry): LogInfo => ({
  timestamp: entry.timestamp,
  logType: LEVEL_TO_LOG_TYPE[entry.level],
  message: entry.message,
});

const isStructured = (data: LogsData): data is Extract<LogsData, { kind: "structured" }> => data.kind === "structured";

const Logs = ({ logsData, fetchMaxLogs, downloadFilename = "miner-logs" }: LogsProps) => {
  const [isExporting, setIsExporting] = useState(false);
  const [initPage, setInitPage] = useState(false);
  const [storedLogs, setStoredLogs] = useState<string[]>([]);
  const [storedEntries, setStoredEntries] = useState<StructuredLogEntry[]>([]);
  const [logs, setLogs] = useState<LogInfo[]>([]);
  const [filterByLogType, setFilterByLogType] = useState<logType[]>([]);
  const [focusSearch, setFocusSearch] = useState(false);
  const [errorCount, setErrorCount] = useState(0);
  const [warningCount, setWarningCount] = useState(0);

  const [searchValue, setSearchValue] = useState<string>("");
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const searchBarRef = useRef<HTMLDivElement>(null);
  const [isPinnedToBottom, setIsPinnedToBottom] = useState(true);

  useClickOutside({
    ref: searchBarRef,
    onClickOutside: () => setFocusSearch(false),
  });

  const filteredLogs = useMemo(() => {
    if (!searchValue && !filterByLogType.length) return logs;
    return logs.filter(
      (log) =>
        `${log.timestamp} ${log.message}`.toLowerCase().includes(searchValue.toLowerCase()) &&
        (!filterByLogType.length || filterByLogType.includes(log.logType as logType)),
    );
  }, [searchValue, logs, filterByLogType]);

  useEffect(() => {
    if (filteredLogs.length) {
      // on first load of the logs, scroll to bottom
      if (!initPage && messagesEndRef.current) {
        setInitPage(true);
        messagesEndRef.current?.scrollIntoView({ behavior: "instant" });
        setIsPinnedToBottom(true);
      } else if (messagesEndRef.current && isPinnedToBottom) {
        // auto-scroll to bottom when new logs come in, but only if user is already at the bottom
        messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
      }
    }
    // eslint-disable-next-line react-hooks/set-state-in-effect -- mark init page done on first paint with content so subsequent updates animate instead of jump
  }, [filteredLogs, initPage, isPinnedToBottom]);

  // Reset first-load scroll flag when filters/search change so we don't animate a long scroll
  const [prevFilterByLogType, setPrevFilterByLogType] = useState(filterByLogType);
  const [prevSearchValue, setPrevSearchValue] = useState(searchValue);
  if (prevFilterByLogType !== filterByLogType || prevSearchValue !== searchValue) {
    setPrevFilterByLogType(filterByLogType);
    setPrevSearchValue(searchValue);
    setInitPage(false);
  }

  const formatAndSetLogsData = useCallback(
    (logsDataToSet: string[]) => {
      if (logsDataToSet.length === storedLogs.length) return;
      setStoredLogs(logsDataToSet);

      const { error, warning } = getErrorWarningCount(logsDataToSet);
      setErrorCount(error);
      setWarningCount(warning);

      const formattedLogs = formatLogs(logsDataToSet);
      setLogs(formattedLogs);
    },
    [storedLogs],
  );

  const formatAndSetStructuredEntries = useCallback(
    (entriesToSet: StructuredLogEntry[]) => {
      if (entriesToSet.length === storedEntries.length) return;
      setStoredEntries(entriesToSet);

      let error = 0;
      let warning = 0;
      for (const entry of entriesToSet) {
        if (entry.level === "error") error++;
        else if (entry.level === "warn") warning++;
      }
      setErrorCount(error);
      setWarningCount(warning);

      setLogs(entriesToSet.map(entryToLogInfo));
    },
    [storedEntries],
  );

  useEffect(() => {
    if (!logsData) return;

    if (isStructured(logsData)) {
      if (!logsData.entries.length) return;
      const knownIds = new Set(storedEntries.map((e) => e.id));
      const uniqueEntries = storedEntries.length
        ? logsData.entries.filter((e) => !knownIds.has(e.id))
        : logsData.entries;
      const combinedEntries = [...storedEntries, ...uniqueEntries];
      // eslint-disable-next-line react-hooks/set-state-in-effect -- ingest new entries when upstream logsData changes
      formatAndSetStructuredEntries(combinedEntries);
      return;
    }

    if (logsData.content?.length) {
      // after initial logs are fetched, remove duplicated logs and add them
      const uniqueLogs = storedLogs.length
        ? logsData.content.filter((log) => !storedLogs.find((storedLog) => storedLog === log))
        : logsData.content;

      const combinedLogs = [...storedLogs, ...uniqueLogs];
      // eslint-disable-next-line react-hooks/set-state-in-effect -- ingest new logs when upstream logsData changes
      formatAndSetLogsData(combinedLogs);
    }
  }, [logsData, storedLogs, storedEntries, formatAndSetLogsData, formatAndSetStructuredEntries]);

  const blurSearch = (e: SyntheticEvent) => {
    e.stopPropagation();
    setFocusSearch(false);
  };

  const createToggleFilter = (logType: logType) => {
    return (e: MouseEvent<HTMLDivElement>) => {
      blurSearch(e);
      if (filterByLogType?.includes(logType)) {
        setFilterByLogType((prev) => prev.filter((type) => type !== logType));
      } else {
        setFilterByLogType((prev) => [...prev, logType]);
      }
    };
  };

  const clearSearch = useCallback((e: MouseEvent<HTMLButtonElement>) => {
    setSearchValue("");
    blurSearch(e);
  }, []);

  const handleExportLogs = useCallback(
    async (e: SyntheticEvent) => {
      try {
        e.stopPropagation();
        e.preventDefault();
        blurSearch(e);
        setIsExporting(true);
        const exportLogs = await fetchMaxLogs();
        if (!exportLogs) return;
        let csvData: string[] | undefined;
        if (isStructured(exportLogs)) {
          if (exportLogs.entries.length) {
            csvData = formatLogInfoToCSV(exportLogs.entries.map(entryToLogInfo));
          }
        } else if (exportLogs.content?.length) {
          csvData = formatLogsToCSV(exportLogs.content);
        }
        if (csvData) {
          downloadLogs(csvData, getFileName(downloadFilename, "csv"));
        }
      } catch (error) {
        console.error("Error exporting logs:", error);
      } finally {
        setIsExporting(false);
      }
    },
    [fetchMaxLogs, downloadFilename],
  );

  const handleClickSearchBar = useCallback(() => {
    setFocusSearch(true);
  }, []);

  const noResultsMessage = useMemo(() => {
    if (searchValue) {
      if (filterByLogType.length === 0) return `No results match “${searchValue}”`;
      if (filterByLogType.includes(logTypes.error) && filterByLogType.includes(logTypes.warn)) {
        return `No errors or warnings match “${searchValue}”`;
      }
      if (filterByLogType.includes(logTypes.error)) return `No errors match “${searchValue}”`;
      return `No warnings match “${searchValue}”`;
    }
    if (filterByLogType.includes(logTypes.error) && filterByLogType.includes(logTypes.warn)) {
      return "No errors or warnings found";
    }
    if (filterByLogType.includes(logTypes.error)) return "No errors found";
    return "No warnings found";
  }, [searchValue, filterByLogType]);

  const handleScroll = useCallback(() => {
    const scrollTop = window.pageYOffset || document.documentElement.scrollTop;
    const scrollHeight = document.documentElement.scrollHeight;
    const clientHeight = document.documentElement.clientHeight;
    const distanceFromBottom = scrollHeight - scrollTop - clientHeight;
    // consider within 5px as "at bottom"
    setIsPinnedToBottom(distanceFromBottom < 5);
  }, []);

  useEffect(() => {
    window.addEventListener("scroll", handleScroll);
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initialize isPinnedToBottom from current scroll position on mount
    handleScroll();

    return () => {
      window.removeEventListener("scroll", handleScroll);
    };
  }, [handleScroll]);

  return (
    <>
      {logs.length ? (
        <>
          <div
            className="sticky top-[100px] z-10 h-[58px] bg-surface-base laptop:top-[60px]"
            onClick={handleClickSearchBar}
            ref={searchBarRef}
          >
            <div
              className={clsx(
                "flex items-center border-b-[1px] border-border-5 bg-surface-base p-[15px]",
                "focus-within:border-b-2 focus-within:border-border-primary",
              )}
            >
              <div className="flex grow items-center space-x-4">
                <Search
                  className="bg-surface-base!"
                  onChange={setSearchValue}
                  initValue={searchValue}
                  compact
                  shouldFocus={focusSearch}
                />
              </div>
              <div className="flex items-center space-x-4">
                <LogBadges
                  label={errorCount === 1 ? "error" : "errors"}
                  count={errorCount}
                  testId="logs-error-badge"
                  className={clsx(
                    "text-text-critical",
                    {
                      "border-transparent bg-intent-critical-10": filterByLogType?.includes(logTypes.error),
                    },
                    {
                      "border-intent-critical-10": !filterByLogType?.includes(logTypes.error),
                    },
                  )}
                  selected={filterByLogType?.includes(logTypes.error) || false}
                  onClick={createToggleFilter(logTypes.error)}
                />
                <LogBadges
                  label={warningCount === 1 ? "warning" : "warnings"}
                  count={warningCount}
                  testId="logs-warning-badge"
                  className={clsx(
                    "text-text-warning",
                    {
                      "border-transparent bg-intent-warning-10": filterByLogType?.includes(logTypes.warn),
                    },
                    {
                      "border-intent-warning-10": !filterByLogType?.includes(logTypes.warn),
                    },
                  )}
                  selected={filterByLogType?.includes(logTypes.warn) || false}
                  onClick={createToggleFilter(logTypes.warn)}
                />
                <Button
                  size={sizes.compact}
                  variant={variants.secondary}
                  text="Export"
                  disabled={isExporting}
                  prefixIcon={isExporting ? <ProgressCircular indeterminate /> : undefined}
                  onClick={handleExportLogs}
                />
                {searchValue ? (
                  <Button
                    variant={variants.secondary}
                    size={sizes.compact}
                    prefixIcon={<DismissTiny />}
                    onClick={clearSearch}
                    className="rounded-full!"
                  />
                ) : null}
              </div>
            </div>
          </div>
          <div className="h-[calc(100%-60px-58px)] overflow-y-hidden">
            <div className="p-4 font-mono text-mono-text-50 font-light text-text-primary">
              {filteredLogs.length ? (
                filteredLogs.map((log, index) => {
                  const line = padLeft(index + 1, 4);
                  const isDebug = log.logType === logTypes.debug;
                  const isError = log.logType === logTypes.error;
                  const isWarning = log.logType === logTypes.warn;
                  const normalizedLogType = isError ? "error" : isWarning ? "warn" : isDebug ? "debug" : "info";
                  return (
                    <div
                      key={line}
                      data-testid="log-row"
                      data-log-type={normalizedLogType}
                      className={clsx("mb-1 flex leading-6", {
                        "ml-[2px] text-text-primary-70": !isError && !isWarning && !isDebug,
                        "-ml-[16px] border-l-[2px] pl-4": isError || isWarning || isDebug,
                        "border-border-text-warning text-text-warning": isWarning,
                        "border-border-text-critical text-text-critical": isError,
                        "border-border-intent-info-fill text-intent-info-fill": isDebug,
                      })}
                    >
                      <div className="mr-10">{line}</div>
                      <div ref={index === filteredLogs.length - 1 ? messagesEndRef : undefined}>
                        {log.timestamp ? <>[{log.timestamp}] </> : null}
                        {log.message}
                      </div>
                    </div>
                  );
                })
              ) : (
                <div
                  data-testid="logs-empty-state"
                  className="flex h-[189px] w-full items-center justify-center rounded-2xl bg-core-primary-5"
                >
                  <div className="font-body text-heading-100 text-text-primary-50">{noResultsMessage}</div>
                </div>
              )}
            </div>
          </div>
        </>
      ) : (
        <div className="flex h-[calc(100vh-65px)] w-full items-center justify-center">
          <ProgressCircular indeterminate />
        </div>
      )}
    </>
  );
};

export default Logs;
