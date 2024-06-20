import { useCallback, useState } from "react";

import { api } from "./api";
import { Error, LogsResponseLogs } from "./types";
import { usePoll } from "./usePoll";

interface UseSystemLogsProps {
  poll?: boolean;
}

const useSystemLogs = ({ poll }: UseSystemLogsProps) => {
  const [data, setData] = useState<LogsResponseLogs>();
  const [error, setError] = useState<Error>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    setPending(true);
    api
      .getSystemLogs({ lines: 500, source: "miner_sw" })
      .then((res) => {
        setData(res?.data["logs"]);
      })
      .catch((err) => {
        setError(err?.error || err);
      })
      .finally(() => {
        setPending(false);
      });
  }, []);

  usePoll({ fetchData, poll, pollIntervalMilliseconds: 5000 });

  return {
    pending,
    error,
    data,
  };
};

export { useSystemLogs };
