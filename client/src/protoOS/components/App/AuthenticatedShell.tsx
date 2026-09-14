import { useEffect } from "react";

import { useErrors } from "@/protoOS/api/hooks/useErrors";
import { useFirmwareUpdate } from "@/protoOS/api/hooks/useFirmwareUpdate";
import { useNetworkInfo } from "@/protoOS/api/hooks/useNetworkInfo";
import { usePoolsInfo } from "@/protoOS/api/hooks/usePoolsInfo";

interface AuthenticatedShellProps {
  reloadSystemInfo: () => void;
}

const AuthenticatedShell = ({ reloadSystemInfo }: AuthenticatedShellProps) => {
  useNetworkInfo({ poll: false });
  useErrors({ poll: true, pollIntervalMs: 15 * 1000 });
  usePoolsInfo({ poll: true, pollIntervalMs: 15 * 1000 });

  // Per-ASIC hashboard status is fetched lazily by the views that consume it
  // (HashboardTemperature for one board, Diagnostics for the board grid) rather
  // than eagerly here — a CT1 container is up to 864 boards, and no top-level
  // view renders ASIC-level data, so the shell must not fan out on every session.

  const { checkFirmwareUpdate } = useFirmwareUpdate();
  useEffect(() => {
    const checkForFirmwareUpdates = () => {
      checkFirmwareUpdate()
        .then(() => {
          reloadSystemInfo();
        })
        .catch((error) => {
          console.error("Error checking for firmware updates:", error);
        });
    };

    checkForFirmwareUpdates();
  }, [checkFirmwareUpdate, reloadSystemInfo]);

  return null;
};

export default AuthenticatedShell;
