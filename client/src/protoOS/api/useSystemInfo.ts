import { useCallback, useMemo, useState } from "react";

import { SystemInfoSysteminfo } from "./types";
import { usePoll } from "@/protoOS/api/usePoll";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

interface UseSystemInfoProps {
  poll?: boolean;
}

interface ProcessedSystemInfo {
  // miner API server
  isWebServerRunning: boolean;
  // MCDD
  isMiningDriverRunning: boolean;
}

const useSystemInfo = ({ poll }: UseSystemInfoProps) => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<SystemInfoSysteminfo>();
  const [processedData, setProcessedData] = useState<ProcessedSystemInfo>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    if (!api) return;

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
        });
      })
      .finally(() => {
        setPending(false);
      });
  }, [api]);

  usePoll({
    fetchData,
    poll,
  });

  return useMemo(
    () => ({ pending, error, data, processedData }),
    [pending, error, data, processedData],
  );
};

export { useSystemInfo };
