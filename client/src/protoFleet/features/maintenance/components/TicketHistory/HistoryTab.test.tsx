import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
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
vi.mock("../TicketDetail/TicketDetailModal", () => ({
  default: ({ ticketId, ticketIds }: { ticketId: string; ticketIds: string[] }) => (
    <div data-testid="history-detail-navigation">
      {ticketId}:{ticketIds.join(",")}
    </div>
  ),
}));
beforeEach(() => {
  // Popovers stay hidden when their trigger has jsdom's default zero-size bounds.
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(0, 0, 200, 32));
});
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

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

it("places Refresh after the component and technician filters", async () => {
  render(<HistoryTab />);
  await screen.findByText("TK-2");
  expect(screen.getAllByRole("button").slice(0, 3)).toEqual([
    screen.getByRole("button", { name: "Component" }),
    screen.getByRole("button", { name: "Technician" }),
    screen.getByRole("button", { name: "Refresh" }),
  ]);
});

it("selects components from a dropdown and clears the filter without losing the technician", async () => {
  render(<HistoryTab />);
  await screen.findByText("TK-2");
  expect(screen.queryByRole("textbox", { name: "Component" })).not.toBeInTheDocument();

  fireEvent.click(screen.getByRole("button", { name: "Technician" }));
  fireEvent.click(screen.getByRole("option", { name: "former-tech" }));
  await waitFor(() =>
    expect(listCompleted).toHaveBeenLastCalledWith(expect.objectContaining({ assigneeUserIdFilter: 9n })),
  );
  fireEvent.click(screen.getByRole("button", { name: "Next page" }));
  await screen.findByText("TK-3");

  for (const component of [
    "Fan",
    "Hashboard",
    "PSU",
    "Control Board",
    "Network",
    "Electrical",
    "HVAC",
    "Cleaning",
    "Building",
  ]) {
    fireEvent.click(screen.getByRole("button", { name: "Component" }));
    fireEvent.click(screen.getByRole("option", { name: component }));
    expect(screen.queryByRole("listbox", { name: "Component options" })).not.toBeInTheDocument();
    await waitFor(() =>
      expect(listCompleted).toHaveBeenLastCalledWith(
        expect.objectContaining({ componentFilter: component, assigneeUserIdFilter: 9n, pageToken: "" }),
      ),
    );
    expect(screen.getByRole("button", { name: "Component" })).toHaveTextContent(component);
    expect(screen.getByRole("button", { name: "Previous page" })).toBeDisabled();
  }

  fireEvent.click(screen.getByRole("button", { name: "Component" }));
  expect(screen.getByRole("option", { name: "Building", selected: true })).toBeInTheDocument();
  fireEvent.click(screen.getByRole("option", { name: "All components" }));
  await waitFor(() =>
    expect(listCompleted).toHaveBeenLastCalledWith(
      expect.objectContaining({ componentFilter: undefined, assigneeUserIdFilter: 9n, pageToken: "" }),
    ),
  );
  expect(screen.getByRole("button", { name: "Component" })).toHaveTextContent("Component");
  fireEvent.click(screen.getByRole("button", { name: "Component" }));
  expect(screen.getByRole("option", { name: "All components", selected: true })).toBeInTheDocument();
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

it("keeps history stable until refresh and retains the modal navigation snapshot", async () => {
  vi.useFakeTimers();
  let refreshed = false;
  listCompleted.mockImplementation(async ({ onSuccess, onFinally }) => {
    const id = refreshed ? 4n : 2n;
    onSuccess({
      tickets: [
        {
          ticket: {
            id,
            ticketNumber: `TK-${id}`,
            component: "Fan",
            diagnosis: "Fixed",
            resolution: 1,
          },
        },
      ],
      totalCount: 1,
      nextPageToken: "",
      assigneeFacets: [],
    });
    onFinally();
  });
  render(<HistoryTab />);
  await act(async () => vi.advanceTimersByTimeAsync(0));
  fireEvent.click(screen.getByText("TK-2"));
  expect(screen.getByTestId("history-detail-navigation")).toHaveTextContent("2:2");

  refreshed = true;
  await act(async () => vi.advanceTimersByTimeAsync(30_000));
  expect(screen.getByText("TK-2")).toBeInTheDocument();
  await act(async () => fireEvent.click(screen.getByRole("button", { name: "Refresh" })));

  expect(screen.getByText("TK-4")).toBeInTheDocument();
  expect(screen.getByTestId("history-detail-navigation")).toHaveTextContent("2:2");
});

it("starts at page one without probing the old history cursor on refresh", async () => {
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
  await act(async () => fireEvent.click(screen.getByRole("button", { name: "Refresh" })));

  expect(listCompleted).toHaveBeenCalledOnce();
  expect(listCompleted).toHaveBeenCalledWith(expect.objectContaining({ pageToken: "" }));
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

  fireEvent.click(screen.getByRole("button", { name: "Component" }));
  fireEvent.click(screen.getByRole("option", { name: "Fan" }));
  await waitFor(() => expect(requests).toHaveLength(2));
  act(() => requests[0].onFinally());

  const status = screen.getByRole("status", { name: "Loading history" });
  expect(status.querySelector(".animate-spin")).toBeInTheDocument();
  expect(status).not.toHaveTextContent("Loading history");
});
