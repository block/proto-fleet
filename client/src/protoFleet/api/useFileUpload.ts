import { useCallback, useMemo } from "react";
import { useLogout } from "@/protoFleet/store";

export interface ChunkedUploadConfig {
  enabled: boolean;
  chunkSize: number;
  initiateUrl: string;
  chunkUrl: (uploadId: string) => string;
  completeUrl: (uploadId: string) => string;
}

export interface FileUploadOptions {
  onProgress?: (percent: number) => void;
  signal?: AbortSignal;
  fieldName?: string;
  chunked?: ChunkedUploadConfig;
}

interface ErrorBody {
  error?: string;
}

export async function extractFetchError(response: Response, fallback: string): Promise<string> {
  try {
    const data: ErrorBody = await response.json();
    if (data.error) return data.error;
  } catch {
    /* not JSON */
  }
  return fallback;
}

function extractXhrError(responseText: string, fallback: string): string {
  try {
    const data: ErrorBody = JSON.parse(responseText);
    if (data.error) return data.error;
  } catch {
    /* not JSON */
  }
  return fallback;
}

function handleAuth401(status: number, logout: () => void): void {
  if (status === 401) {
    logout();
    throw new Error("Session expired. Please log in again.");
  }
}

async function uploadChunked(
  file: File,
  options: FileUploadOptions & { chunked: ChunkedUploadConfig },
  logout: () => void,
): Promise<unknown> {
  const { chunked, onProgress, signal } = options;
  const totalChunks = Math.ceil(file.size / chunked.chunkSize);

  const initResponse = await fetch(chunked.initiateUrl, {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ filename: file.name, file_size: file.size }),
    signal,
  });

  handleAuth401(initResponse.status, logout);
  if (!initResponse.ok) {
    throw new Error(
      await extractFetchError(
        initResponse,
        `Failed to initiate upload: ${initResponse.status} ${initResponse.statusText}`,
      ),
    );
  }

  const initData: { upload_id?: string } = await initResponse.json();
  if (!initData.upload_id) {
    throw new Error("Server response missing upload_id.");
  }
  const uploadId = initData.upload_id;

  for (let i = 0; i < totalChunks; i++) {
    if (signal?.aborted) {
      throw new Error("Upload was cancelled.");
    }

    const start = i * chunked.chunkSize;
    const end = Math.min(start + chunked.chunkSize, file.size);

    const chunkResponse = await fetch(chunked.chunkUrl(uploadId), {
      method: "PUT",
      credentials: "include",
      headers: {
        "Content-Type": "application/octet-stream",
        "Content-Range": `bytes ${start}-${end - 1}/${file.size}`,
      },
      body: file.slice(start, end),
      signal,
    });

    handleAuth401(chunkResponse.status, logout);
    if (!chunkResponse.ok) {
      throw new Error(
        await extractFetchError(
          chunkResponse,
          `Chunk upload failed: ${chunkResponse.status} ${chunkResponse.statusText}`,
        ),
      );
    }

    onProgress?.(Math.round(((i + 1) / totalChunks) * 100));
  }

  const completeResponse = await fetch(chunked.completeUrl(uploadId), {
    method: "POST",
    credentials: "include",
    signal,
  });

  handleAuth401(completeResponse.status, logout);
  if (!completeResponse.ok) {
    throw new Error(
      await extractFetchError(
        completeResponse,
        `Failed to complete upload: ${completeResponse.status} ${completeResponse.statusText}`,
      ),
    );
  }

  return completeResponse.json();
}

function uploadDirect(
  url: string,
  file: File,
  options: FileUploadOptions | undefined,
  logout: () => void,
): Promise<unknown> {
  if (options?.signal?.aborted) {
    return Promise.reject(new Error("Upload was cancelled."));
  }
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", url);
    xhr.withCredentials = true;

    if (options?.onProgress) {
      const onProgress = options.onProgress;
      xhr.upload.addEventListener("progress", (event) => {
        if (event.lengthComputable) {
          onProgress(Math.round((event.loaded / event.total) * 100));
        }
      });
    }

    if (options?.signal) {
      options.signal.addEventListener("abort", () => xhr.abort(), { once: true });
    }

    xhr.addEventListener("load", () => {
      if (xhr.status === 401) {
        logout();
        reject(new Error("Session expired. Please log in again."));
        return;
      }

      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          resolve(JSON.parse(xhr.responseText));
        } catch {
          reject(new Error("Invalid response from upload endpoint."));
        }
      } else {
        const message = extractXhrError(xhr.responseText, `Upload failed: ${xhr.status} ${xhr.statusText}`);
        reject(new Error(message));
      }
    });

    xhr.addEventListener("error", () => {
      reject(new Error("Network error during upload."));
    });

    xhr.addEventListener("abort", () => {
      reject(new Error("Upload was cancelled."));
    });

    const formData = new FormData();
    formData.append(options?.fieldName ?? "file", file);
    xhr.send(formData);
  });
}

export const useFileUpload = () => {
  const logout = useLogout();

  const upload = useCallback(
    async (url: string, file: File, options?: FileUploadOptions): Promise<unknown> => {
      if (options?.chunked?.enabled) {
        return uploadChunked(file, options as FileUploadOptions & { chunked: ChunkedUploadConfig }, logout);
      }
      return uploadDirect(url, file, options, logout);
    },
    [logout],
  );

  return useMemo(() => ({ upload }), [upload]);
};
