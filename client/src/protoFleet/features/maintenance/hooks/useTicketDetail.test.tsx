import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const getTicket = vi.fn();
const updateTicket = vi.fn();
const createComment = vi.fn();
const deleteComment = vi.fn();
vi.mock("@/protoFleet/api/maintenance", () => ({
  useMaintenanceApi: () => ({ getTicket, updateTicket, createComment, deleteComment }),
}));
vi.mock("../mappers", () => ({
  toTicketDetail: (value: { ticket: { id: bigint } }) => ({ id: value.ticket.id.toString() }),
}));
const { useTicketDetail } = await import("./useTicketDetail");

beforeEach(() => {
  vi.clearAllMocks();
});
afterEach(() => {
  vi.useRealTimers();
});
it("loads the selected ticket ID", async () => {
  getTicket.mockImplementation(async ({ onSuccess }) => onSuccess({ ticket: { id: 9n } }));
  const { result } = renderHook(() => useTicketDetail("9"));
  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(getTicket).toHaveBeenCalledWith(expect.objectContaining({ id: 9n }));
  expect(result.current.data).toEqual({ id: "9" });
});
it("keeps ticket detail stable until explicitly refreshed", async () => {
  vi.useFakeTimers();
  getTicket.mockImplementation(async ({ onSuccess }) => onSuccess({ ticket: { id: 9n } }));
  const { result, unmount } = renderHook(() => useTicketDetail("9"));
  await act(async () => vi.advanceTimersByTimeAsync(0));
  expect(getTicket).toHaveBeenCalledTimes(1);
  expect(result.current.data).toEqual({ id: "9" });

  await act(async () => vi.advanceTimersByTimeAsync(30_000));
  expect(getTicket).toHaveBeenCalledTimes(1);
  await act(() => result.current.refresh());
  expect(getTicket).toHaveBeenCalledTimes(2);
  expect(result.current.data).toEqual({ id: "9" });

  unmount();
  await act(async () => vi.advanceTimersByTimeAsync(15_000));
  expect(getTicket).toHaveBeenCalledTimes(2);
});

it("clears retained detail when refresh reports remote deletion", async () => {
  vi.useFakeTimers();
  let deleted = false;
  getTicket.mockImplementation(async ({ onSuccess, onNotFound, onError }) => {
    if (deleted) {
      onNotFound();
      onError("ticket not found");
    } else onSuccess({ ticket: { id: 9n } });
  });
  const { result } = renderHook(() => useTicketDetail("9"));
  await act(async () => vi.advanceTimersByTimeAsync(0));
  expect(result.current.data).toEqual({ id: "9" });

  deleted = true;
  await act(() => result.current.refresh());

  expect(result.current.data).toBeNull();
  expect(result.current.error).toBe("ticket not found");
});

it("clears the prior ticket while a newly selected ticket loads", async () => {
  let resolveSecond: ((value: unknown) => void) | undefined;
  getTicket.mockImplementation(({ id, onSuccess }) => {
    if (id === 1n) {
      onSuccess({ ticket: { id: 1n } });
      return Promise.resolve();
    }
    return new Promise((resolve) => {
      resolveSecond = resolve;
    });
  });
  const { result, rerender } = renderHook(({ id }) => useTicketDetail(id), {
    initialProps: { id: "1" },
  });
  await waitFor(() => expect(result.current.data).toEqual({ id: "1" }));

  rerender({ id: "2" });
  await waitFor(() => expect(getTicket).toHaveBeenCalledWith(expect.objectContaining({ id: 2n })));
  expect(result.current.loading).toBe(true);
  expect(result.current.data).toBeNull();

  await act(async () => resolveSecond?.(undefined));
});

it("does not let an old ticket mutation cancel the newly selected ticket load", async () => {
  let finishUpdate: (() => void) | undefined;
  let secondSignal: AbortSignal | undefined;
  getTicket.mockImplementation(({ id, signal, onSuccess }) => {
    if (id === 1n) {
      onSuccess({ ticket: { id: 1n } });
      return Promise.resolve();
    }
    secondSignal = signal;
    return new Promise(() => undefined);
  });
  updateTicket.mockImplementation(
    ({ onSuccess }) =>
      new Promise<void>((resolve) => {
        finishUpdate = () => {
          onSuccess();
          resolve();
        };
      }),
  );
  const { result, rerender } = renderHook(({ id }) => useTicketDetail(id), { initialProps: { id: "1" } });
  await waitFor(() => expect(result.current.data).toEqual({ id: "1" }));

  let updatePromise: Promise<boolean> | undefined;
  act(() => {
    updatePromise = result.current.update({
      urgent: true,
      expectedUpdatedAt: { $typeName: "google.protobuf.Timestamp", seconds: 1700000000n, nanos: 123456000 },
    });
  });
  rerender({ id: "2" });
  await waitFor(() => expect(secondSignal).toBeDefined());

  await act(async () => finishUpdate?.());
  await updatePromise;

  expect(getTicket).toHaveBeenCalledTimes(2);
  expect(secondSignal?.aborted).toBe(false);
});

it("clears state without an RPC when no ticket is selected", async () => {
  const { result } = renderHook(() => useTicketDetail(null));
  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(getTicket).not.toHaveBeenCalled();
});
