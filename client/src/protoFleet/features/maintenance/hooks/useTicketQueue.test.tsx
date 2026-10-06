import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import {
  RepairTicketSchema,
  RepairTicketSummarySchema,
  TicketStatus,
} from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";

const version = { $typeName: "google.protobuf.Timestamp" as const, seconds: 1700000000n, nanos: 123456000 };
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
      tickets: [
        create(RepairTicketSummarySchema, {
          ticket: create(RepairTicketSchema, { updatedAt: version, id: 1n, status: TicketStatus.OPEN }),
        }),
      ],
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

it("loads once and refreshes only on request", async () => {
  vi.useFakeTimers();
  const { result, unmount } = renderHook(() => useTicketQueue());
  await act(async () => vi.advanceTimersByTimeAsync(0));
  expect(result.current.loading).toBe(false);
  expect(listTickets).toHaveBeenCalledTimes(1);

  await act(async () => vi.advanceTimersByTimeAsync(30_000));
  expect(listTickets).toHaveBeenCalledTimes(1);
  await act(() => result.current.refresh());
  expect(listTickets).toHaveBeenCalledTimes(2);

  unmount();
  await act(async () => vi.advanceTimersByTimeAsync(15_000));
  expect(listTickets).toHaveBeenCalledTimes(2);
});

it("returns to the previous list page when refresh finds an empty later page", async () => {
  vi.useFakeTimers();
  let externallyClosed = false;
  listTickets.mockImplementation(async ({ pageToken, onSuccess }) => {
    const ticketId = pageToken === "cursor-2" && externallyClosed ? undefined : pageToken ? 2n : 1n;
    onSuccess({
      tickets:
        ticketId === undefined
          ? []
          : [
              create(RepairTicketSummarySchema, {
                ticket: create(RepairTicketSchema, { updatedAt: version, id: ticketId }),
              }),
            ],
      nextPageToken: pageToken || externallyClosed ? "" : "cursor-2",
      totalCount: externallyClosed ? 1 : 2,
    });
  });
  const { result } = renderHook(() => useTicketQueue());
  await act(async () => vi.advanceTimersByTimeAsync(0));
  await act(() => result.current.nextPage());
  expect(result.current.currentPage).toBe(1);

  externallyClosed = true;
  await act(() => result.current.refresh());

  expect(result.current.currentPage).toBe(0);
  expect(result.current.data.map((ticket) => ticket.id)).toEqual(["1"]);
});

it("starts a new board page sequence on explicit refresh", async () => {
  vi.useFakeTimers();
  listTickets.mockImplementation(async ({ pageToken, onSuccess }) =>
    onSuccess({
      tickets: [
        create(RepairTicketSummarySchema, {
          ticket: create(RepairTicketSchema, { updatedAt: version, id: pageToken === "cursor-2" ? 2n : 1n }),
        }),
      ],
      nextPageToken: pageToken ? "" : "cursor-2",
      totalCount: 2,
    }),
  );
  const { result } = renderHook(() => useTicketQueue());
  await act(async () => vi.advanceTimersByTimeAsync(0));
  await act(() => result.current.loadMore());
  expect(result.current.data.map((ticket) => ticket.id)).toEqual(["1", "2"]);

  await act(() => result.current.refresh());

  expect(result.current.data.map((ticket) => ticket.id)).toEqual(["1"]);
  expect(listTickets).toHaveBeenCalledTimes(3);
  expect(listTickets).toHaveBeenLastCalledWith(expect.objectContaining({ pageToken: "" }));
});

it("ignores refresh callbacks captured before a view change", async () => {
  const { result } = renderHook(() => useTicketQueue());
  await waitFor(() => expect(result.current.loading).toBe(false));
  const staleRefresh = result.current.refresh;

  act(() => result.current.setFilter({ statuses: [TicketStatus.ON_HOLD] }));
  await waitFor(() => expect(listTickets).toHaveBeenCalledTimes(2));
  await act(() => staleRefresh());

  expect(listTickets).toHaveBeenCalledTimes(2);
  expect(listTickets).toHaveBeenLastCalledWith(
    expect.objectContaining({ filter: { statuses: [TicketStatus.ON_HOLD] } }),
  );
});

it("loads tickets and refreshes after a successful mutation", async () => {
  bulkUpdate.mockImplementation(async ({ onSuccess }) => onSuccess(1));
  const { result } = renderHook(() => useTicketQueue());
  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(result.current.data[0].id).toBe("1");
  await act(() => result.current.bulkUpdate(["1"], { case: "setStatus", value: TicketStatus.IN_PROGRESS }));
  expect(bulkUpdate).toHaveBeenCalledWith(
    expect.objectContaining({
      ticketIds: [1n],
      mutation: { case: "setStatus", value: TicketStatus.IN_PROGRESS },
      expectedVersions: [{ ticketId: 1n, updatedAt: version }],
    }),
  );
  expect(listTickets).toHaveBeenCalledTimes(2);
});

it("does not refresh an obsolete queue view after a mutation completes", async () => {
  let finishMutation!: () => void;
  bulkUpdate.mockImplementation(
    ({ onSuccess }) =>
      new Promise<void>((resolve) => {
        finishMutation = () => {
          onSuccess(1);
          resolve();
        };
      }),
  );
  const { result } = renderHook(() => useTicketQueue());
  await waitFor(() => expect(result.current.loading).toBe(false));

  let mutation!: Promise<boolean>;
  act(() => {
    mutation = result.current.bulkUpdate(["1"], { case: "setStatus", value: TicketStatus.IN_PROGRESS });
  });
  await waitFor(() => expect(bulkUpdate).toHaveBeenCalledOnce());
  act(() => result.current.setFilter({ statuses: [TicketStatus.ON_HOLD] }));
  await waitFor(() => expect(listTickets).toHaveBeenCalledTimes(2));

  await act(async () => {
    finishMutation();
    await mutation;
  });

  expect(listTickets).toHaveBeenCalledTimes(2);
  expect(listTickets).toHaveBeenLastCalledWith(
    expect.objectContaining({ filter: { statuses: [TicketStatus.ON_HOLD] } }),
  );
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
          ticket: create(RepairTicketSchema, { updatedAt: version, id: pageToken === "cursor-2" ? 2n : 1n }),
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

it("returns to the previous page after bulk-closing the final visible rows", async () => {
  let laterPageLoads = 0;
  listTickets.mockImplementation(async ({ pageToken, onSuccess }) => {
    if (pageToken === "cursor-2") laterPageLoads += 1;
    const ticketId = pageToken === "cursor-2" && laterPageLoads === 1 ? 2n : pageToken ? undefined : 1n;
    onSuccess({
      tickets:
        ticketId === undefined
          ? []
          : [
              create(RepairTicketSummarySchema, {
                ticket: create(RepairTicketSchema, { updatedAt: version, id: ticketId }),
              }),
            ],
      nextPageToken: pageToken ? "" : "cursor-2",
      totalCount: ticketId === undefined ? 1 : 2,
    });
  });
  bulkUpdate.mockImplementation(async ({ onSuccess }) => onSuccess(1));
  const { result } = renderHook(() => useTicketQueue());
  await waitFor(() => expect(result.current.loading).toBe(false));
  await act(() => result.current.nextPage());
  expect(result.current.currentPage).toBe(1);

  await act(() =>
    result.current.bulkUpdate(["2"], {
      case: "bulkClose",
      value: {
        resolution: 3,
        repairLocation: 0,
        notes: "",
      },
    }),
  );

  expect(result.current.currentPage).toBe(0);
  expect(result.current.data.map((ticket) => ticket.id)).toEqual(["1"]);
  expect(listTickets).toHaveBeenLastCalledWith(expect.objectContaining({ pageToken: "" }));
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
