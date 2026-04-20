import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useFirmwareUpload } from "./useFirmwareUpload";

const mockGetConfig = vi.fn();
const mockCheckFirmwareFile = vi.fn();
const mockUploadFirmwareFile = vi.fn();

vi.mock("@/protoFleet/api/useFirmwareApi", () => ({
  useFirmwareApi: () => ({
    getConfig: mockGetConfig,
    checkFirmwareFile: mockCheckFirmwareFile,
    uploadFirmwareFile: mockUploadFirmwareFile,
  }),
  computeSha256: vi.fn().mockResolvedValue("abc123sha256"),
  validateFirmwareFile: vi.fn().mockReturnValue(null),
}));

const defaultConfig = {
  allowedExtensions: [".swu", ".tar.gz", ".zip"],
  maxFileSizeBytes: 500 * 1024 * 1024,
  chunkSizeBytes: 32 * 1024 * 1024,
};

beforeEach(() => {
  vi.clearAllMocks();
  mockGetConfig.mockResolvedValue(defaultConfig);
  mockCheckFirmwareFile.mockResolvedValue({ exists: false });
  mockUploadFirmwareFile.mockResolvedValue("fw-new-id");
});

describe("useFirmwareUpload", () => {
  describe("initial state", () => {
    it("returns idle state when inactive", () => {
      const { result } = renderHook(() => useFirmwareUpload(false));

      expect(result.current.state).toBe("idle");
      expect(result.current.file).toBeNull();
      expect(result.current.firmwareFileId).toBeNull();
      expect(result.current.uploadProgress).toBe(0);
      expect(result.current.errorMessage).toBeNull();
      expect(result.current.serverConfig).toBeNull();
    });

    it("does not fetch config when inactive", () => {
      renderHook(() => useFirmwareUpload(false));

      expect(mockGetConfig).not.toHaveBeenCalled();
    });
  });

  describe("config loading", () => {
    it("fetches config when active", async () => {
      const { result } = renderHook(() => useFirmwareUpload(true));

      await vi.waitFor(() => {
        expect(result.current.serverConfig).toEqual(defaultConfig);
      });
      expect(result.current.state).toBe("idle");
    });

    it("sets error state when config fetch fails", async () => {
      mockGetConfig.mockRejectedValue(new Error("Network error"));

      const { result } = renderHook(() => useFirmwareUpload(true));

      await vi.waitFor(() => {
        expect(result.current.state).toBe("error");
      });
      expect(result.current.errorMessage).toBe("Network error");
      expect(result.current.serverConfig).toBeNull();
    });

    it("retry re-fetches config after failure", async () => {
      mockGetConfig.mockRejectedValueOnce(new Error("Network error"));

      const { result } = renderHook(() => useFirmwareUpload(true));

      await vi.waitFor(() => {
        expect(result.current.state).toBe("error");
      });

      mockGetConfig.mockResolvedValue(defaultConfig);

      act(() => {
        result.current.retry();
      });

      await vi.waitFor(() => {
        expect(result.current.serverConfig).toEqual(defaultConfig);
      });
      expect(result.current.state).toBe("idle");
      expect(result.current.errorMessage).toBeNull();
      expect(mockGetConfig).toHaveBeenCalledTimes(2);
    });
  });

  describe("processFile", () => {
    it("completes upload when file does not exist on server", async () => {
      const { result } = renderHook(() => useFirmwareUpload(true));

      await vi.waitFor(() => {
        expect(result.current.serverConfig).not.toBeNull();
      });

      const file = new File(["data"], "firmware.swu");

      act(() => {
        result.current.processFile(file);
      });

      await vi.waitFor(() => {
        expect(result.current.state).toBe("ready");
      });
      expect(result.current.firmwareFileId).toBe("fw-new-id");
      expect(result.current.file).toBe(file);
      expect(mockUploadFirmwareFile).toHaveBeenCalled();
    });

    it("skips upload when file already exists on server (SHA-256 dedup)", async () => {
      mockCheckFirmwareFile.mockResolvedValue({ exists: true, firmwareFileId: "fw-existing" });

      const { result } = renderHook(() => useFirmwareUpload(true));

      await vi.waitFor(() => {
        expect(result.current.serverConfig).not.toBeNull();
      });

      const file = new File(["data"], "firmware.swu");

      act(() => {
        result.current.processFile(file);
      });

      await vi.waitFor(() => {
        expect(result.current.state).toBe("ready");
      });
      expect(result.current.firmwareFileId).toBe("fw-existing");
      expect(mockUploadFirmwareFile).not.toHaveBeenCalled();
    });

    it("falls back to getConfig when called before config loads", async () => {
      mockGetConfig.mockReturnValueOnce(new Promise(() => {}));
      mockGetConfig.mockResolvedValueOnce(defaultConfig);

      const { result } = renderHook(() => useFirmwareUpload(true));

      expect(result.current.serverConfig).toBeNull();

      act(() => {
        result.current.processFile(new File(["data"], "firmware.swu"));
      });

      await vi.waitFor(() => {
        expect(result.current.state).toBe("ready");
      });
      expect(result.current.firmwareFileId).toBe("fw-new-id");
      expect(mockGetConfig).toHaveBeenCalledTimes(2);
    });

    it("sets error state when check fails", async () => {
      mockCheckFirmwareFile.mockRejectedValue(new Error("Check failed"));

      const { result } = renderHook(() => useFirmwareUpload(true));

      await vi.waitFor(() => {
        expect(result.current.serverConfig).not.toBeNull();
      });

      act(() => {
        result.current.processFile(new File(["data"], "firmware.swu"));
      });

      await vi.waitFor(() => {
        expect(result.current.state).toBe("error");
      });
      expect(result.current.errorMessage).toBe("Check failed");
      expect(mockUploadFirmwareFile).not.toHaveBeenCalled();
    });

    it("aborts previous upload when processFile is called again", async () => {
      let resolveFirstUpload: (value: string) => void;
      mockUploadFirmwareFile
        .mockImplementationOnce(() => new Promise<string>((resolve) => (resolveFirstUpload = resolve)))
        .mockResolvedValueOnce("fw-second-id");

      const { result } = renderHook(() => useFirmwareUpload(true));

      await vi.waitFor(() => {
        expect(result.current.serverConfig).not.toBeNull();
      });

      act(() => {
        result.current.processFile(new File(["data"], "first.swu"));
      });

      await vi.waitFor(() => {
        expect(result.current.state).toBe("uploading");
      });

      act(() => {
        result.current.processFile(new File(["data"], "second.swu"));
      });

      await vi.waitFor(() => {
        expect(result.current.state).toBe("ready");
      });

      act(() => {
        resolveFirstUpload!("fw-first-id");
      });

      expect(result.current.firmwareFileId).toBe("fw-second-id");
      expect(result.current.file?.name).toBe("second.swu");
    });

    it("sets error state when upload fails", async () => {
      mockUploadFirmwareFile.mockRejectedValue(new Error("Upload failed"));

      const { result } = renderHook(() => useFirmwareUpload(true));

      await vi.waitFor(() => {
        expect(result.current.serverConfig).not.toBeNull();
      });

      act(() => {
        result.current.processFile(new File(["data"], "firmware.swu"));
      });

      await vi.waitFor(() => {
        expect(result.current.state).toBe("error");
      });
      expect(result.current.errorMessage).toBe("Upload failed");
    });

    it("sets error state on validation failure", async () => {
      const { validateFirmwareFile } = await import("@/protoFleet/api/useFirmwareApi");
      vi.mocked(validateFirmwareFile).mockReturnValueOnce("Unsupported file type");

      const { result } = renderHook(() => useFirmwareUpload(true));

      await vi.waitFor(() => {
        expect(result.current.serverConfig).not.toBeNull();
      });

      act(() => {
        result.current.processFile(new File(["data"], "firmware.bin"));
      });

      await vi.waitFor(() => {
        expect(result.current.state).toBe("error");
      });
      expect(result.current.errorMessage).toBe("Unsupported file type");
    });
  });

  describe("reset", () => {
    it("clears all upload state back to idle", async () => {
      mockUploadFirmwareFile.mockRejectedValue(new Error("fail"));

      const { result } = renderHook(() => useFirmwareUpload(true));

      await vi.waitFor(() => {
        expect(result.current.serverConfig).not.toBeNull();
      });

      act(() => {
        result.current.processFile(new File(["data"], "firmware.swu"));
      });

      await vi.waitFor(() => {
        expect(result.current.state).toBe("error");
      });

      act(() => {
        result.current.reset();
      });

      expect(result.current.state).toBe("idle");
      expect(result.current.file).toBeNull();
      expect(result.current.firmwareFileId).toBeNull();
      expect(result.current.errorMessage).toBeNull();
      expect(result.current.uploadProgress).toBe(0);
    });
  });

  describe("cleanup", () => {
    it("aborts in-flight operations when active flips to false", async () => {
      let resolveUpload: (value: string) => void;
      mockUploadFirmwareFile.mockImplementation(() => new Promise<string>((resolve) => (resolveUpload = resolve)));

      const { result, rerender } = renderHook(({ active }) => useFirmwareUpload(active), {
        initialProps: { active: true },
      });

      await vi.waitFor(() => {
        expect(result.current.serverConfig).not.toBeNull();
      });

      act(() => {
        result.current.processFile(new File(["data"], "firmware.swu"));
      });

      await vi.waitFor(() => {
        expect(result.current.state).toBe("uploading");
      });

      rerender({ active: false });

      act(() => {
        resolveUpload!("fw-id");
      });

      expect(result.current.state).not.toBe("ready");
    });
  });
});
