import { useCallback } from "react";
import { usePoll, useSystemLogs } from "@/protoOS/api";
import { LogsResponseLogs } from "@/protoOS/api/generatedApi";
import Logs from "@/shared/components/Logs";

const MAX_LOG_LINES = 10000;
const POLL_LOG_LINES = 1000;
const POLL_INTERVAL_MS = 10000;

const LogsWrapper = () => {
  const { data: logsData, fetchData: fetchLogs } = useSystemLogs();

  const fetchMaxLogs = useCallback(async (): Promise<LogsResponseLogs | undefined> => {
    return await fetchLogs({ lines: MAX_LOG_LINES });
  }, [fetchLogs]);

  usePoll({
    fetchData: async () => {
      await fetchLogs({
        lines: POLL_LOG_LINES,
      });
    },
    poll: true,
    pollIntervalMs: POLL_INTERVAL_MS,
  });

  return <Logs logsData={logsData} fetchMaxLogs={fetchMaxLogs} />;
};

export default LogsWrapper;
