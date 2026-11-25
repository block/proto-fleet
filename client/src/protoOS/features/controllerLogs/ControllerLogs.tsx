import { useMemo } from "react";

import { controllerRecordsSource } from "./controllerRecordsSource";
import type { MinerAdapter } from "@/protoOS/api/adapters/types";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import SharedControllerLogs from "@/shared/features/controllerLogs/ControllerLogs";

export const CONTROLLER_RECORDS_POLL_MS = 10_000;

const ControllerLogs = ({ adapter }: { adapter: MinerAdapter }) => {
  const authRetry = useAuthRetry();
  const source = useMemo(() => controllerRecordsSource(adapter, authRetry), [adapter, authRetry]);
  return <SharedControllerLogs {...source} pollIntervalMs={CONTROLLER_RECORDS_POLL_MS} />;
};

export default ControllerLogs;
