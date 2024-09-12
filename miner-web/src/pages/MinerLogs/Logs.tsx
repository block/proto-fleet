import { useCallback, useEffect, useRef, useState } from "react";
import clsx from "clsx";

import { LogsResponseLogs } from "apiTypes";

import { useWindowDimensions } from "common/hooks/useWindowDimensions";
import { padLeft } from "common/utils/stringUtils";

import Button, { sizes, variants } from "components/Button";
import Spinner from "components/Spinner";

import { logTypes } from "./constants";
import LogBadges from "./LogBadges";
import { LogInfo } from "./types";
import {
  formatLogs,
  formatLogType,
  getErrorWarningCount,
  getExportLink,
  getFileName,
} from "./utility";

interface LogsProps {
  logsData?: LogsResponseLogs;
}

const Logs = ({ logsData }: LogsProps) => {
  const [initPage, setInitPage] = useState(false);
  const [storedLogs, setStoredLogs] = useState<string[]>([]);
  const [logs, setLogs] = useState<LogInfo[]>([]);
  const [errorCount, setErrorCount] = useState(0);
  const [warningCount, setWarningCount] = useState(0);
  const [exportLink, setExportLink] = useState<string | null>(null);
  const messagesEndRef = useRef<HTMLDivElement>(null);

  const { isPhone, isTablet } = useWindowDimensions();

  useEffect(() => {
    if (logs.length) {
      // on first load of the logs, scroll to bottom
      if (!initPage) {
        setInitPage(true);
        messagesEndRef.current?.scrollIntoView({ behavior: "instant" });
      }
    }
  }, [logs, initPage]);

  const formatAndSetLogsData = useCallback(
    (logsDataToSet: string[]) => {
      if (logsDataToSet.length === storedLogs.length) return;
      setStoredLogs(logsDataToSet);

      const { error, warning } = getErrorWarningCount(logsDataToSet);
      setErrorCount(error);
      setWarningCount(warning);

      const formattedLogs = formatLogs(logsDataToSet);
      setLogs(formattedLogs);

      const newExportLink = getExportLink([
        "Time,Type,Message",
        ...formattedLogs.map(
          (log) =>
            `${log.timestamp},${formatLogType(log.logType)},${log.message.replace(/,/g, " | ")}`
        ),
      ]);
      setExportLink(newExportLink);
    },
    [storedLogs]
  );

  useEffect(() => {
    if (logsData?.content?.length) {
      // after initial logs are fetched, remove duplicated logs and add them
      const uniqueLogs = storedLogs.length
        ? logsData.content.filter(
            (log) => !storedLogs.find((storedLog) => storedLog === log)
          )
        : logsData.content;

      const combinedLogs = [...storedLogs, ...uniqueLogs];
      formatAndSetLogsData(combinedLogs);
    }
  }, [logsData, storedLogs, formatAndSetLogsData]);

  return (
    <div>
      <div
        className={clsx("fixed h-[58px] -mt-[58px] bg-surface-base", {
          "w-full": isPhone || isTablet,
          "w-[calc(100%-240px)]": !isPhone && !isTablet,
        })}
      >
        <div className="flex items-center p-[15px] border-b-[1px] border-border-primary/5">
          <div className="flex space-x-4 items-center flex-grow">
            {/* TODO: BTCM-1693 - add search bar here */}
          </div>
          <div className="space-x-4 flex items-center">
            <LogBadges
              label={errorCount === 1 ? "error" : "errors"}
              count={errorCount}
              className="border-intent-critical-fill/10 text-text-critical"
            />
            <LogBadges
              label={warningCount === 1 ? "warning" : "warnings"}
              count={warningCount}
              className="border-intent-warning-fill/10 text-text-warning"
            />
            <Button size={sizes.compact} variant={variants.secondary}>
              <a href={exportLink || ""} download={`${getFileName()}`}>
                Export
              </a>
            </Button>
          </div>
        </div>
      </div>
      <div className="overflow-y-scroll mt-[58px] h-[calc(100%-60px-58px)]">
        {logs.length ? (
          <div className="font-mono text-mono-text-50 font-light text-text-primary p-4">
            {logs.length
              ? logs.map((log, index) => {
                  const line = padLeft(index + 1, 4);
                  const isDebug = log.logType === logTypes.debug;
                  const isError = log.logType === logTypes.error;
                  const isWarning = log.logType === logTypes.warn;
                  return (
                    <div
                      key={line}
                      className={clsx("flex leading-6 mb-1", {
                        "ml-[2px] text-text-primary/70":
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
                          index === logs.length - 1 ? messagesEndRef : undefined
                        }
                      >
                        {log.timestamp && <>[{log.timestamp}] </>}
                        {log.message}
                      </div>
                    </div>
                  );
                })
              : null}
          </div>
        ) : (
          <div className="flex h-[calc(100vh-65px)] w-full justify-center items-center">
            <Spinner />
          </div>
        )}
      </div>
    </div>
  );
};

export default Logs;
