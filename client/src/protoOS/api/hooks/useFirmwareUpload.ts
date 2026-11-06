import { useCallback, useEffect, useRef, useState } from "react";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store";

export const useFirmwareUpload = (reloadStatus: (signal: AbortSignal) => void) => {
  const { adapter, isFleetHosted } = useMinerHosting();
  const authRetry = useAuthRetry();
  const activeRequest = useRef<AbortController | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string>();
  const supported = !!adapter?.uploadFirmware && !isFleetHosted;

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- connection changes discard the previous device's upload state
    setPending(false);
    setError(undefined);
    return () => {
      activeRequest.current?.abort();
      activeRequest.current = null;
    };
  }, [adapter, isFleetHosted]);

  const uploadFirmware = useCallback(
    async (file: File): Promise<boolean> => {
      if (!supported || !adapter?.uploadFirmware || activeRequest.current) return false;
      if (!file.name.toLowerCase().endsWith(".swu") || file.size === 0) {
        setError("Choose a non-empty .swu firmware file.");
        return false;
      }

      const controller = new AbortController();
      activeRequest.current = controller;
      setPending(true);
      setError(undefined);
      let accepted = false;
      try {
        await authRetry({
          signal: controller.signal,
          request: (params) => adapter.uploadFirmware!(file, params),
          onSuccess: () => {
            accepted = true;
          },
          onError: (failure) => {
            if (controller.signal.aborted) return;
            const message =
              failure?.status === 401
                ? "Sign in, then try uploading again."
                : failure?.status === 409
                  ? "An update is already in progress. Check its status before uploading again."
                  : failure?.status === 400 || failure?.status === 413 || failure?.status === 422
                    ? "The rig rejected this firmware file. Choose a valid R2 .swu file."
                    : "Upload could not be confirmed. Check the update status before trying again.";
            setError(message);
          },
        });
        if (!controller.signal.aborted) reloadStatus(controller.signal);
        return accepted && !controller.signal.aborted;
      } finally {
        if (activeRequest.current === controller) {
          activeRequest.current = null;
          setPending(false);
        }
      }
    },
    [adapter, authRetry, reloadStatus, supported],
  );

  return { uploadFirmware, pending, error, supported };
};
