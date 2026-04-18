import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";

const mockListDeviceSetMembers = vi.fn();

vi.mock("./clients", () => ({
  deviceSetClient: {
    listDeviceSetMembers: (...args: unknown[]) => mockListDeviceSetMembers(...args),
  },
}));

const mockHandleAuthErrors = vi.fn();

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: vi.fn(() => ({
    handleAuthErrors: mockHandleAuthErrors,
  })),
}));

// Import after mocks are set up
const { useDeviceSets } = await import("./useDeviceSets");

describe("useDeviceSets — listGroupMembers", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockHandleAuthErrors.mockImplementation(({ onError }: { onError: () => void }) => onError());
  });

  it("returns member IDs via onSuccess on normal completion", async () => {
    mockListDeviceSetMembers.mockResolvedValueOnce({
      members: [{ deviceIdentifier: "d1" }, { deviceIdentifier: "d2" }],
      nextPageToken: "",
    });

    const onSuccess = vi.fn();
    const onFinally = vi.fn();

    const { result } = renderHook(() => useDeviceSets());

    await act(async () => {
      await result.current.listGroupMembers({
        deviceSetId: 1n,
        onSuccess,
        onFinally,
      });
    });

    expect(onSuccess).toHaveBeenCalledWith(["d1", "d2"]);
    expect(onFinally).toHaveBeenCalledTimes(1);
  });

  it("does not call onError or handleAuthErrors when AbortError is thrown", async () => {
    mockListDeviceSetMembers.mockRejectedValueOnce(new DOMException("aborted", "AbortError"));

    const onSuccess = vi.fn();
    const onError = vi.fn();
    const onFinally = vi.fn();

    const { result } = renderHook(() => useDeviceSets());

    await act(async () => {
      await result.current.listGroupMembers({
        deviceSetId: 1n,
        onSuccess,
        onError,
        onFinally,
      });
    });

    expect(onSuccess).not.toHaveBeenCalled();
    expect(onError).not.toHaveBeenCalled();
    expect(mockHandleAuthErrors).not.toHaveBeenCalled();
    expect(onFinally).toHaveBeenCalledTimes(1);
  });

  it("does not call onError when ConnectError with Canceled code is thrown after signal abort", async () => {
    const controller = new AbortController();
    controller.abort();

    mockListDeviceSetMembers.mockRejectedValueOnce(new ConnectError("canceled", Code.Canceled));

    const onError = vi.fn();
    const onFinally = vi.fn();

    const { result } = renderHook(() => useDeviceSets());

    await act(async () => {
      await result.current.listGroupMembers({
        deviceSetId: 1n,
        signal: controller.signal,
        onError,
        onFinally,
      });
    });

    expect(onError).not.toHaveBeenCalled();
    expect(mockHandleAuthErrors).not.toHaveBeenCalled();
    expect(onFinally).toHaveBeenCalledTimes(1);
  });

  it("calls handleAuthErrors when ConnectError with Canceled code is thrown without an aborted signal", async () => {
    mockListDeviceSetMembers.mockRejectedValueOnce(new ConnectError("canceled", Code.Canceled));

    const onError = vi.fn();
    const onFinally = vi.fn();

    const { result } = renderHook(() => useDeviceSets());

    await act(async () => {
      await result.current.listGroupMembers({
        deviceSetId: 1n,
        onError,
        onFinally,
      });
    });

    expect(mockHandleAuthErrors).toHaveBeenCalledTimes(1);
    expect(onError).toHaveBeenCalledTimes(1);
    expect(onFinally).toHaveBeenCalledTimes(1);
  });

  it("still calls handleAuthErrors for Unauthenticated error even if signal is aborted", async () => {
    const controller = new AbortController();
    controller.abort();

    mockListDeviceSetMembers.mockRejectedValueOnce(new ConnectError("session expired", Code.Unauthenticated));

    const onError = vi.fn();
    const onFinally = vi.fn();

    const { result } = renderHook(() => useDeviceSets());

    await act(async () => {
      await result.current.listGroupMembers({
        deviceSetId: 1n,
        signal: controller.signal,
        onError,
        onFinally,
      });
    });

    expect(mockHandleAuthErrors).toHaveBeenCalledTimes(1);
    expect(onFinally).toHaveBeenCalledTimes(1);
  });

  it("calls onError via handleAuthErrors for non-abort RPC errors", async () => {
    mockListDeviceSetMembers.mockRejectedValueOnce(new ConnectError("internal error", Code.Internal));

    const onError = vi.fn();
    const onFinally = vi.fn();

    const { result } = renderHook(() => useDeviceSets());

    await act(async () => {
      await result.current.listGroupMembers({
        deviceSetId: 1n,
        onError,
        onFinally,
      });
    });

    expect(mockHandleAuthErrors).toHaveBeenCalledTimes(1);
    expect(onFinally).toHaveBeenCalledTimes(1);
  });
});
