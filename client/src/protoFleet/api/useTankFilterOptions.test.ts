import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { ListTankOptionsResponseSchema } from "./generated/fleetinventory/v1/fleetinventory_pb";
import { useTankFilterOptions } from "./useTankFilterOptions";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";

const { listTankOptions } = vi.hoisted(() => ({ listTankOptions: vi.fn() }));
vi.mock("./clients", () => ({ fleetInventoryClient: { listTankOptions } }));

const page = (id: bigint, name: string, siteName = "", nextPageToken = "") =>
  create(ListTankOptionsResponseSchema, {
    options: [{ id, name, siteName }],
    nextPageToken,
  });

beforeEach(() => listTankOptions.mockReset());
afterEach(() => vi.useRealTimers());

it("loads all readable tanks before publishing options with container/site context", async () => {
  let finish!: (value: ReturnType<typeof page>) => void;
  listTankOptions.mockResolvedValueOnce(page(7n, "North CT1 T1", "North", "next"));
  listTankOptions.mockImplementationOnce(() => new Promise((resolve) => (finish = resolve)));
  const { result } = renderHook(() => useTankFilterOptions(true));
  await waitFor(() => expect(listTankOptions).toHaveBeenCalledTimes(2));
  expect(result.current).toEqual([]);
  expect(listTankOptions.mock.calls[0][0]).toEqual({
    pageSize: 1000,
    pageToken: "",
  });
  expect(listTankOptions.mock.calls[1][0].pageToken).toBe("next");
  await act(async () => finish(page(8n, "South CT1 T1")));
  expect(result.current).toEqual([
    { id: "7", label: "North CT1 T1 · North" },
    { id: "8", label: "South CT1 T1 · Unassigned" },
  ]);
});

it("deduplicates overlapping cursor pages by Fleet tank identity", async () => {
  listTankOptions.mockResolvedValueOnce(page(7n, "CT1 T1", "North", "next"));
  listTankOptions.mockResolvedValueOnce(page(7n, "Renamed CT1 T1", "North"));
  const { result } = renderHook(() => useTankFilterOptions(true));
  await waitFor(() => expect(result.current).toEqual([{ id: "7", label: "Renamed CT1 T1 · North" }]));
});

it("discards partial options when a later page fails or repeats its cursor", async () => {
  listTankOptions.mockResolvedValueOnce(page(7n, "CT1 T1", "North", "next"));
  listTankOptions.mockRejectedValueOnce(new Error("Unavailable"));
  const { result, unmount } = renderHook(() => useTankFilterOptions(true));
  await waitFor(() => expect(listTankOptions).toHaveBeenCalledTimes(2));
  expect(result.current).toEqual([]);
  unmount();
  listTankOptions.mockReset().mockResolvedValue(page(7n, "CT1 T1", "North", "repeat"));
  const repeated = renderHook(() => useTankFilterOptions(true));
  await waitFor(() => expect(listTankOptions).toHaveBeenCalledTimes(2));
  expect(repeated.result.current).toEqual([]);
});

it("clears unreadable options on polling failure and cancels work when access is revoked", async () => {
  vi.useFakeTimers();
  listTankOptions.mockResolvedValueOnce(page(7n, "CT1 T1", "North"));
  const { result, rerender } = renderHook(({ enabled }) => useTankFilterOptions(enabled), {
    initialProps: { enabled: true },
  });
  await act(async () => {});
  expect(result.current).toHaveLength(1);
  listTankOptions.mockRejectedValueOnce(new Error("Permission denied"));
  await act(async () => vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS));
  expect(result.current).toEqual([]);
  let finish!: (value: ReturnType<typeof page>) => void;
  listTankOptions.mockImplementationOnce(() => new Promise((resolve) => (finish = resolve)));
  await act(async () => vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS));
  const signal = listTankOptions.mock.calls[2][1].signal as AbortSignal;
  rerender({ enabled: false });
  expect(signal.aborted).toBe(true);
  await act(async () => finish(page(8n, "Late CT1 T1")));
  expect(result.current).toEqual([]);
});

it("does not fetch without inventory access", () => {
  const { result } = renderHook(() => useTankFilterOptions(false));
  expect(listTankOptions).not.toHaveBeenCalled();
  expect(result.current).toEqual([]);
});
