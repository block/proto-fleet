import { createContext, ReactNode, useMemo, useState } from "react";
import { useFirmwareUpdate as useApiFirmwareUpdate } from "@/protoOS/api";
import { SystemInfoSysteminfo } from "@/protoOS/api/types";

const FirmwareUpdateContext = createContext({
  status: null as string | null,
  pending: false as boolean,
  version: null as string | null,
  changelog: null as string | null,
  message: null as string | null,
  progress: null as number | null,
  dismissed: false,
  installing: false,
  setDismissed: (dismissed: boolean) => {
    void dismissed;
  },
  updateFirmware: () => {},
});

type FirmwareUpdateProviderProps = {
  children: ReactNode;
  systemInfo?: SystemInfoSysteminfo;
};

export const FirmwareUpdateProvider = ({
  children,
  systemInfo,
}: FirmwareUpdateProviderProps) => {
  const { updateFirmware } = useApiFirmwareUpdate({
    poll: false,
  });

  const [dismissed, setDismissed] = useState<boolean>(false);

  const installing = useMemo(() => {
    return (
      systemInfo?.sw_update_status?.status === "downloading" ||
      systemInfo?.sw_update_status?.status === "downloaded" ||
      systemInfo?.sw_update_status?.status === "installing" ||
      systemInfo?.sw_update_status?.status === "confirming"
    );
  }, [systemInfo?.sw_update_status?.status]);

  return (
    <FirmwareUpdateContext.Provider
      value={{
        status: systemInfo?.sw_update_status?.status ?? null,
        pending: !systemInfo,
        version: systemInfo?.sw_update_status?.new_version ?? null,
        changelog: "",
        message: systemInfo?.sw_update_status?.message ?? null,
        progress: systemInfo?.sw_update_status?.progress ?? null,
        dismissed,
        installing,
        setDismissed: (dismissed: boolean) => {
          setDismissed(dismissed);
        },
        updateFirmware,
      }}
    >
      {children}
    </FirmwareUpdateContext.Provider>
  );
};

export default FirmwareUpdateContext;
