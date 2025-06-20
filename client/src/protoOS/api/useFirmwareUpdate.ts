import { useCallback, useMemo } from "react";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

type useFirmwareUpdateProps = {
  poll: boolean;
  duration?: number;
};

const useFirmwareUpdate = ({ poll, duration }: useFirmwareUpdateProps) => {
  void poll;
  void duration;
  const { api } = useMinerHosting();

  const updateFirmware = useCallback(async () => {
    const response = await api?.updateSystem();
    return response;
  }, [api]);

  return useMemo(
    () => ({
      updateFirmware,
    }),
    [updateFirmware],
  );
};

export { useFirmwareUpdate };
