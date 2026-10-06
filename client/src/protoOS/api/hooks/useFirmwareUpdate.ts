import { useCallback, useEffect, useMemo, useState } from "react";

import { HttpResponse, MessageResponse } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import {
  AUTH_ACTIONS,
  useAccessToken,
  useAuthHeader,
  usePausedAuthAction,
  useSetPausedAuthAction,
} from "@/protoOS/store";

const useFirmwareUpdate = () => {
  const { api, isFleetHosted } = useMinerHosting();
  const { checkAccess, hasAccess } = useAccessToken(!isFleetHosted);
  const authHeader = useAuthHeader();
  const pausedAuthAction = usePausedAuthAction();
  const setPausedAuthAction = useSetPausedAuthAction();
  const [pendingUpdate, setPendingUpdate] = useState(false);

  const performUpdate = useCallback(async () => {
    setPendingUpdate(true);
    try {
      const response = await api?.postUpdateSystem(authHeader);

      // Check if the response has a JSON parsing error embedded in it
      if (response?.error?.message?.includes("Unexpected end of JSON input")) {
        return {
          data: { message: "System update initiated successfully" },
          status: 200,
          ok: true,
          error: null,
        };
      }

      return response;
    } catch (error: any) {
      // Handle JSON parsing errors embedded in response object
      if (error?.error?.message?.includes("Unexpected end of JSON input")) {
        return {
          data: { message: "System update initiated successfully" },
          status: 200,
          ok: true,
          error: null,
        };
      }

      // Handle JSON parsing errors from thrown exceptions
      if (error?.message?.includes("Unexpected end of JSON input")) {
        return {
          data: { message: "System update initiated successfully" },
          status: 200,
          ok: true,
          error: null,
        };
      }

      // Re-throw other errors
      throw error;
    } finally {
      setPendingUpdate(false);
    }
  }, [api, authHeader]);

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
    try {
      const response = await api?.updateCheck();

      // Check if the response has a JSON parsing error embedded in it
      if (response?.error?.message?.includes("Unexpected end of JSON input")) {
        return {
          data: { message: "Update check completed successfully" },
          status: 200,
          ok: true,
          error: null,
        } as HttpResponse<MessageResponse>;
      }

      // API returns void on success, so check status codes
      if (response && response.status === 200) {
        return {
          data: { message: "Update check initiated successfully" },
          status: 200,
          ok: true,
          error: null,
        } as HttpResponse<MessageResponse>;
      }
      if (response && response.status === 202) {
        return {
          data: { message: "Update check accepted and in progress" },
          status: 202,
          ok: true,
          error: null,
        } as HttpResponse<MessageResponse>;
      }

      return response as unknown as HttpResponse<MessageResponse>;
    } catch (error: any) {
      // Handle JSON parsing errors from thrown exceptions
      if (error?.message?.includes("Unexpected end of JSON input")) {
        return {
          data: { message: "Update check completed successfully" },
          status: 200,
          ok: true,
          error: null,
        } as HttpResponse<MessageResponse>;
      }

      // Re-throw other errors
      throw error;
    }
  }, [api]);

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
