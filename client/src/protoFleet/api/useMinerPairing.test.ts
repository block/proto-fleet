import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { useMinerPairing } from "./useMinerPairing";
import {
  DeviceSchema,
  DiscoverRequestSchema,
  DiscoverResponseSchema,
} from "@/protoFleet/api/generated/pairing/v1/pairing_pb";

const { mockDiscover, mockHandleAuthErrors } = vi.hoisted(() => ({
  mockDiscover: vi.fn(),
  mockHandleAuthErrors: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  pairingClient: { discover: mockDiscover },
}));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({ handleAuthErrors: mockHandleAuthErrors }),
}));

describe("useMinerPairing discovery", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockHandleAuthErrors.mockImplementation(({ onError }) => onError());
  });

  it("retains devices before and after warnings, including a warning sharing a device response", async () => {
    const first = create(DeviceSchema, { deviceIdentifier: "first" });
    const second = create(DeviceSchema, { deviceIdentifier: "second" });
    const last = create(DeviceSchema, { deviceIdentifier: "last" });
    mockDiscover.mockImplementation(async function* () {
      yield create(DiscoverResponseSchema, { devices: [first] });
      yield { ...create(DiscoverResponseSchema), warning: "Fleet Node north: scan timed out" };
      yield {
        ...create(DiscoverResponseSchema, { devices: [second] }),
        warning: "Fleet Server: scan incomplete",
      };
      yield create(DiscoverResponseSchema, { devices: [last] });
    });
    const onStreamData = vi.fn();
    const onWarning = vi.fn();
    const onError = vi.fn();
    const { result } = renderHook(() => useMinerPairing());

    await act(async () => {
      await result.current.discover({
        discoverRequest: create(DiscoverRequestSchema),
        onStreamData,
        onWarning,
        onError,
      });
    });

    expect(onStreamData.mock.calls).toEqual([[[first]], [[second]], [[last]]]);
    expect(onWarning.mock.calls).toEqual([["Fleet Node north: scan timed out"], ["Fleet Server: scan incomplete"]]);
    expect(onError).not.toHaveBeenCalled();
    expect(mockHandleAuthErrors).not.toHaveBeenCalled();
    expect(result.current.discoverPending).toBe(false);
  });

  it("keeps stream failures on the error and authentication handling path", async () => {
    const failure = new ConnectError("Permission denied", Code.PermissionDenied);
    const first = create(DeviceSchema, { deviceIdentifier: "first" });
    mockDiscover.mockImplementation(async function* () {
      yield create(DiscoverResponseSchema, { devices: [first] });
      throw failure;
    });
    const onStreamData = vi.fn();
    const onWarning = vi.fn();
    const onError = vi.fn();
    const { result } = renderHook(() => useMinerPairing());

    await act(async () => {
      await result.current.discover({
        discoverRequest: create(DiscoverRequestSchema),
        onStreamData,
        onWarning,
        onError,
      });
    });

    expect(onStreamData).toHaveBeenCalledWith([first]);
    expect(mockHandleAuthErrors).toHaveBeenCalledWith({ error: failure, onError: expect.any(Function) });
    expect(onError).toHaveBeenCalledWith("Permission denied");
    expect(onWarning).not.toHaveBeenCalled();
    expect(result.current.discoverPending).toBe(false);
  });

  it.each(["warning", "error"])("keeps user cancellation quiet when the stream ends with a %s", async (ending) => {
    const controller = new AbortController();
    mockDiscover.mockImplementation(async function* () {
      controller.abort();
      if (ending === "error") throw new ConnectError("Canceled", Code.Canceled);
      yield { ...create(DiscoverResponseSchema), warning: "Fleet Server: scan canceled" };
    });
    const onStreamData = vi.fn();
    const onWarning = vi.fn();
    const onError = vi.fn();
    const { result } = renderHook(() => useMinerPairing());
    const discoverRequest = create(DiscoverRequestSchema);

    await act(async () => {
      await result.current.discover({
        discoverRequest,
        discoverAbortController: controller,
        onStreamData,
        onWarning,
        onError,
      });
    });

    expect(mockDiscover).toHaveBeenCalledWith(discoverRequest, { signal: controller.signal });
    expect(onStreamData).not.toHaveBeenCalled();
    expect(onWarning).not.toHaveBeenCalled();
    expect(onError).not.toHaveBeenCalled();
    expect(mockHandleAuthErrors).not.toHaveBeenCalled();
    expect(result.current.discoverPending).toBe(false);
  });
});
