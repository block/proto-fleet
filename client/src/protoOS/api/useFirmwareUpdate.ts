import { useCallback, useMemo } from "react";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import {
  getAuthHeader,
  useAuthContext,
} from "@/protoOS/features/auth/contexts/AuthContext";

type useFirmwareUpdateProps = {
  poll: boolean;
  duration?: number;
};

const useFirmwareUpdate = ({ poll, duration }: useFirmwareUpdateProps) => {
  void poll;
  void duration;
  const { api } = useMinerHosting();
  const { authTokens } = useAuthContext();

  const updateFirmware = useCallback(async () => {
    const response = await api?.updateSystem(
      getAuthHeader(authTokens.accessToken.value),
    );
    return response;
  }, [api, authTokens.accessToken.value]);

  return useMemo(
    () => ({
      updateFirmware,
    }),
    [updateFirmware],
  );
};

export { useFirmwareUpdate };
