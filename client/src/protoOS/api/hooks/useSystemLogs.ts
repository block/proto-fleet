import { useCallback, useMemo, useState } from "react";

import { LogsResponseLogs } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

interface UseSystemLogsProps {
  lines: number;
}

const useSystemLogs = () => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<LogsResponseLogs>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(
    async ({ lines }: UseSystemLogsProps) => {
      if (!api) return;

      setPending(true);
      let logs: LogsResponseLogs | undefined;
      await api
        .getSystemLogs({ lines, source: "miner_sw" })
        .then((res) => {
          logs = res?.data["logs"];
          setData(logs);
        })
        .catch((err) => {
          setError(err?.error?.message ?? "An error occurred");
        })
        .finally(() => {
          setPending(false);
        });
      return logs;
    },
    [api],
  );

  const response = useMemo(() => ({ fetchData, pending, error, data }), [fetchData, pending, error, data]);

  return response;
};

export { useSystemLogs };
