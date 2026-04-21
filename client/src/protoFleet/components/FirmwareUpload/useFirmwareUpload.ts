import { useCallback, useEffect, useRef, useState } from "react";
import type { FirmwareConfig } from "@/protoFleet/api/useFirmwareApi";
import { computeSha256, useFirmwareApi, validateFirmwareFile } from "@/protoFleet/api/useFirmwareApi";

export type UploadState = "idle" | "hashing" | "checking" | "uploading" | "ready" | "error";

export interface UseFirmwareUploadReturn {
  state: UploadState;
  file: File | null;
  firmwareFileId: string | null;
  uploadProgress: number;
  errorMessage: string | null;
  serverConfig: FirmwareConfig | null;
  processFile: (file: File) => void;
  reset: () => void;
  retry: () => void;
}

export function useFirmwareUpload(active: boolean): UseFirmwareUploadReturn {
  const [state, setState] = useState<UploadState>("idle");
  const [file, setFile] = useState<File | null>(null);
  const [firmwareFileId, setFirmwareFileId] = useState<string | null>(null);
  const [uploadProgress, setUploadProgress] = useState(0);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [serverConfig, setServerConfig] = useState<FirmwareConfig | null>(null);
  const [retryCount, setRetryCount] = useState(0);
  const abortControllerRef = useRef<AbortController | null>(null);

  const { getConfig, checkFirmwareFile, uploadFirmwareFile } = useFirmwareApi();

  useEffect(() => {
    if (active) {
      let cancelled = false;
      void getConfig()
        .then((config) => {
          if (cancelled) return;
          setServerConfig(config);
          setState((prev) => (prev === "error" ? "idle" : prev));
          setErrorMessage(null);
        })
        .catch((err) => {
          if (cancelled) return;
          setErrorMessage(err instanceof Error ? err.message : "Failed to load firmware configuration.");
          setState("error");
        });
      return () => {
        cancelled = true;
      };
    }
  }, [active, getConfig, retryCount]);

  useEffect(() => {
    if (!active) {
      abortControllerRef.current?.abort();
      abortControllerRef.current = null;
    }
    return () => {
      abortControllerRef.current?.abort();
      abortControllerRef.current = null;
    };
  }, [active]);

  const reset = useCallback(() => {
    abortControllerRef.current?.abort();
    abortControllerRef.current = null;
    setState("idle");
    setFile(null);
    setFirmwareFileId(null);
    setUploadProgress(0);
    setErrorMessage(null);
  }, []);

  const retry = useCallback(() => {
    reset();
    setRetryCount((c) => c + 1);
  }, [reset]);

  const processFile = useCallback(
    async (selectedFile: File) => {
      abortControllerRef.current?.abort();
      const controller = new AbortController();
      abortControllerRef.current = controller;

      try {
        const config = serverConfig ?? (await getConfig());
        if (controller.signal.aborted) return;

        const validationError = validateFirmwareFile(selectedFile, config);
        if (validationError) {
          setErrorMessage(validationError);
          setState("error");
          return;
        }

        setFile(selectedFile);
        setState("hashing");
        const sha256 = await computeSha256(selectedFile);
        if (controller.signal.aborted) return;

        setState("checking");
        const { exists, firmwareFileId: existingId } = await checkFirmwareFile(sha256, controller.signal);
        if (controller.signal.aborted) return;

        if (exists && existingId) {
          setFirmwareFileId(existingId);
          setState("ready");
          return;
        }

        setState("uploading");
        setUploadProgress(0);
        const newId = await uploadFirmwareFile(selectedFile, {
          onProgress: setUploadProgress,
          signal: controller.signal,
        });
        if (controller.signal.aborted) return;
        setFirmwareFileId(newId);
        setState("ready");
      } catch (err) {
        if (controller.signal.aborted) return;
        setErrorMessage(err instanceof Error ? err.message : String(err));
        setState("error");
      }
    },
    [checkFirmwareFile, uploadFirmwareFile, serverConfig, getConfig],
  );

  const wrappedProcessFile = useCallback((f: File) => void processFile(f), [processFile]);

  return {
    state,
    file,
    firmwareFileId,
    uploadProgress,
    errorMessage,
    serverConfig,
    processFile: wrappedProcessFile,
    reset,
    retry,
  };
}
