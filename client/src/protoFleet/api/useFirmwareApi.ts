import { useCallback, useMemo } from "react";
import { API_PROXY_BASE } from "@/protoFleet/api/constants";
import { extractFetchError, useFileUpload } from "@/protoFleet/api/useFileUpload";
import { useLogout } from "@/protoFleet/store";

export { computeSha256 } from "@/protoFleet/utils/crypto";

const API_BASE = `${API_PROXY_BASE}/api/v1/firmware`;

export const ALLOWED_EXTENSIONS = [".swu", ".tar.gz", ".zip"];

const DEFAULT_MAX_FILE_SIZE = 500 * 1024 * 1024;

export interface FirmwareConfig {
  allowedExtensions: string[];
  maxFileSizeBytes: number;
  chunkSizeBytes: number;
}

const DEFAULT_CONFIG: FirmwareConfig = {
  allowedExtensions: ALLOWED_EXTENSIONS,
  maxFileSizeBytes: DEFAULT_MAX_FILE_SIZE,
  chunkSizeBytes: 5 * 1024 * 1024,
};

let configCache: FirmwareConfig | null = null;
let configPromise: Promise<FirmwareConfig> | null = null;

/** @internal Exported for test cleanup only. */
export function _resetConfigCache(): void {
  configCache = null;
  configPromise = null;
}

async function fetchFirmwareConfig(logout: () => void): Promise<FirmwareConfig> {
  if (configCache) return configCache;
  if (configPromise) return configPromise;

  configPromise = (async () => {
    try {
      const response = await fetch(`${API_BASE}/config`, {
        method: "GET",
        credentials: "include",
      });

      if (response.status === 401) {
        logout();
        throw new Error("Session expired. Please log in again.");
      }

      if (!response.ok) {
        return DEFAULT_CONFIG;
      }

      const data = await response.json();
      const config: FirmwareConfig = {
        allowedExtensions: data.allowed_extensions ?? ALLOWED_EXTENSIONS,
        maxFileSizeBytes: data.max_file_size_bytes ?? DEFAULT_MAX_FILE_SIZE,
        chunkSizeBytes: data.chunk_size_bytes ?? 5 * 1024 * 1024,
      };
      configCache = config;
      return config;
    } catch (err) {
      if (err instanceof Error && err.message.includes("Session expired")) throw err;
      return DEFAULT_CONFIG;
    } finally {
      configPromise = null;
    }
  })();

  return configPromise;
}

export interface FirmwareUploadOptions {
  onProgress?: (percent: number) => void;
  signal?: AbortSignal;
}

export function validateFirmwareFile(
  file: File,
  config?: { allowedExtensions?: string[]; maxFileSizeBytes?: number },
): string | null {
  if (!file.name) {
    return "No filename provided.";
  }
  const extensions = config?.allowedExtensions ?? ALLOWED_EXTENSIONS;
  const lower = file.name.toLowerCase();
  const valid = extensions.some((ext) => lower.endsWith(ext));
  if (!valid) {
    return `Unsupported file type. Allowed: ${extensions.join(", ")}`;
  }
  if (file.size === 0) {
    return "File is empty.";
  }
  const maxSize = config?.maxFileSizeBytes;
  if (maxSize && file.size > maxSize) {
    return `File too large. Maximum size: ${Math.round(maxSize / (1024 * 1024))} MB.`;
  }
  return null;
}

interface CheckFirmwareResponse {
  exists: boolean;
  firmware_file_id?: string;
}

export const useFirmwareApi = () => {
  const logout = useLogout();
  const { upload } = useFileUpload();

  const getConfig = useCallback(async (): Promise<FirmwareConfig> => {
    return fetchFirmwareConfig(logout);
  }, [logout]);

  const checkFirmwareFile = useCallback(
    async (sha256: string, signal?: AbortSignal): Promise<{ exists: boolean; firmwareFileId?: string }> => {
      const response = await fetch(`${API_BASE}/check`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ sha256 }),
        signal,
      });

      if (response.status === 401) {
        logout();
        throw new Error("Session expired. Please log in again.");
      }

      if (!response.ok) {
        const message = await extractFetchError(
          response,
          `Firmware check failed: ${response.status} ${response.statusText}`,
        );
        throw new Error(message);
      }

      const data: CheckFirmwareResponse = await response.json();
      return {
        exists: data.exists,
        firmwareFileId: data.firmware_file_id,
      };
    },
    [logout],
  );

  const uploadFirmwareFile = useCallback(
    async (file: File, options?: FirmwareUploadOptions): Promise<string> => {
      const config = await fetchFirmwareConfig(logout);

      let data: unknown;
      const useChunked = file.size > config.chunkSizeBytes;
      if (useChunked) {
        data = await upload(`${API_BASE}/upload`, file, {
          onProgress: options?.onProgress,
          signal: options?.signal,
          chunked: {
            enabled: true,
            chunkSize: config.chunkSizeBytes,
            initiateUrl: `${API_BASE}/upload/chunked`,
            chunkUrl: (id) => `${API_BASE}/upload/chunked/${encodeURIComponent(id)}`,
            completeUrl: (id) => `${API_BASE}/upload/chunked/${encodeURIComponent(id)}/complete`,
          },
        });
      } else {
        data = await upload(`${API_BASE}/upload`, file, {
          onProgress: options?.onProgress,
          signal: options?.signal,
        });
      }

      const result = data as { firmware_file_id?: string };
      if (!result.firmware_file_id) {
        throw new Error("Server response missing firmware_file_id.");
      }
      return result.firmware_file_id;
    },
    [logout, upload],
  );

  return useMemo(
    () => ({
      getConfig,
      checkFirmwareFile,
      uploadFirmwareFile,
    }),
    [getConfig, checkFirmwareFile, uploadFirmwareFile],
  );
};
