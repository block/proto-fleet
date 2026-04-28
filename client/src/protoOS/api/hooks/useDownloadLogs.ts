import { useCallback } from "react";
import { useSystemLogs } from "./useSystemLogs";
import { downloadLogs as downloadLogsUtil, formatLogsToCSV } from "@/shared/components/Logs/utility";
import { getFileName } from "@/shared/utils/utility";

export const useDownloadLogs = () => {
  const { fetchData } = useSystemLogs();

  const downloadLogs = useCallback(async () => {
    const logsResponse = await fetchData({ lines: 10000 });

    if (logsResponse?.content) {
      const csvData = formatLogsToCSV(logsResponse.content);
      downloadLogsUtil(csvData, getFileName("miner-logs", "csv"));
    }
  }, [fetchData]);

  return { downloadLogs };
};
