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
  const { checkAccess, hasAccess } = useAccessToken(true);
  const { api } = useMinerHosting();
  const authHeader = useAuthHeader();
  const pausedAuthAction = usePausedAuthAction();
  const setPausedAuthAction = useSetPausedAuthAction();
  const [pendingUpdate, setPendingUpdate] = useState(false);

  // called when you click install.
  // adds a paused action and calls check access
  // if user is logged out it will trigger the login modal
  // if user is logged in the pausedAuthAction change will trigger
  // the useEffect below
  const updateFirmware = useCallback(async () => {
    setPausedAuthAction(AUTH_ACTIONS.update);
    checkAccess();
  }, [checkAccess, setPausedAuthAction]);

  useEffect(() => {
    const handleUpdate = async () => {
      if (hasAccess && pausedAuthAction === AUTH_ACTIONS.update) {
        setPausedAuthAction(null);
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
      }
    };

    handleUpdate();
  }, [api, authHeader, hasAccess, pausedAuthAction, setPausedAuthAction]);

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
