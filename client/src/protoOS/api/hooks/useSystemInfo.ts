import { useCallback, useMemo, useRef, useState } from "react";

import { usePoll } from "./usePoll";
import { SystemInfoSysteminfo } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

interface UseSystemInfoProps {
  poll?: boolean;
  pollIntervalMs?: number;
}

interface ProcessedSystemInfo {
  // miner API server
  isWebServerRunning: boolean;
  // MCDD
  isMiningDriverRunning: boolean;
  hasFirmwareUpdate: boolean;
}

/**
 * Do NOT use this hook directly.
 *
 * Instead, use the centralized SystemContext:
 *   import { useSystemContext } from "@/protoOS/contexts/SystemContext";
 *
 * This hook is wrapped by the SystemContextProvider to ensure a single polling instance
 * and consistent system info data across the app.
 *
 * See SystemContext documentation for details and migration instructions.
 */

const useSystemInfo = ({ poll, pollIntervalMs }: UseSystemInfoProps) => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<SystemInfoSysteminfo>();
  const [processedData, setProcessedData] = useState<ProcessedSystemInfo>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);
  const isFetchingRef = useRef<boolean>(false);

  const fetchData = useCallback(() => {
    if (!api || isFetchingRef.current) return;

    isFetchingRef.current = true;
    setPending(true);

    api
      .getSystemInfo()
      .then((res) => {
        const responseData = res?.data["system-info"];
        setData(responseData);

        if (responseData === undefined) {
          // no data to examine the state of the system
          setProcessedData({
            isWebServerRunning: false,
            isMiningDriverRunning: false,
            hasFirmwareUpdate: false,
          });
        } else {
          let isMiningDriverRunning = true;
          // look for error message
          const miningDriverSwName = responseData.mining_driver_sw?.name;
          if (
            miningDriverSwName === undefined ||
            /tcp connect error: Connection refused|Failed to connect to MinerDataApiClient/.test(
              miningDriverSwName,
            )
          ) {
            // service name not found or indicates that the connection to the MCDD cannot be established
            isMiningDriverRunning = false;
          }

          setProcessedData({
            isWebServerRunning: true,
            isMiningDriverRunning: isMiningDriverRunning,
            hasFirmwareUpdate:
              responseData.sw_update_status?.status === "available",
          });
        }
      })
      .catch((err) => {
        setError(err?.error?.message ?? err);
        // error response means the web server is not running
        // we don't know the state of the MCDD, however we don't care since we cannot reach it without the server
        setProcessedData({
          isWebServerRunning: false,
          isMiningDriverRunning: false,
          hasFirmwareUpdate: false,
        });
      })
      .finally(() => {
        isFetchingRef.current = false;
        setPending(false);
      });
  }, [api]);

  const reload = useCallback(() => {
    if (isFetchingRef.current) return;
    fetchData();
  }, [fetchData]);

  usePoll({
    fetchData: reload,
    poll,
    pollIntervalMs,
  });

  return useMemo(
    () => ({ pending, error, data, processedData, reload }),
    [pending, error, data, processedData, reload],
  );
};

export { useSystemInfo };
