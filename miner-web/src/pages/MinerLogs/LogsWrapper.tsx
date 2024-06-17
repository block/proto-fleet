import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

import { useSystemLogs } from "api";

import Button, { sizes, variants } from "components/Button";

import { Dismiss } from "icons";
import { iconSizes } from "icons/constants";

import { mockLogs } from "./constants";
import LogBadges from "./LogBadges";
import Logs from "./Logs";
import { LogInfo } from "./types";
import { formatLogs, getErrorWarningCount, getExportLink, getFileName } from "./utility";

const LogsWrapper = () => {
  const navigate = useNavigate();
  const { data: logsData } = useSystemLogs({ poll: true });
  const [logs, setLogs] = useState<LogInfo[]>([]);
  const [errorCount, setErrorCount] = useState(0);
  const [warningCount, setWarningCount] = useState(0);
  const [exportLink, setExportLink] = useState<string | null>(null);

  useEffect(() => {
    if (logsData?.content) {
      let newLogs;
      // TODO: remove else when mocks moved to swagger
      if (logsData.content[0] === "string") {
        newLogs = mockLogs.content;
      } else {
        newLogs = logsData.content;
      }

      const { error, warning } = getErrorWarningCount(newLogs);
      setErrorCount(error);
      setWarningCount(warning);

      const formattedLogs = formatLogs(newLogs);
      setLogs(formattedLogs);

      const newExportLink = getExportLink([
        "Time,Message",
        ...formattedLogs.map(
          (log) => `${log.timestamp},${log.message.replace(/,/g, " | ")}`
        ),
      ]);
      setExportLink(newExportLink);
    }
  }, [logsData]);

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
        {logs.length ? <Logs logs={logs} /> : null}
      </div>
    </div>
  );
};

export default LogsWrapper;
