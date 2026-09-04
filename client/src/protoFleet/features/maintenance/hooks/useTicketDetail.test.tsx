import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";

const getTicket = vi.fn();
const updateTicket = vi.fn();
const createComment = vi.fn();
const deleteComment = vi.fn();
vi.mock("@/protoFleet/api/maintenance", () => ({
  useMaintenanceApi: () => ({ getTicket, updateTicket, createComment, deleteComment }),
}));
vi.mock("../mappers", () => ({ toTicketDetail: () => ({ id: "9" }) }));
const { useTicketDetail } = await import("./useTicketDetail");

beforeEach(() => {
  vi.clearAllMocks();
});
it("loads the selected ticket ID", async () => {
  getTicket.mockImplementation(async ({ onSuccess }) => onSuccess({ ticket: { id: 9n } }));
  const { result } = renderHook(() => useTicketDetail("9"));
  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(getTicket).toHaveBeenCalledWith(expect.objectContaining({ id: 9n }));
  expect(result.current.data).toEqual({ id: "9" });
});
it("clears state without an RPC when no ticket is selected", async () => {
  const { result } = renderHook(() => useTicketDetail(null));
  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(getTicket).not.toHaveBeenCalled();
});
