import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

import { useSystemLogs } from "api";

import Button, { sizes, variants } from "components/Button";
import Spinner from "components/Spinner";

import { Dismiss } from "icons";
import { iconSizes } from "icons/constants";

import { mockLogs, mockNewLogs } from "./constants";
import LogBadges from "./LogBadges";
import Logs from "./Logs";
import { LogInfo } from "./types";
import {
  formatLogs,
  formatLogType,
  getErrorWarningCount,
  getExportLink,
  getFileName,
} from "./utility";

const LogsWrapper = () => {
  const navigate = useNavigate();
  const { data: logsData } = useSystemLogs({ poll: true });
  const [storedLogs, setStoredLogs] = useState<string[]>([]);
  const [logs, setLogs] = useState<LogInfo[]>([]);
  const [errorCount, setErrorCount] = useState(0);
  const [warningCount, setWarningCount] = useState(0);
  const [exportLink, setExportLink] = useState<string | null>(null);

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
      let newLogs;
      // TODO: remove else when mocks moved to swagger
      if (logsData.content[0] === "string") {
        // if no logs are stored, set initial logs
        newLogs = storedLogs.length ? mockNewLogs.content : mockLogs.content;
      } else {
        newLogs = logsData.content;
      }

      // after initial logs are fetched, remove duplicated logs and add them
      const uniqueLogs = storedLogs.length
        ? newLogs.filter(
            (log) => !storedLogs.find((storedLog) => storedLog === log)
          )
        : newLogs;

      const combinedLogs = [...storedLogs, ...uniqueLogs];
      formatAndSetLogsData(combinedLogs);
    }
  }, [logsData, storedLogs, formatAndSetLogsData]);

  const handleClickDismiss = () => {
    navigate("/");
  };

  return (
    <div className="bg-[#191919] min-h-screen text-text-contrast">
      <div className="h-[65px]">
        <div className="fixed bg-[#191919] w-full">
          <div className="flex items-center p-4 pl-6 border-b-[1px] border-text-contrast/10">
            <div className="flex space-x-4 items-center flex-grow">
              <button onClick={handleClickDismiss}>
                <Dismiss width={iconSizes.small} opacity=".02" />
              </button>
              <div className="text-heading-100">Miner Logs</div>
            </div>
            <div className="space-x-4 flex items-center">
              <LogBadges
                label="Errors"
                count={errorCount}
                wrapperClassName="shadow-[0_0_1px_0_rgba(255,0,40,0.37)] bg-intent-critical-fill/10 text-intent-critical-fill"
                className="bg-intent-critical-fill/20"
              />
              <LogBadges
                label="Warnings"
                count={warningCount}
                wrapperClassName="shadow-[0_0_1px_0_rgba(255,73,0,0.37)] bg-intent-warning-fill/10 text-intent-warning-fill"
                className="bg-intent-warning-fill/20"
              />
              <Button
                size={sizes.compact}
                variant={variants.primary}
                className="!bg-[#262626]"
              >
                <a href={exportLink || ""} download={`${getFileName()}`}>
                  Export
                </a>
              </Button>
            </div>
          </div>
        </div>
      </div>
      <div className="overflow-y-scroll h-[calc(100%-65px)]">
        {logs.length ? (
          <Logs logs={logs} />
        ) : (
          <div className="flex h-[calc(100vh-65px)] w-full justify-center items-center">
            <Spinner />
          </div>
        )}
      </div>
    </div>
  );
};

export default LogsWrapper;
