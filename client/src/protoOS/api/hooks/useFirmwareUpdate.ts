import { useCallback, useEffect, useMemo, useState } from "react";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { AUTH_ACTIONS, useAccessToken, usePausedAuthAction, useSetPausedAuthAction } from "@/protoOS/store";

const useFirmwareUpdate = () => {
  const { adapter, isFleetHosted } = useMinerHosting();
  const { checkAccess, hasAccess } = useAccessToken(!isFleetHosted);
  const pausedAuthAction = usePausedAuthAction();
  const setPausedAuthAction = useSetPausedAuthAction();
  const [pendingUpdate, setPendingUpdate] = useState(false);

  const performUpdate = useCallback(async () => {
    setPendingUpdate(true);
    try {
      await adapter?.startFirmwareUpdate();
    } finally {
      setPendingUpdate(false);
    }
  }, [adapter]);

  // called when you click install.
  // Fleet-hosted: the server proxy authenticates to the miner, so run the
  // update directly. Otherwise add a paused action and call check access — if
  // logged out this triggers the login modal; if logged in the pausedAuthAction
  // change triggers the useEffect below.
  const updateFirmware = useCallback(async () => {
    if (isFleetHosted) {
      await performUpdate();
      return;
    }
    setPausedAuthAction(AUTH_ACTIONS.update);
    checkAccess();
  }, [isFleetHosted, performUpdate, checkAccess, setPausedAuthAction]);

  useEffect(() => {
    if (hasAccess && pausedAuthAction === AUTH_ACTIONS.update) {
      setPausedAuthAction(null);
      // eslint-disable-next-line react-hooks/set-state-in-effect -- resume the paused firmware update once auth resolves (performUpdate toggles pending state)
      void performUpdate();
    }
  }, [hasAccess, pausedAuthAction, setPausedAuthAction, performUpdate]);

  const checkFirmwareUpdate = useCallback(async () => {
    await adapter?.checkForFirmwareUpdate();
  }, [adapter]);

  return useMemo(
    () => ({
      updateFirmware,
      checkFirmwareUpdate,
      pendingUpdate,
    }),
    [updateFirmware, checkFirmwareUpdate, pendingUpdate],
  );
};

export { useFirmwareUpdate };
