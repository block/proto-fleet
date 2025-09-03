import {
  createContext,
  ReactNode,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useFirmwareUpdate } from "@/protoOS/api";
import { SystemInfoSysteminfo, UpdateStatus } from "@/protoOS/api/types";
import { useSystemContext } from "@/protoOS/contexts/SystemContext/useSystemContext";

const FIRMWARE_UPDATE_CHECK_INTERVAL = 60000; // 60 seconds

const FirmwareUpdateContext = createContext({
  updateStatus: undefined as UpdateStatus | undefined,
  pending: false as boolean,
  dismissed: false,
  installing: false,
  setDismissed: (dismissed: boolean) => {
    void dismissed;
  },
});

type FirmwareUpdateProviderProps = {
  children: ReactNode;
  systemInfo?: SystemInfoSysteminfo;
};

export const FirmwareUpdateProvider = ({
  children,
  systemInfo,
}: FirmwareUpdateProviderProps) => {
  const [dismissed, setDismissed] = useState<boolean>(false);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const { checkFirmwareUpdate } = useFirmwareUpdate();
  const { reload: reloadSystemInfo } = useSystemContext();

  const installing = useMemo(() => {
    return (
      systemInfo?.sw_update_status?.status === "downloading" ||
      systemInfo?.sw_update_status?.status === "downloaded" ||
      systemInfo?.sw_update_status?.status === "installing" ||
      systemInfo?.sw_update_status?.status === "confirming"
    );
  }, [systemInfo?.sw_update_status?.status]);

  useEffect(() => {
    const checkForFirmwareUpdates = () => {
      checkFirmwareUpdate()
        .then(() => {
          reloadSystemInfo();
        })
        .catch((error) => {
          // Check if this is a JSON parsing error we should ignore
          if (
            error?.error?.message?.includes("Unexpected end of JSON input") ||
            error?.message?.includes("Unexpected end of JSON input")
          ) {
            // JSON parsing error from empty response - this is normal, ignore it
            return;
          }
          console.error("Error checking for firmware updates:", error);
        });
    };

    // Immediately check on component mount
    checkForFirmwareUpdates();

    // Setup periodic check
    intervalRef.current = setInterval(
      checkForFirmwareUpdates,
      FIRMWARE_UPDATE_CHECK_INTERVAL,
    );

    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
    };
  }, [checkFirmwareUpdate, reloadSystemInfo]);

  return (
    <FirmwareUpdateContext.Provider
      value={{
        updateStatus: systemInfo?.sw_update_status,
        pending: !systemInfo,
        dismissed,
        installing,
        setDismissed: (dismissed: boolean) => {
          setDismissed(dismissed);
        },
      }}
    >
      {children}
    </FirmwareUpdateContext.Provider>
  );
};

export default FirmwareUpdateContext;
