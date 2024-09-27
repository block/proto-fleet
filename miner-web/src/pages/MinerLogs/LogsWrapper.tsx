import { usePoll, useSystemLogs } from "api";

import Logs from "./Logs";

const LogsWrapper = () => {
  const { data: logsData, fetchData: fetchLogs } = useSystemLogs();

  usePoll({
    fetchData: () =>
      fetchLogs({
        lines: 1000,
      }),
    poll: true,
    pollIntervalMs: 10000,
  });

  return <Logs logsData={logsData} />;
};

export default LogsWrapper;
