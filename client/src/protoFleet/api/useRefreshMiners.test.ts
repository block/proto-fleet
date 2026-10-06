import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import useRefreshMiners from "./useRefreshMiners";
import {
  MinerStateSnapshotSchema,
  type RefreshMinersResponse,
  RefreshMinersResponseSchema,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

const { mockHandleAuthErrors, mockRefreshMiners } = vi.hoisted(() => ({
  mockHandleAuthErrors: vi.fn(),
  mockRefreshMiners: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  fleetManagementClient: {
    refreshMiners: mockRefreshMiners,
  },
}));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({ handleAuthErrors: mockHandleAuthErrors }),
}));

describe("useRefreshMiners", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("rejects empty device arrays before calling the RPC", async () => {
    const { result } = renderHook(() => useRefreshMiners());

    await expect(result.current.refreshMiners([])).rejects.toThrow("At least one miner is required.");
    expect(mockRefreshMiners).not.toHaveBeenCalled();
  });

  it("rejects more than 50 devices before calling the RPC", async () => {
    const { result } = renderHook(() => useRefreshMiners());
    const deviceIds = Array.from({ length: 51 }, (_, index) => `miner-${index}`);

    await expect(result.current.refreshMiners(deviceIds)).rejects.toThrow("Refresh is limited to 50 miners.");
    expect(mockRefreshMiners).not.toHaveBeenCalled();
  });

  it("returns snapshots and errors from the RPC", async () => {
    const response = create(RefreshMinersResponseSchema, {
      snapshots: [create(MinerStateSnapshotSchema, { deviceIdentifier: "miner-1" })],
      errors: { "miner-2": "not found" },
    });
    mockRefreshMiners.mockResolvedValue(response);

    const { result } = renderHook(() => useRefreshMiners());
    await expect(result.current.refreshMiners(["miner-1", "miner-2"])).resolves.toBe(response);

    expect(mockRefreshMiners).toHaveBeenCalledWith(
      expect.objectContaining({
        deviceIds: ["miner-1", "miner-2"],
      }),
      undefined,
    );
  });

  it("forwards an AbortSignal to the RPC as a call option", async () => {
    mockRefreshMiners.mockResolvedValue(create(RefreshMinersResponseSchema));
    const controller = new AbortController();

    const { result } = renderHook(() => useRefreshMiners());
    await result.current.refreshMiners(["miner-1"], controller.signal);

    expect(mockRefreshMiners).toHaveBeenCalledWith(expect.objectContaining({ deviceIds: ["miner-1"] }), {
      signal: controller.signal,
    });
  });

  it("tracks devices that are currently refreshing", async () => {
    let resolveRefresh: (response: RefreshMinersResponse) => void = () => undefined;
    const response = create(RefreshMinersResponseSchema);
    mockRefreshMiners.mockReturnValue(
      new Promise<RefreshMinersResponse>((resolve) => {
        resolveRefresh = resolve;
      }),
    );

    const { result } = renderHook(() => useRefreshMiners());
    let refreshPromise: Promise<RefreshMinersResponse> | undefined;

    act(() => {
      refreshPromise = result.current.refreshMiners(["miner-1"]);
    });

    expect(result.current.refreshing.has("miner-1")).toBe(true);

    await act(async () => {
      resolveRefresh(response);
      await refreshPromise;
    });

    expect(result.current.refreshing.has("miner-1")).toBe(false);
  });
});
