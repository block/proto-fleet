import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import {
  RepairLocation,
  TicketCategory,
  TicketResolution,
  TicketStatus,
} from "./generated/maintenance/v1/maintenance_pb";

const ticketVersion: Timestamp = { $typeName: "google.protobuf.Timestamp", seconds: 1700000000n, nanos: 123456000 };

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
  it("forwards the caller-generated ticket idempotency key", async () => {
    clients.createRepairTicket.mockResolvedValue({});
    const { result } = renderHook(() => useMaintenanceApi());

    await act(() =>
      result.current.createTicket({
        category: TicketCategory.MINER,
        component: "Fan",
        idempotencyKey: "create-ticket-1",
      }),
    );

    expect(clients.createRepairTicket).toHaveBeenCalledWith(
      expect.objectContaining({ idempotencyKey: "create-ticket-1" }),
      expect.anything(),
    );
  });

  it("forwards the caller-generated comment idempotency key", async () => {
    clients.createTicketComment.mockResolvedValue({});
    const { result } = renderHook(() => useMaintenanceApi());

    await act(() =>
      result.current.createComment({ ticketId: 9n, text: "Replaced fan", idempotencyKey: "comment-operation-1" }),
    );

    expect(clients.createTicketComment).toHaveBeenCalledWith(
      { ticketId: 9n, text: "Replaced fan", idempotencyKey: "comment-operation-1" },
      expect.anything(),
    );
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

  it("reports a missing ticket separately", async () => {
    clients.getRepairTicket.mockRejectedValue(new ConnectError("ticket not found", Code.NotFound));
    const onNotFound = vi.fn();
    const { result } = renderHook(() => useMaintenanceApi());

    await act(() => result.current.getTicket({ id: 4n, onNotFound }));

    expect(onNotFound).toHaveBeenCalledOnce();
  });

  it("preserves explicit empty and clear signals", async () => {
    clients.updateRepairTicket.mockResolvedValue({});
    const { result } = renderHook(() => useMaintenanceApi());
    await act(() =>
      result.current.updateTicket({
        id: 4n,
        partsSelection: [],
        clearRmaEta: true,
        expectedUpdatedAt: ticketVersion,
      }),
    );
    expect(clients.updateRepairTicket).toHaveBeenCalledWith(
      expect.objectContaining({
        id: 4n,
        partsSelection: { parts: [] },
        clearRmaEta: true,
        expectedUpdatedAt: ticketVersion,
      }),
      expect.anything(),
    );
  });
  it("forwards expected versions for a bulk status transition", async () => {
    clients.bulkUpdateRepairTickets.mockResolvedValue({ updatedCount: 1 });
    const { result } = renderHook(() => useMaintenanceApi());
    const expectedVersions = [{ ticketId: 4n, updatedAt: ticketVersion }];

    await act(() =>
      result.current.bulkUpdate({
        ticketIds: [4n],
        mutation: { case: "setStatus", value: TicketStatus.IN_PROGRESS },
        expectedVersions,
      }),
    );

    expect(clients.bulkUpdateRepairTickets).toHaveBeenCalledWith(
      {
        ticketIds: [4n],
        mutation: { case: "setStatus", value: TicketStatus.IN_PROGRESS },
        expectedVersions,
        clearAssignee: false,
      },
      expect.anything(),
    );
  });
  it("forwards bulk-close expected versions", async () => {
    clients.bulkUpdateRepairTickets.mockResolvedValue({ updatedCount: 1 });
    const { result } = renderHook(() => useMaintenanceApi());
    await act(() =>
      result.current.bulkUpdate({
        ticketIds: [4n],
        expectedVersions: [{ ticketId: 4n, updatedAt: ticketVersion }],
        mutation: {
          case: "bulkClose",
          value: {
            resolution: TicketResolution.DEFERRED,
            repairLocation: RepairLocation.UNSPECIFIED,
          },
        },
      }),
    );

    expect(clients.bulkUpdateRepairTickets).toHaveBeenCalledWith(
      {
        ticketIds: [4n],
        expectedVersions: [{ ticketId: 4n, updatedAt: ticketVersion }],
        mutation: {
          case: "bulkClose",
          value: {
            resolution: TicketResolution.DEFERRED,
            repairLocation: RepairLocation.UNSPECIFIED,
            notes: "",
          },
        },
        clearAssignee: false,
      },
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
