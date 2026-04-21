import { useCallback, useMemo } from "react";
import { API_PROXY_BASE } from "@/protoFleet/api/constants";
import { extractFetchError, useFileUpload } from "@/protoFleet/api/useFileUpload";
import { useLogout } from "@/protoFleet/store";

export { computeSha256 } from "@/protoFleet/utils/crypto";

const API_BASE = `${API_PROXY_BASE}/api/v1/firmware`;

const DEFAULT_MAX_FILE_SIZE = 500 * 1024 * 1024;
const DEFAULT_CHUNK_SIZE = 32 * 1024 * 1024;

export interface FirmwareConfig {
  allowedExtensions: string[];
  maxFileSizeBytes: number;
  chunkSizeBytes: number;
}

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
        throw new Error(`Failed to load firmware config: ${response.status} ${response.statusText}`);
      }

      const data = await response.json();
      if (!Array.isArray(data.allowed_extensions) || data.allowed_extensions.length === 0) {
        throw new Error("Server returned invalid firmware config: missing allowed_extensions.");
      }

      const config: FirmwareConfig = {
        allowedExtensions: data.allowed_extensions,
        maxFileSizeBytes: data.max_file_size_bytes ?? DEFAULT_MAX_FILE_SIZE,
        chunkSizeBytes: data.chunk_size_bytes ?? DEFAULT_CHUNK_SIZE,
      };
      configCache = config;
      return config;
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
  config: { allowedExtensions: string[]; maxFileSizeBytes?: number },
): string | null {
  if (!file.name) {
    return "No filename provided.";
  }
  const lower = file.name.toLowerCase();
  const valid = config.allowedExtensions.some((ext) => lower.endsWith(ext));
  if (!valid) {
    return `Unsupported file type. Allowed: ${config.allowedExtensions.join(", ")}`;
  }
  if (file.size === 0) {
    return "File is empty.";
  }
  if (config.maxFileSizeBytes && file.size > config.maxFileSizeBytes) {
    return `File too large. Maximum size: ${Math.round(config.maxFileSizeBytes / (1024 * 1024))} MB.`;
  }
  return null;
}

export interface FirmwareFileInfo {
  id: string;
  filename: string;
  size: number;
  uploaded_at: string;
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

  const listFirmwareFiles = useCallback(
    async (signal?: AbortSignal): Promise<FirmwareFileInfo[]> => {
      const response = await fetch(`${API_BASE}/files`, {
        method: "GET",
        credentials: "include",
        signal,
      });

      if (response.status === 401) {
        logout();
        throw new Error("Session expired. Please log in again.");
      }

      if (!response.ok) {
        const message = await extractFetchError(
          response,
          `Failed to list firmware files: ${response.status} ${response.statusText}`,
        );
        throw new Error(message);
      }

      const data = await response.json();
      return (data.files ?? []) as FirmwareFileInfo[];
    },
    [logout],
  );

  const deleteFirmwareFile = useCallback(
    async (fileId: string, signal?: AbortSignal): Promise<void> => {
      const response = await fetch(`${API_BASE}/files/${encodeURIComponent(fileId)}`, {
        method: "DELETE",
        credentials: "include",
        signal,
      });

      if (response.status === 401) {
        logout();
        throw new Error("Session expired. Please log in again.");
      }

      if (!response.ok) {
        const message = await extractFetchError(
          response,
          `Failed to delete firmware file: ${response.status} ${response.statusText}`,
        );
        throw new Error(message);
      }
    },
    [logout],
  );

  const deleteAllFirmwareFiles = useCallback(
    async (signal?: AbortSignal): Promise<{ deleted_count: number }> => {
      const response = await fetch(`${API_BASE}/files`, {
        method: "DELETE",
        credentials: "include",
        signal,
      });

      if (response.status === 401) {
        logout();
        throw new Error("Session expired. Please log in again.");
      }

      if (!response.ok) {
        const message = await extractFetchError(
          response,
          `Failed to delete all firmware files: ${response.status} ${response.statusText}`,
        );
        throw new Error(message);
      }

      return (await response.json()) as { deleted_count: number };
    },
    [logout],
  );

  return useMemo(
    () => ({
      getConfig,
      checkFirmwareFile,
      uploadFirmwareFile,
      listFirmwareFiles,
      deleteFirmwareFile,
      deleteAllFirmwareFiles,
    }),
    [getConfig, checkFirmwareFile, uploadFirmwareFile, listFirmwareFiles, deleteFirmwareFile, deleteAllFirmwareFiles],
  );
};
