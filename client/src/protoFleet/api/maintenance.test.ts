import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { TicketStatus } from "./generated/maintenance/v1/maintenance_pb";

const clients = {
  listRepairTickets: vi.fn(),
  updateRepairTicket: vi.fn(),
  createRepairTicket: vi.fn(),
  getRepairTicket: vi.fn(),
  bulkUpdateRepairTickets: vi.fn(),
  getTicketStats: vi.fn(),
  listAssignees: vi.fn(),
  listCompletedTickets: vi.fn(),
  createTicketComment: vi.fn(),
  deleteTicketComment: vi.fn(),
};
vi.mock("./clients", () => ({ maintenanceClient: clients }));
const handleAuthErrors = vi.fn();
vi.mock("@/protoFleet/store", () => ({ useAuthErrors: () => ({ handleAuthErrors }) }));
const { useMaintenanceApi } = await import("./maintenance");

describe("useMaintenanceApi", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    handleAuthErrors.mockImplementation(({ onError, error }) => onError?.(error));
  });
  it("maps filters, cursor, sort, and abort signal", async () => {
    clients.listRepairTickets.mockResolvedValue({ tickets: [], nextPageToken: "next", totalCount: 7 });
    const signal = new AbortController().signal;
    const onSuccess = vi.fn();
    const { result } = renderHook(() => useMaintenanceApi());
    await act(() =>
      result.current.listTickets({
        filter: { statuses: [TicketStatus.OPEN], siteIds: [9n] },
        pageSize: 25,
        pageToken: "cursor",
        signal,
        onSuccess,
      }),
    );
    expect(clients.listRepairTickets).toHaveBeenCalledWith(
      expect.objectContaining({
        filter: expect.objectContaining({ statuses: [TicketStatus.OPEN], siteIds: [9n] }),
        pageSize: 25,
        pageToken: "cursor",
      }),
      { signal },
    );
    expect(onSuccess).toHaveBeenCalledWith({ tickets: [], nextPageToken: "next", totalCount: 7 });
  });
  it("forwards completed-ticket assignee facets", async () => {
    const facets = [{ userId: 9n, username: "former-tech", roleName: "" }];
    clients.listCompletedTickets.mockResolvedValue({
      tickets: [],
      nextPageToken: "",
      totalCount: 0,
      assigneeFacets: facets,
    });
    const onSuccess = vi.fn();
    const { result } = renderHook(() => useMaintenanceApi());

    await act(() => result.current.listCompleted({ onSuccess }));

    expect(onSuccess).toHaveBeenCalledWith({
      tickets: [],
      nextPageToken: "",
      totalCount: 0,
      assigneeFacets: facets,
    });
  });

  it("preserves explicit empty and clear signals", async () => {
    clients.updateRepairTicket.mockResolvedValue({});
    const { result } = renderHook(() => useMaintenanceApi());
    await act(() => result.current.updateTicket({ id: 4n, partsSelection: [], clearRmaEta: true }));
    expect(clients.updateRepairTicket).toHaveBeenCalledWith(
      expect.objectContaining({ id: 4n, partsSelection: { parts: [] }, clearRmaEta: true }),
      expect.anything(),
    );
  });
  it("routes failures through auth handling and finalizes once", async () => {
    const error = new Error("failed");
    clients.getRepairTicket.mockRejectedValue(error);
    const onError = vi.fn();
    const onFinally = vi.fn();
    const { result } = renderHook(() => useMaintenanceApi());
    await act(() => result.current.getTicket({ id: 1n, onError, onFinally }));
    expect(handleAuthErrors).toHaveBeenCalledWith(expect.objectContaining({ error }));
    expect(onError).toHaveBeenCalledWith("failed");
    expect(onFinally).toHaveBeenCalledOnce();
  });
  it("does not call a client for a pre-aborted request but still finalizes", async () => {
    const controller = new AbortController();
    controller.abort();
    const onFinally = vi.fn();
    const { result } = renderHook(() => useMaintenanceApi());
    await act(() => result.current.getTicket({ id: 1n, signal: controller.signal, onFinally }));
    expect(clients.getRepairTicket).not.toHaveBeenCalled();
    expect(onFinally).toHaveBeenCalledOnce();
  });
});
