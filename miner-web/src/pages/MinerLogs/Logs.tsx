import { useEffect, useRef, useState } from "react";
import clsx from "clsx";

import { padLeft } from "common/utils/stringUtils";

import { LogInfo } from "./types";

interface LogsProps {
  logs: LogInfo[];
}

const Logs = ({ logs }: LogsProps) => {
  const [initPage, setInitPage] = useState(false);
  const messagesEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (logs.length) {
      // on first load of the logs, scroll to bottom
      if (!initPage) {
        setInitPage(true);
        messagesEndRef.current?.scrollIntoView({ behavior: "instant" });
      }
    }
  }, [logs, initPage]);

  return (
    <div className="bg-[#191919] font-mono text-mono-text-50 font-light text-text-contrast p-4">
      {logs.length
        ? logs.map((log, index) => {
            const line = padLeft(index + 1, 3);
            return (
              <div
                key={line}
                className={clsx("flex pl-4 leading-6 mb-1", {
                  "border-l-[2px] pl-[14px]":
                    log.isError || log.isWarning || log.isDebug,
                  "text-text-warning border-border-text-warning": log.isWarning,
                  "text-text-critical border-border-text-critical": log.isError,
                  "text-intent-info-fill border-border-intent-info-fill":
                    log.isDebug,
                })}
              >
                <div className="mr-10 text-text-contrast/30">{line}</div>
                <div>
                  {log.timestamp && (
                    <div className="text-text-contrast/30">
                      [{log.timestamp}]
                    </div>
                  )}
                  <div
                    ref={index === logs.length - 1 ? messagesEndRef : undefined}
                  >
                    {log.message}
                  </div>
                </div>
              </div>
            );
          })
        : null}
    </div>
  );
};

export default Logs;
