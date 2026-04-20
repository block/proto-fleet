import { useEffect } from "react";

import { useErrors } from "@/protoOS/api/hooks/useErrors";
import { useFirmwareUpdate } from "@/protoOS/api/hooks/useFirmwareUpdate";
import { useHardware } from "@/protoOS/api/hooks/useHardware";
import { useHashboardStatus } from "@/protoOS/api/hooks/useHashboardStatus";
import { useNetworkInfo } from "@/protoOS/api/hooks/useNetworkInfo";
import { usePoolsInfo } from "@/protoOS/api/hooks/usePoolsInfo";
import { useHashboardSerials } from "@/protoOS/store";

interface AuthenticatedShellProps {
  reloadSystemInfo: () => void;
}

const AuthenticatedShell = ({ reloadSystemInfo }: AuthenticatedShellProps) => {
  useHardware();
  useNetworkInfo({ poll: false });
  useErrors({ poll: true, pollIntervalMs: 15 * 1000 });
  usePoolsInfo({ poll: true, pollIntervalMs: 15 * 1000 });

  const hashboardSerials = useHashboardSerials();
  useHashboardStatus({ hashboardSerialNumbers: hashboardSerials, poll: false });

  const { checkFirmwareUpdate } = useFirmwareUpdate();
  useEffect(() => {
    const checkForFirmwareUpdates = () => {
      checkFirmwareUpdate()
        .then(() => {
          reloadSystemInfo();
        })
        .catch((error) => {
          // Empty-response bodies parse to "Unexpected end of JSON input";
          // firmware routinely returns these, so swallow rather than log.
          if (
            error?.error?.message?.includes("Unexpected end of JSON input") ||
            error?.message?.includes("Unexpected end of JSON input")
          ) {
            return;
          }
          console.error("Error checking for firmware updates:", error);
        });
    };

    checkForFirmwareUpdates();
  }, [checkFirmwareUpdate, reloadSystemInfo]);

  return null;
};

export default AuthenticatedShell;
