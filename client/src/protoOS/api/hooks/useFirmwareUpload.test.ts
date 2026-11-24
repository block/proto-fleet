import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useFirmwareUpload } from "./useFirmwareUpload";

const mocks = vi.hoisted(() => ({
  upload: vi.fn(),
  reload: vi.fn(),
  hosted: false,
  adapter: {} as { uploadFirmware?: ReturnType<typeof vi.fn> },
}));

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ adapter: mocks.adapter, isFleetHosted: mocks.hosted }),
}));
vi.mock("@/protoOS/store", () => ({
  useAuthRetry:
    () =>
    async ({
      request,
      onSuccess,
      onError,
      signal,
    }: {
      request: (params: { signal: AbortSignal }) => Promise<void>;
      onSuccess: () => void;
      onError: (error: unknown) => void;
      signal: AbortSignal;
    }) => {
      try {
        await request({ signal });
        onSuccess();
      } catch (error) {
        onError(error);
      }
    },
}));

const file = new File(["firmware"], "r2.swu");

describe("useFirmwareUpload", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    mocks.adapter = { uploadFirmware: mocks.upload };
    mocks.hosted = false;
  });

  it("uploads once, prevents duplicate submissions, and refreshes device status", async () => {
    let finish!: () => void;
    mocks.upload.mockReturnValue(
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
    );
    const { result } = renderHook(() => useFirmwareUpload(mocks.reload));
    let request!: Promise<boolean>;
    act(() => {
      request = result.current.uploadFirmware(file);
    });
    expect(result.current.pending).toBe(true);
    await act(async () => {
      expect(await result.current.uploadFirmware(file)).toBe(false);
    });
    expect(mocks.upload).toHaveBeenCalledTimes(1);
    expect(mocks.upload).toHaveBeenCalledWith(file, { signal: expect.any(AbortSignal) });
    await act(async () => {
      finish();
      expect(await request).toBe(true);
    });
    expect(result.current.pending).toBe(false);
    expect(mocks.reload).toHaveBeenCalledTimes(1);
  });

  it.each([400, 401, 409, 413, 422, 500, undefined])("keeps a failed upload recoverable (%s)", async (status) => {
    mocks.upload.mockRejectedValue(status ? { status } : new Error("offline"));
    const { result } = renderHook(() => useFirmwareUpload(mocks.reload));
    await act(async () => {
      expect(await result.current.uploadFirmware(file)).toBe(false);
    });
    expect(result.current.error).toBeTruthy();
    expect(result.current.pending).toBe(false);
    expect(mocks.reload).toHaveBeenCalledOnce();
    mocks.upload.mockResolvedValue(undefined);
    await act(async () => {
      expect(await result.current.uploadFirmware(file)).toBe(true);
    });
    expect(result.current.error).toBeUndefined();
  });

  it("clears a failed upload's error on request", async () => {
    mocks.upload.mockRejectedValue({ status: 400 });
    const { result } = renderHook(() => useFirmwareUpload(mocks.reload));
    await act(async () => {
      await result.current.uploadFirmware(file);
    });
    expect(result.current.error).toMatch(/rejected this firmware file/);
    act(() => result.current.clearError());
    expect(result.current.error).toBeUndefined();
  });

  it.each([new File([], "empty.swu"), new File(["x"], "wrong.zip")])("rejects invalid selections", async (invalid) => {
    const { result } = renderHook(() => useFirmwareUpload(mocks.reload));
    await act(async () => {
      expect(await result.current.uploadFirmware(invalid)).toBe(false);
    });
    expect(mocks.upload).not.toHaveBeenCalled();
    expect(result.current.error).toMatch(/non-empty .swu/);
  });

  it.each(["fleet", "unsupported"])("does not send uploads for %s connections", async (mode) => {
    mocks.hosted = mode === "fleet";
    if (mode === "unsupported") mocks.adapter = {};
    const { result } = renderHook(() => useFirmwareUpload(mocks.reload));
    expect(result.current.supported).toBe(false);
    await act(async () => {
      expect(await result.current.uploadFirmware(file)).toBe(false);
    });
    expect(mocks.upload).not.toHaveBeenCalled();
  });

  it.each(["unmount", "connection change"])("aborts on %s without publishing stale success", async (action) => {
    let finish!: () => void;
    mocks.upload.mockReturnValue(
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
    );
    const { result, unmount, rerender } = renderHook(() => useFirmwareUpload(mocks.reload));
    let request!: Promise<boolean>;
    act(() => {
      request = result.current.uploadFirmware(file);
    });
    const signal = mocks.upload.mock.calls[0][1].signal as AbortSignal;
    if (action === "unmount") unmount();
    else {
      mocks.adapter = {};
      rerender();
    }
    expect(signal.aborted).toBe(true);
    await act(async () => {
      finish();
      expect(await request).toBe(false);
    });
    expect(mocks.reload).not.toHaveBeenCalled();
  });
});
