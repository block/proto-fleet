import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { activityClient } from "./clients";
import { useActivityFilterOptions } from "./useActivityFilterOptions";
import { ListActivityFilterOptionsResponseSchema } from "@/protoFleet/api/generated/activity/v1/activity_pb";

vi.mock("./clients", () => ({
  activityClient: {
    listActivityFilterOptions: vi.fn(),
  },
}));

const mockHandleAuthErrors = vi.fn(({ onError }) => onError?.(new Error("auth error")));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: vi.fn(() => ({
    handleAuthErrors: mockHandleAuthErrors,
  })),
}));

const optionsResponse = () =>
  create(ListActivityFilterOptionsResponseSchema, {
    eventTypes: [{ eventType: "login", eventCategory: "auth" }],
    scopeTypes: ["device"],
    users: [{ userId: "u1", username: "operator" }],
  });

describe("useActivityFilterOptions", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("does not fetch while disabled and fetches once enabled", async () => {
    vi.mocked(activityClient.listActivityFilterOptions).mockResolvedValue(optionsResponse());

    const { result, rerender } = renderHook(({ enabled }) => useActivityFilterOptions({ enabled }), {
      initialProps: { enabled: false },
    });

    expect(activityClient.listActivityFilterOptions).not.toHaveBeenCalled();
    expect(result.current.eventTypes).toEqual([]);

    rerender({ enabled: true });

    await waitFor(() => expect(result.current.eventTypes).toHaveLength(1));
    expect(activityClient.listActivityFilterOptions).toHaveBeenCalledTimes(1);
  });

  it("stops exposing loaded options when disabled", async () => {
    vi.mocked(activityClient.listActivityFilterOptions).mockResolvedValue(optionsResponse());

    const { result, rerender } = renderHook(({ enabled }) => useActivityFilterOptions({ enabled }), {
      initialProps: { enabled: true },
    });

    await waitFor(() => expect(result.current.users).toHaveLength(1));

    rerender({ enabled: false });

    expect(result.current.eventTypes).toEqual([]);
    expect(result.current.scopeTypes).toEqual([]);
    expect(result.current.users).toEqual([]);
    expect(result.current.isLoading).toBe(false);
  });

  it("drops a response that lands after the hook is disabled", async () => {
    let resolveResponse!: (value: ReturnType<typeof optionsResponse>) => void;
    vi.mocked(activityClient.listActivityFilterOptions).mockReturnValue(
      new Promise((resolve) => {
        resolveResponse = resolve;
      }),
    );

    const { result, rerender } = renderHook(({ enabled }) => useActivityFilterOptions({ enabled }), {
      initialProps: { enabled: true },
    });
    expect(result.current.isLoading).toBe(true);

    rerender({ enabled: false });
    expect(result.current.isLoading).toBe(false);

    await act(async () => {
      resolveResponse(optionsResponse());
    });

    expect(result.current.users).toEqual([]);
    expect(result.current.isLoading).toBe(false);
  });

  it("ignores a stale response that lands after a newer request settles", async () => {
    let resolveStale!: (value: ReturnType<typeof optionsResponse>) => void;
    vi.mocked(activityClient.listActivityFilterOptions)
      .mockReturnValueOnce(
        new Promise((resolve) => {
          resolveStale = resolve;
        }),
      )
      .mockResolvedValueOnce(optionsResponse());

    const { result, rerender } = renderHook(({ enabled }) => useActivityFilterOptions({ enabled }), {
      initialProps: { enabled: true },
    });

    rerender({ enabled: false });
    rerender({ enabled: true });

    await waitFor(() => expect(result.current.users).toHaveLength(1));

    await act(async () => {
      resolveStale(
        create(ListActivityFilterOptionsResponseSchema, {
          users: [
            { userId: "stale-1", username: "stale" },
            { userId: "stale-2", username: "staler" },
          ],
        }),
      );
    });

    expect(result.current.users.map((user) => user.userId)).toEqual(["u1"]);
  });
});
