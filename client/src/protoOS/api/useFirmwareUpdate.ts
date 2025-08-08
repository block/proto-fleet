import { useCallback, useMemo } from "react";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import {
  getAuthHeader,
  useAccessToken,
  useAuthContext,
} from "@/protoOS/features/auth/contexts/AuthContext";

type useFirmwareUpdateProps = {
  poll: boolean;
  duration?: number;
};

const useFirmwareUpdate = ({ poll, duration }: useFirmwareUpdateProps) => {
  void poll;
  void duration;
  const { hasAccess } = useAccessToken(true, false);
  const { api } = useMinerHosting();
  const { authTokens, setShowLoginModal } = useAuthContext();

  const updateFirmware = useCallback(async () => {
    if (!hasAccess) {
      setShowLoginModal(true);
      return;
    }

    const response = await api?.updateSystem(
      getAuthHeader(authTokens.accessToken.value),
    );
    return response;
  }, [api, authTokens.accessToken.value, hasAccess, setShowLoginModal]);

  return useMemo(
    () => ({
      updateFirmware,
    }),
    [updateFirmware],
  );
};

export { useFirmwareUpdate };
