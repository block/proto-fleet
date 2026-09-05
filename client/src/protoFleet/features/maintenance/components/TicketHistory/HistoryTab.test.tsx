import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import HistoryTab from "./HistoryTab";
import { SortDirection, TicketSortField } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
const listCompleted = vi.fn(async ({ pageToken, onSuccess, onFinally }) => {
  const secondPage = pageToken === "cursor-2";
  onSuccess({
    tickets: [
      {
        ticket: {
          id: secondPage ? 3n : 2n,
          ticketNumber: secondPage ? "TK-3" : "TK-2",
          component: "Fan",
          diagnosis: "Fixed",
          minerIdentifier: "M2",
          resolution: 1,
          assigneeName: "alex",
          siteName: "Denver",
          buildingName: "B1",
        },
      },
    ],
    totalCount: 2,
    nextPageToken: secondPage ? "" : "cursor-2",
    assigneeFacets: [{ userId: 9n, username: "former-tech", roleName: "" }],
  });
  onFinally();
});
vi.mock("@/protoFleet/api/maintenance", () => ({ useMaintenanceApi: () => ({ listCompleted }) }));
vi.mock("@/protoFleet/features/maintenance/hooks/useMaintenanceOptions", () => ({
  useMaintenanceOptions: () => ({ assignees: [{ id: "1", username: "alex" }] }),
}));
vi.mock("../TicketDetail/TicketDetailModal", () => ({ default: () => null }));
afterEach(() => vi.useRealTimers());

it("loads completed ticket history without an export control", async () => {
  render(<HistoryTab />);
  await waitFor(() => expect(screen.getByText("TK-2")).toBeInTheDocument());
  expect(screen.getByText("Repaired")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /Export CSV/i })).not.toBeInTheDocument();
  expect(listCompleted).toHaveBeenCalledWith(
    expect.objectContaining({
      pageSize: 50,
      sortField: TicketSortField.COMPLETED_AT,
      sortDirection: SortDirection.DESC,
    }),
  );
});

it("offers inactive technicians returned by history facets", async () => {
  render(<HistoryTab />);
  await waitFor(() => expect(screen.getByText("TK-2")).toBeInTheDocument());

  fireEvent.click(screen.getByRole("button", { name: "Technician" }));

  expect(screen.getByText("former-tech")).toBeInTheDocument();
});

it("replaces completed tickets when moving between cursor pages", async () => {
  render(<HistoryTab />);
  await waitFor(() => expect(screen.getByText("TK-2")).toBeInTheDocument());
  fireEvent.click(screen.getByRole("button", { name: "Next page" }));
  await waitFor(() => expect(screen.getByText("TK-3")).toBeInTheDocument());
  expect(screen.queryByText("TK-2")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Previous page" })).toBeEnabled();
});

it("backs up when polling empties the current history cursor page", async () => {
  vi.useFakeTimers();
  let externallyDeleted = false;
  listCompleted.mockImplementation(async ({ pageToken, onSuccess, onFinally }) => {
    const secondPage = pageToken === "cursor-2";
    onSuccess({
      tickets:
        secondPage && externallyDeleted
          ? []
          : [
              {
                ticket: {
                  id: secondPage ? 3n : 2n,
                  ticketNumber: secondPage ? "TK-3" : "TK-2",
                  component: "Fan",
                  diagnosis: "Fixed",
                  resolution: 1,
                },
              },
            ],
      totalCount: externallyDeleted ? 1 : 2,
      nextPageToken: secondPage || externallyDeleted ? "" : "cursor-2",
      assigneeFacets: [],
    });
    onFinally();
  });
  render(<HistoryTab />);
  await act(async () => vi.advanceTimersByTimeAsync(0));
  fireEvent.click(screen.getByRole("button", { name: "Next page" }));
  await act(async () => undefined);
  expect(screen.getByText("TK-3")).toBeInTheDocument();

  externallyDeleted = true;
  listCompleted.mockClear();
  await act(async () => vi.advanceTimersByTimeAsync(15_000));

  expect(listCompleted).toHaveBeenNthCalledWith(1, expect.objectContaining({ pageToken: "cursor-2" }));
  expect(listCompleted).toHaveBeenNthCalledWith(2, expect.objectContaining({ pageToken: "" }));
  expect(screen.getByText("TK-2")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Previous page" })).toBeDisabled();
});

it("ignores completion callbacks from an older filtered request", async () => {
  const requests: Array<{ onFinally: () => void }> = [];
  listCompleted.mockImplementation(({ onFinally }) => {
    requests.push({ onFinally });
    return new Promise(() => undefined);
  });
  render(<HistoryTab />);
  await waitFor(() => expect(requests).toHaveLength(1));

  fireEvent.change(screen.getByLabelText("Component"), { target: { value: "Fan" } });
  await waitFor(() => expect(requests).toHaveLength(2));
  act(() => requests[0].onFinally());

  const status = screen.getByRole("status", { name: "Loading history" });
  expect(status.querySelector(".animate-spin")).toBeInTheDocument();
  expect(status).not.toHaveTextContent("Loading history");
});
