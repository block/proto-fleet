import { MouseEvent, useCallback, useEffect, useRef, useState } from "react";
import clsx from "clsx";

import { logTypes } from "./constants";
import LogBadges from "./LogBadges";
import { LogInfo, logType } from "./types";
import {
  formatLogs,
  formatLogType,
  getErrorWarningCount,
  getExportLink,
} from "./utility";
import { LogsResponseLogs } from "@/protoOS/api/types";
import { DismissTiny } from "@/shared/assets/icons";

import Button, { sizes, variants } from "@/shared/components/Button";
import Search from "@/shared/components/Search";
import Spinner from "@/shared/components/Spinner";
import { useClickOutside } from "@/shared/hooks/useClickOutside";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";
import { padLeft } from "@/shared/utils/stringUtils";
import { getFileName } from "@/shared/utils/utility";

interface LogsProps {
  logsData?: LogsResponseLogs;
}

const Logs = ({ logsData }: LogsProps) => {
  const [initPage, setInitPage] = useState(false);
  const [storedLogs, setStoredLogs] = useState<string[]>([]);
  const [logs, setLogs] = useState<LogInfo[]>([]);
  const [filteredLogs, setFilteredLogs] = useState<LogInfo[]>([]);
  const [filterByLogType, setFilterByLogType] = useState<logType>();
  const [focusSearch, setFocusSearch] = useState(false);
  const [errorCount, setErrorCount] = useState(0);
  const [warningCount, setWarningCount] = useState(0);
  const [exportLink, setExportLink] = useState<string | null>(null);
  const [searchValue, setSearchValue] = useState<string>("");
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const searchBarRef = useRef<HTMLDivElement>(null);

  const { isPhone, isTablet } = useWindowDimensions();

  useClickOutside({
    ref: searchBarRef,
    onClickOutside: () => setFocusSearch(false),
  });

  useEffect(() => {
    if (filteredLogs.length) {
      // on first load of the logs, scroll to bottom
      if (!initPage && messagesEndRef.current) {
        setInitPage(true);
        messagesEndRef.current?.scrollIntoView({ behavior: "instant" });
      }
    }
  }, [filteredLogs, initPage]);

  const updateFilteredLogs = useCallback(() => {
    let newLogs = logs;
    if (searchValue || filterByLogType) {
      const newFilteredLogs = logs.filter(
        (log) =>
          `${log.timestamp} ${log.message}`
            .toLowerCase()
            .includes(searchValue.toLowerCase()) &&
          (!filterByLogType || log.logType === filterByLogType),
      );
      newLogs = newFilteredLogs;
    }
    setFilteredLogs(newLogs);

    const newExportLink = getExportLink([
      "Time,Type,Message",
      ...newLogs.map(
        (log) =>
          `${log.timestamp},${formatLogType(log.logType)},${log.message.replace(/,/g, " | ")}`,
      ),
    ]);
    setExportLink(newExportLink);
  }, [searchValue, logs, filterByLogType]);

  useEffect(() => {
    updateFilteredLogs();
  }, [updateFilteredLogs]);

  const formatAndSetLogsData = useCallback(
    (logsDataToSet: string[]) => {
      if (logsDataToSet.length === storedLogs.length) return;
      setStoredLogs(logsDataToSet);

      const { error, warning } = getErrorWarningCount(logsDataToSet);
      setErrorCount(error);
      setWarningCount(warning);

      const formattedLogs = formatLogs(logsDataToSet);
      setLogs(formattedLogs);
      updateFilteredLogs();
    },
    [storedLogs, updateFilteredLogs],
  );

  useEffect(() => {
    if (logsData?.content?.length) {
      // after initial logs are fetched, remove duplicated logs and add them
      const uniqueLogs = storedLogs.length
        ? logsData.content.filter(
            (log) => !storedLogs.find((storedLog) => storedLog === log),
          )
        : logsData.content;

      const combinedLogs = [...storedLogs, ...uniqueLogs];
      formatAndSetLogsData(combinedLogs);
    }
  }, [logsData, storedLogs, formatAndSetLogsData]);

  const blurSearch = (e: MouseEvent<HTMLElement>) => {
    e.stopPropagation();
    setFocusSearch(false);
  };

  const toggleFilterErrorLogs = useCallback(
    (e: MouseEvent<HTMLDivElement>) => {
      blurSearch(e);
      if (filterByLogType === logTypes.error) {
        setFilterByLogType(undefined);
      } else {
        setFilterByLogType(logTypes.error);
      }
    },
    [filterByLogType],
  );

  const toggleFilterWarningLogs = useCallback(
    (e: MouseEvent<HTMLDivElement>) => {
      blurSearch(e);
      if (filterByLogType === logTypes.warn) {
        setFilterByLogType(undefined);
      } else {
        setFilterByLogType(logTypes.warn);
      }
    },
    [filterByLogType],
  );

  const clearSearch = useCallback((e: MouseEvent<HTMLButtonElement>) => {
    setSearchValue("");
    blurSearch(e);
  }, []);

  const handleClickSearchBar = useCallback(() => {
    setFocusSearch(true);
  }, []);

  return (
    <>
      {logs.length ? (
        <>
          <div
            className={clsx("fixed h-[58px] -mt-[58px] bg-surface-base", {
              "w-full": isPhone || isTablet,
              "w-[calc(100%-240px)]": !isPhone && !isTablet,
            })}
            onClick={handleClickSearchBar}
            ref={searchBarRef}
          >
            <div
              className={clsx(
                "flex items-center p-[15px] border-b-[1px] border-border-5",
                "focus-within:border-b-2 focus-within:border-border-primary",
              )}
            >
              <div className="flex space-x-4 items-center grow">
                <Search
                  className="bg-surface-base!"
                  onChange={setSearchValue}
                  initValue={searchValue}
                  compact
                  shouldFocus={focusSearch}
                />
              </div>
              <div className="space-x-4 flex items-center">
                <LogBadges
                  label={errorCount === 1 ? "error" : "errors"}
                  count={errorCount}
                  className={clsx(
                    "text-text-critical",
                    {
                      "bg-intent-critical-10 border-transparent":
                        filterByLogType === logTypes.error,
                    },
                    {
                      "border-intent-critical-10":
                        filterByLogType !== logTypes.error,
                    },
                  )}
                  selected={filterByLogType === logTypes.error}
                  onClick={toggleFilterErrorLogs}
                />
                <LogBadges
                  label={warningCount === 1 ? "warning" : "warnings"}
                  count={warningCount}
                  className={clsx(
                    "text-text-warning",
                    {
                      "bg-intent-warning-10 border-transparent":
                        filterByLogType === logTypes.warn,
                    },
                    {
                      "border-intent-warning-10":
                        filterByLogType !== logTypes.warn,
                    },
                  )}
                  selected={filterByLogType === logTypes.warn}
                  onClick={toggleFilterWarningLogs}
                />
                <a
                  href={exportLink || ""}
                  download={`${getFileName("miner-logs")}`}
                  onClick={blurSearch}
                  onMouseDown={(e) => e.preventDefault()} // to prevent focus-within trigger on parent
                >
                  <Button
                    size={sizes.compact}
                    variant={variants.secondary}
                    text="Export"
                  />
                </a>
                {searchValue && (
                  <Button
                    variant={variants.secondary}
                    size={sizes.compact}
                    prefixIcon={<DismissTiny />}
                    onClick={clearSearch}
                    className="rounded-full!"
                  />
                )}
              </div>
            </div>
          </div>
          <div className="overflow-y-scroll mt-[58px] h-[calc(100%-60px-58px)]">
            <div className="font-mono text-mono-text-50 font-light text-text-primary p-4">
              {filteredLogs.length ? (
                filteredLogs.map((log, index) => {
                  const line = padLeft(index + 1, 4);
                  const isDebug = log.logType === logTypes.debug;
                  const isError = log.logType === logTypes.error;
                  const isWarning = log.logType === logTypes.warn;
                  return (
                    <div
                      key={line}
                      className={clsx("flex leading-6 mb-1", {
                        "ml-[2px] text-text-primary-70":
                          !isError && !isWarning && !isDebug,
                        "border-l-[2px] -ml-[16px] pl-4":
                          isError || isWarning || isDebug,
                        "text-text-warning border-border-text-warning":
                          isWarning,
                        "text-text-critical border-border-text-critical":
                          isError,
                        "text-intent-info-fill border-border-intent-info-fill":
                          isDebug,
                      })}
                    >
                      <div className="mr-10">{line}</div>
                      <div
                        ref={
                          index === filteredLogs.length - 1
                            ? messagesEndRef
                            : undefined
                        }
                      >
                        {log.timestamp && <>[{log.timestamp}] </>}
                        {log.message}
                      </div>
                    </div>
                  );
                })
              ) : (
                <div className="bg-core-primary-5 w-full h-[189px] flex justify-center items-center rounded-2xl">
                  <div className="font-body text-heading-100 text-text-primary-50">
                    {searchValue &&
                      filterByLogType === undefined &&
                      `No results match “${searchValue}”`}
                    {searchValue &&
                      filterByLogType === logTypes.error &&
                      `No errors match “${searchValue}”`}
                    {searchValue &&
                      filterByLogType === logTypes.warn &&
                      `No warnings match “${searchValue}”`}
                    {!searchValue &&
                      filterByLogType === logTypes.error &&
                      "No errors found"}
                    {!searchValue &&
                      filterByLogType === logTypes.warn &&
                      "No warnings found"}
                  </div>
                </div>
              )}
            </div>
          </div>
        </>
      ) : (
        <div className="flex h-[calc(100vh-65px)] w-full justify-center items-center">
          <Spinner />
        </div>
      )}
    </>
  );
};

export default Logs;
