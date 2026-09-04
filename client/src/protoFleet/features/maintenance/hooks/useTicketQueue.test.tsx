import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import {
  RepairTicketSchema,
  RepairTicketSummarySchema,
} from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";

const listTickets = vi.fn();
const getStats = vi.fn();
const bulkUpdate = vi.fn();
const updateTicket = vi.fn();
vi.mock("@/protoFleet/api/maintenance", () => ({
  useMaintenanceApi: () => ({ listTickets, getStats, bulkUpdate, updateTicket }),
}));
const { useTicketQueue } = await import("./useTicketQueue");

beforeEach(() => {
  vi.clearAllMocks();
  listTickets.mockImplementation(async ({ onSuccess }) =>
    onSuccess({
      tickets: [create(RepairTicketSummarySchema, { ticket: create(RepairTicketSchema, { id: 1n }) })],
      nextPageToken: "",
      totalCount: 1,
    }),
  );
  getStats.mockImplementation(async ({ onSuccess }) =>
    onSuccess({
      openCount: 1,
      inProgressCount: 0,
      onHoldCount: 0,
      sentToVendorCount: 0,
      overdueCount: 0,
      urgentCount: 0,
    }),
  );
});

afterEach(() => {
  vi.useRealTimers();
});

it("polls the current queue page and stops polling after unmount", async () => {
  vi.useFakeTimers();
  const { result, unmount } = renderHook(() => useTicketQueue());
  await act(async () => vi.advanceTimersByTimeAsync(0));
  expect(result.current.loading).toBe(false);
  expect(listTickets).toHaveBeenCalledTimes(1);

  await act(async () => vi.advanceTimersByTimeAsync(15_000));
  expect(listTickets).toHaveBeenCalledTimes(2);

  unmount();
  await act(async () => vi.advanceTimersByTimeAsync(15_000));
  expect(listTickets).toHaveBeenCalledTimes(2);
});

it("loads tickets and refreshes after a successful mutation", async () => {
  bulkUpdate.mockImplementation(async ({ onSuccess }) => onSuccess(1));
  const { result } = renderHook(() => useTicketQueue());
  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(result.current.data[0].id).toBe("1");
  await act(() => result.current.bulkUpdate(["1"], { case: "setStatus", value: 2 }));
  expect(listTickets).toHaveBeenCalledTimes(2);
});

it("sets urgent false through the single-ticket update and refreshes", async () => {
  updateTicket.mockImplementation(async ({ onSuccess }) => onSuccess());
  const { result } = renderHook(() => useTicketQueue());
  await waitFor(() => expect(result.current.loading).toBe(false));
  await act(() => result.current.setUrgent("1", false));
  expect(updateTicket).toHaveBeenCalledWith(expect.objectContaining({ id: 1n, urgent: false }));
  expect(listTickets).toHaveBeenCalledTimes(2);
});

it("replaces rows when moving through cursor-backed list pages", async () => {
  listTickets.mockImplementation(async ({ pageToken, onSuccess }) =>
    onSuccess({
      tickets: [
        create(RepairTicketSummarySchema, {
          ticket: create(RepairTicketSchema, { id: pageToken === "cursor-2" ? 2n : 1n }),
        }),
      ],
      nextPageToken: pageToken ? "" : "cursor-2",
      totalCount: 2,
    }),
  );
  const { result } = renderHook(() => useTicketQueue());
  await waitFor(() => expect(result.current.loading).toBe(false));

  await act(() => result.current.nextPage());
  expect(result.current.data.map((ticket) => ticket.id)).toEqual(["2"]);
  expect(result.current.currentPage).toBe(1);
  expect(listTickets).toHaveBeenLastCalledWith(expect.objectContaining({ pageToken: "cursor-2" }));

  await act(() => result.current.previousPage());
  expect(result.current.data.map((ticket) => ticket.id)).toEqual(["1"]);
  expect(result.current.currentPage).toBe(0);
  expect(listTickets).toHaveBeenLastCalledWith(expect.objectContaining({ pageToken: "" }));

  await act(() => result.current.nextPage());
  await act(() => result.current.resetPagination());
  expect(result.current.data.map((ticket) => ticket.id)).toEqual(["1"]);
  expect(result.current.currentPage).toBe(0);
});

it("aborts the active request on unmount", async () => {
  let signal: AbortSignal | undefined;
  listTickets.mockImplementation(({ signal: value }) => {
    signal = value;
    return new Promise(() => undefined);
  });
  const { unmount } = renderHook(() => useTicketQueue());
  await waitFor(() => expect(signal).toBeDefined());
  unmount();
  expect(signal?.aborted).toBe(true);
});
