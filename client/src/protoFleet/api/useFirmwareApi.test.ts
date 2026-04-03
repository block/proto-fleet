import { renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { _resetConfigCache, useFirmwareApi, validateFirmwareFile } from "./useFirmwareApi";

const mockLogout = vi.fn();
const mockUpload = vi.fn();

vi.mock("@/protoFleet/store", () => ({
  useLogout: () => mockLogout,
}));

vi.mock("@/protoFleet/api/useFileUpload", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/api/useFileUpload")>()),
  useFileUpload: () => ({ upload: mockUpload }),
}));

describe("validateFirmwareFile", () => {
  const defaultConfig = { allowedExtensions: [".swu", ".tar.gz", ".zip"] };
  const createFile = (name: string, size = 1024): File => new File(["x".repeat(size)], name);

  it("accepts .swu files", () => {
    expect(validateFirmwareFile(createFile("firmware.swu"), defaultConfig)).toBeNull();
  });

  it("accepts .tar.gz files", () => {
    expect(validateFirmwareFile(createFile("firmware.tar.gz"), defaultConfig)).toBeNull();
  });

  it("accepts .zip files", () => {
    expect(validateFirmwareFile(createFile("firmware.zip"), defaultConfig)).toBeNull();
  });

  it("accepts uppercase extensions", () => {
    expect(validateFirmwareFile(createFile("firmware.SWU"), defaultConfig)).toBeNull();
    expect(validateFirmwareFile(createFile("firmware.TAR.GZ"), defaultConfig)).toBeNull();
    expect(validateFirmwareFile(createFile("firmware.ZIP"), defaultConfig)).toBeNull();
  });

  it("rejects unsupported extensions", () => {
    expect(validateFirmwareFile(createFile("firmware.bin"), defaultConfig)).toContain("Unsupported file type");
  });

  it("rejects files with no extension", () => {
    expect(validateFirmwareFile(createFile("firmware"), defaultConfig)).toContain("Unsupported file type");
  });

  it("rejects empty files", () => {
    const emptyFile = new File([], "firmware.swu");
    expect(validateFirmwareFile(emptyFile, defaultConfig)).toBe("File is empty.");
  });

  it("rejects files with no filename", () => {
    const file = new File(["data"], "");
    expect(validateFirmwareFile(file, defaultConfig)).toBe("No filename provided.");
  });

  it("uses custom extensions from config", () => {
    const file = new File(["data"], "firmware.img");
    expect(validateFirmwareFile(file, { allowedExtensions: [".img"] })).toBeNull();
  });

  it("rejects files exceeding maxFileSizeBytes", () => {
    const file = new File(["x".repeat(200)], "firmware.swu");
    expect(validateFirmwareFile(file, { ...defaultConfig, maxFileSizeBytes: 100 })).toContain("File too large");
  });

  it("accepts files within maxFileSizeBytes", () => {
    const file = new File(["x".repeat(50)], "firmware.swu");
    expect(validateFirmwareFile(file, { ...defaultConfig, maxFileSizeBytes: 100 })).toBeNull();
  });
});

describe("useFirmwareApi", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    _resetConfigCache();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  describe("checkFirmwareFile", () => {
    it("sends POST with JSON body and credentials", async () => {
      const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: () => Promise.resolve({ exists: false }),
      });
      vi.stubGlobal("fetch", mockFetch);

      const { result } = renderHook(() => useFirmwareApi());
      await result.current.checkFirmwareFile("abc123");

      expect(mockFetch).toHaveBeenCalledWith(
        "/api-proxy/api/v1/firmware/check",
        expect.objectContaining({
          method: "POST",
          credentials: "include",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ sha256: "abc123" }),
        }),
      );
    });

    it("returns exists and firmwareFileId on success", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: true,
          status: 200,
          json: () => Promise.resolve({ exists: true, firmware_file_id: "file-123" }),
        }),
      );

      const { result } = renderHook(() => useFirmwareApi());
      const data = await result.current.checkFirmwareFile("abc123");

      expect(data).toEqual({ exists: true, firmwareFileId: "file-123" });
    });

    it("returns exists false when file not found", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: true,
          status: 200,
          json: () => Promise.resolve({ exists: false }),
        }),
      );

      const { result } = renderHook(() => useFirmwareApi());
      const data = await result.current.checkFirmwareFile("abc123");

      expect(data).toEqual({ exists: false, firmwareFileId: undefined });
    });

    it("calls logout on 401 response", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: false,
          status: 401,
          statusText: "Unauthorized",
        }),
      );

      const { result } = renderHook(() => useFirmwareApi());
      await expect(result.current.checkFirmwareFile("abc123")).rejects.toThrow("Session expired");

      expect(mockLogout).toHaveBeenCalledOnce();
    });

    it("throws on non-401 HTTP error without calling logout", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: false,
          status: 500,
          statusText: "Internal Server Error",
          json: () => Promise.reject(new Error("no body")),
        }),
      );

      const { result } = renderHook(() => useFirmwareApi());
      await expect(result.current.checkFirmwareFile("abc123")).rejects.toThrow("Firmware check failed: 500");

      expect(mockLogout).not.toHaveBeenCalled();
    });

    it("surfaces server error message from JSON body on failure", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: false,
          status: 400,
          statusText: "Bad Request",
          json: () => Promise.resolve({ error: "sha256 must be a 64-character hex string" }),
        }),
      );

      const { result } = renderHook(() => useFirmwareApi());
      await expect(result.current.checkFirmwareFile("bad")).rejects.toThrow("sha256 must be a 64-character hex string");
    });
  });

  describe("uploadFirmwareFile", () => {
    it("fetches config then delegates to useFileUpload", async () => {
      const configFetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: () =>
          Promise.resolve({
            allowed_extensions: [".swu"],
            max_file_size_bytes: 500 * 1024 * 1024,
            chunk_size_bytes: 1 * 1024 * 1024,
          }),
      });
      vi.stubGlobal("fetch", configFetch);
      mockUpload.mockResolvedValue({ firmware_file_id: "fw-abc" });

      const file = new File(["data"], "firmware.swu");
      const { result } = renderHook(() => useFirmwareApi());
      const id = await result.current.uploadFirmwareFile(file);

      expect(id).toBe("fw-abc");
      expect(mockUpload).toHaveBeenCalledWith(
        "/api-proxy/api/v1/firmware/upload",
        file,
        expect.objectContaining({
          onProgress: undefined,
          signal: undefined,
        }),
      );
    });

    it("uses chunked upload when file exceeds chunk size", async () => {
      const configFetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: () =>
          Promise.resolve({
            allowed_extensions: [".swu"],
            chunk_size_bytes: 5,
          }),
      });
      vi.stubGlobal("fetch", configFetch);
      mockUpload.mockResolvedValue({ firmware_file_id: "fw-chunked" });

      const file = new File(["a".repeat(10)], "firmware.swu");
      const onProgress = vi.fn();
      const { result } = renderHook(() => useFirmwareApi());
      const id = await result.current.uploadFirmwareFile(file, { onProgress });

      expect(id).toBe("fw-chunked");
      expect(mockUpload).toHaveBeenCalledWith(
        "/api-proxy/api/v1/firmware/upload",
        file,
        expect.objectContaining({
          onProgress,
          chunked: expect.objectContaining({
            enabled: true,
            chunkSize: 5,
          }),
        }),
      );
    });

    it("throws when upload response is missing firmware_file_id", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: true,
          status: 200,
          json: () =>
            Promise.resolve({
              allowed_extensions: [".swu"],
              max_file_size_bytes: 500 * 1024 * 1024,
              chunk_size_bytes: 1 * 1024 * 1024,
            }),
        }),
      );
      mockUpload.mockResolvedValue({});

      const file = new File(["data"], "firmware.swu");
      const { result } = renderHook(() => useFirmwareApi());

      await expect(result.current.uploadFirmwareFile(file)).rejects.toThrow(
        "Server response missing firmware_file_id.",
      );
    });

    it("passes signal through to useFileUpload", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: true,
          status: 200,
          json: () =>
            Promise.resolve({
              allowed_extensions: [".swu"],
              max_file_size_bytes: 500 * 1024 * 1024,
              chunk_size_bytes: 1 * 1024 * 1024,
            }),
        }),
      );
      mockUpload.mockResolvedValue({ firmware_file_id: "fw-1" });

      const controller = new AbortController();
      const file = new File(["data"], "firmware.swu");
      const { result } = renderHook(() => useFirmwareApi());

      await result.current.uploadFirmwareFile(file, { signal: controller.signal });

      expect(mockUpload).toHaveBeenCalledWith(
        expect.any(String),
        file,
        expect.objectContaining({ signal: controller.signal }),
      );
    });
  });

  describe("listFirmwareFiles", () => {
    it("sends GET with credentials and returns file list", async () => {
      const mockFiles = [{ id: "f1", filename: "fw.swu", size: 1024, uploaded_at: "2025-01-01T00:00:00Z" }];
      const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: () => Promise.resolve({ files: mockFiles }),
      });
      vi.stubGlobal("fetch", mockFetch);

      const { result } = renderHook(() => useFirmwareApi());
      const files = await result.current.listFirmwareFiles();

      expect(mockFetch).toHaveBeenCalledWith(
        "/api-proxy/api/v1/firmware/files",
        expect.objectContaining({
          method: "GET",
          credentials: "include",
        }),
      );
      expect(files).toEqual(mockFiles);
    });

    it("returns empty array when no files exist", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: true,
          status: 200,
          json: () => Promise.resolve({ files: [] }),
        }),
      );

      const { result } = renderHook(() => useFirmwareApi());
      const files = await result.current.listFirmwareFiles();

      expect(files).toEqual([]);
    });

    it("calls logout on 401 response", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: false,
          status: 401,
          statusText: "Unauthorized",
        }),
      );

      const { result } = renderHook(() => useFirmwareApi());
      await expect(result.current.listFirmwareFiles()).rejects.toThrow("Session expired");
      expect(mockLogout).toHaveBeenCalledOnce();
    });

    it("throws on server error", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: false,
          status: 500,
          statusText: "Internal Server Error",
          json: () => Promise.reject(new Error("no body")),
        }),
      );

      const { result } = renderHook(() => useFirmwareApi());
      await expect(result.current.listFirmwareFiles()).rejects.toThrow("Failed to list firmware files");
    });
  });

  describe("deleteFirmwareFile", () => {
    it("sends DELETE with file ID and credentials", async () => {
      const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 204,
      });
      vi.stubGlobal("fetch", mockFetch);

      const { result } = renderHook(() => useFirmwareApi());
      await result.current.deleteFirmwareFile("file-123");

      expect(mockFetch).toHaveBeenCalledWith(
        "/api-proxy/api/v1/firmware/files/file-123",
        expect.objectContaining({
          method: "DELETE",
          credentials: "include",
        }),
      );
    });

    it("calls logout on 401 response", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: false,
          status: 401,
          statusText: "Unauthorized",
        }),
      );

      const { result } = renderHook(() => useFirmwareApi());
      await expect(result.current.deleteFirmwareFile("file-123")).rejects.toThrow("Session expired");
      expect(mockLogout).toHaveBeenCalledOnce();
    });

    it("throws on 404 response", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: false,
          status: 404,
          statusText: "Not Found",
          json: () => Promise.resolve({ error: "firmware file not found" }),
        }),
      );

      const { result } = renderHook(() => useFirmwareApi());
      await expect(result.current.deleteFirmwareFile("missing-id")).rejects.toThrow("firmware file not found");
    });
  });

  describe("deleteAllFirmwareFiles", () => {
    it("sends DELETE and returns deleted count", async () => {
      const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: () => Promise.resolve({ deleted_count: 3 }),
      });
      vi.stubGlobal("fetch", mockFetch);

      const { result } = renderHook(() => useFirmwareApi());
      const data = await result.current.deleteAllFirmwareFiles();

      expect(mockFetch).toHaveBeenCalledWith(
        "/api-proxy/api/v1/firmware/files",
        expect.objectContaining({
          method: "DELETE",
          credentials: "include",
        }),
      );
      expect(data).toEqual({ deleted_count: 3 });
    });

    it("calls logout on 401 response", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: false,
          status: 401,
          statusText: "Unauthorized",
        }),
      );

      const { result } = renderHook(() => useFirmwareApi());
      await expect(result.current.deleteAllFirmwareFiles()).rejects.toThrow("Session expired");
      expect(mockLogout).toHaveBeenCalledOnce();
    });
  });
});
