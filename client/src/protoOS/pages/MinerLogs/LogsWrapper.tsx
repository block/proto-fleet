import { useCallback } from "react";
import { usePoll, useSystemLogs } from "@/protoOS/api";
import type { SystemLogs } from "@/protoOS/api/adapters/types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import ControllerLogs from "@/protoOS/features/controllerLogs/ControllerLogs";
import Logs from "@/shared/components/Logs";

const MAX_LOG_LINES = 10000;
const POLL_LOG_LINES = 1000;
const POLL_INTERVAL_MS = 10000;

const MinerSystemLogs = () => {
  const { data: logsData, fetchData: fetchLogs } = useSystemLogs();

  const fetchMaxLogs = useCallback(async (): Promise<SystemLogs | undefined> => {
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

const LogsWrapper = () => {
  const { adapter } = useMinerHosting();
  // CT1 controllers expose structured, bounded logs and an audit trail.
  return adapter?.getControllerLogs ? <ControllerLogs adapter={adapter} /> : <MinerSystemLogs />;
};

export default LogsWrapper;
