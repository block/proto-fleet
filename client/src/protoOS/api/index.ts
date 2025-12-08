import { useCoolingStatus } from "./hooks/useCoolingStatus";
import { useCreatePools } from "./hooks/useCreatePools";
import { useEditPool } from "./hooks/useEditPool";
import { useErrors } from "./hooks/useErrors";
import { useFirmwareUpdate } from "./hooks/useFirmwareUpdate";
import { useHardware } from "./hooks/useHardware";
import { useHashboards } from "./hooks/useHashboards";
import { useHashboardStatus } from "./hooks/useHashboardStatus";
import { useLogin } from "./hooks/useLogin";
import { useMiningStart } from "./hooks/useMiningStart";
import { useMiningStatus } from "./hooks/useMiningStatus";
import { useMiningStop } from "./hooks/useMiningStop";
import { useMiningTarget } from "./hooks/useMiningTarget";
import { useNetworkInfo } from "./hooks/useNetworkInfo";
import { usePassword } from "./hooks/usePassword";
import { type FetchPoolsInfoProps, usePoolsInfo } from "./hooks/usePoolsInfo";
import { useRefresh } from "./hooks/useRefresh";
import { useSystemInfo } from "./hooks/useSystemInfo";
import { useSystemLogs } from "./hooks/useSystemLogs";
import { useSystemReboot } from "./hooks/useSystemReboot";
import { useSystemStatus } from "./hooks/useSystemStatus";
import { useTelemetry } from "./hooks/useTelemetry";
import { type TestConnectionProps, useTestConnection } from "./hooks/useTestConnection";
import { useTimeSeries } from "./hooks/useTimeSeries";

import { usePoll } from "@/shared/hooks/usePoll";

export {
  useCoolingStatus,
  useCreatePools,
  useEditPool,
  useErrors,
  useFirmwareUpdate,
  useHashboards,
  useHashboardStatus,
  useHardware,
  useLogin,
  useMiningStart,
  useMiningStatus,
  useMiningStop,
  useMiningTarget,
  useNetworkInfo,
  usePassword,
  usePoll,
  usePoolsInfo,
  useRefresh,
  useSystemInfo,
  useSystemLogs,
  useSystemReboot,
  useSystemStatus,
  useTelemetry,
  useTestConnection,
  useTimeSeries,
  type TestConnectionProps,
  type FetchPoolsInfoProps,
};
