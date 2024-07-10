import { useCallback, useState } from "react";

import { api } from "./api";
import { LogsResponseLogs } from "./types";

interface UseSystemLogsProps {
  lines: number;
}

const useSystemLogs = () => {
  const [data, setData] = useState<LogsResponseLogs>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(async ({ lines }: UseSystemLogsProps) => {
    setPending(true);
    let logs: LogsResponseLogs | undefined;
    await api
      .getSystemLogs({ lines, source: "miner_sw" })
      .then((res) => {
        logs = res?.data["logs"];
        setData(logs);
      })
      .catch((err) => {
        setError(err?.error?.message || err);
      })
      .finally(() => {
        setPending(false);
      });
    return logs;
  }, []);

  return {
    fetchData,
    pending,
    error,
    data,
  };
};

export { useSystemLogs };
