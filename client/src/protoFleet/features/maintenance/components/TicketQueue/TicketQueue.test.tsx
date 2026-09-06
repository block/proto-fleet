import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import TicketQueue from "./TicketQueue";
import { TicketResolution } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";

const queue = {
  data: [
    {
      id: "1",
      ticketNumber: "TK-1",
      category: "miner",
      status: "sent_to_vendor",
      urgent: false,
      component: "Fan",
      diagnosis: "Broken",
      minerIdentifier: "M1",
      assigneeUserId: null,
      assigneeName: null,
      siteId: "1",
      siteName: "Denver",
      buildingId: null,
      buildingName: null,
      rackId: null,
      rackLabel: "",
      zone: "",
      groupLabel: "",
      commentCount: 0,
      partsCount: 0,
      createdAt: null,
      updatedAt: null,
      updatedAtSnapshot: {
        $typeName: "google.protobuf.Timestamp",
        seconds: 1788688800n,
        nanos: 123456000,
      },
    },
  ],
  stats: {
    openCount: 4,
    inProgressCount: 3,
    onHoldCount: 2,
    sentToVendorCount: 1,
    overdueCount: 0,
    urgentCount: 0,
  },
  loading: false,
  error: null,
  total: 1,
  nextPageToken: "",
  currentPage: 0,
  hasPreviousPage: false,
  setFilter: vi.fn(),
  setSort: vi.fn(),
  bulkUpdate: vi.fn(),
  setUrgent: vi.fn(),
  refresh: vi.fn(),
  loadMore: vi.fn(),
  nextPage: vi.fn(),
  previousPage: vi.fn(),
  resetPagination: vi.fn(),
};
const queueRows = [...queue.data];
vi.mock("@/protoFleet/features/maintenance/hooks/useTicketQueue", () => ({ useTicketQueue: () => queue }));
let maintenanceOptions: {
  sites: { id: string; name: string }[];
  manageableSites: { id: string; name: string }[];
  assignees: { id: string; username: string; roleName: string }[];
  currentAssignee: { id: string; username: string; roleName: string } | null;
  loading: boolean;
} = {
  sites: [{ id: "1", name: "Denver" }],
  manageableSites: [{ id: "1", name: "Denver" }],
  assignees: [],
  currentAssignee: null,
  loading: true,
};
const refreshOptions = vi.fn();
vi.mock("@/protoFleet/features/maintenance/hooks/useMaintenanceOptions", () => ({
  useMaintenanceOptions: () => ({ ...maintenanceOptions, refresh: refreshOptions }),
}));
vi.mock("@/protoFleet/store", () => ({ useHasPermission: () => true }));
vi.mock("../TicketDetail/TicketDetailModal", () => ({
  default: ({ onMutationSuccess, ticketIds }: { onMutationSuccess: () => void; ticketIds: string[] }) => (
    <div>
      <span data-testid="detail-navigation-ids">{ticketIds.join(",")}</span>
      <button type="button" onClick={onMutationSuccess}>
        Simulate detail mutation
      </button>
    </div>
  ),
}));
vi.mock("../CreateTicket/CreateTicketModal", () => ({ default: () => null }));

describe("TicketQueue", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    queue.data = [...queueRows];
    queue.loading = false;
    queue.data[0].urgent = false;
    queue.data[0].status = "sent_to_vendor";
    maintenanceOptions = {
      sites: [{ id: "1", name: "Denver" }],
      manageableSites: [{ id: "1", name: "Denver" }],
      assignees: [],
      currentAssignee: null,
      loading: true,
    };
  });

  it("uses an accessible spinner for the initial ticket load", () => {
    queue.data = [];
    queue.loading = true;

    render(
      <MemoryRouter>
        <TicketQueue />
      </MemoryRouter>,
    );

    const status = screen.getByRole("status", { name: "Loading tickets" });
    expect(status.querySelector(".animate-spin")).toBeInTheDocument();
    expect(status).not.toHaveTextContent("Loading tickets");
  });

  it("renders On Hold with a theme-aware grey indicator", () => {
    queue.data[0].status = "on_hold";
    render(
      <MemoryRouter>
        <TicketQueue />
      </MemoryRouter>,
    );

    const statusCell = screen.getByTestId("status");
    expect(statusCell).toHaveTextContent("On Hold");
    expect(statusCell.querySelector('[data-testid="concentric-circles-icon"]')).toHaveClass("text-core-primary-50");
  });

  it("centers the indicator on the status line with the assignee underneath", () => {
    render(
      <MemoryRouter>
        <TicketQueue />
      </MemoryRouter>,
    );

    const statusLabel = screen.getByText("Sent to Vendor", { selector: "span" });
    const statusLayout = statusLabel.parentElement;
    expect(statusLayout).toHaveClass("grid", "items-center");
    expect(statusLayout?.querySelector('[data-testid="concentric-circles-icon"]')?.parentElement).toBe(statusLayout);
    expect(screen.getByText("Unassigned")).toHaveClass("col-start-2");
  });

  it("renders live active status lanes in board view", () => {
    render(
      <MemoryRouter>
        <TicketQueue initialViewMode="kanban" />
      </MemoryRouter>,
    );
    expect(screen.getByText(/^Open \(/)).toBeInTheDocument();
    expect(screen.getByText(/^In Progress \(/)).toBeInTheDocument();
    expect(screen.getByText(/^On Hold \(/)).toBeInTheDocument();
    expect(screen.getByText("Open (4)")).toBeInTheDocument();
    expect(screen.getByText("In Progress (3)")).toBeInTheDocument();
    expect(screen.getByText("On Hold (2)")).toBeInTheDocument();
    expect(screen.getByText("Sent to Vendor (1)")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create ticket" })).toBeInTheDocument();
  });

  it("hides row mutations and selection for tickets outside the manageable site scope", () => {
    maintenanceOptions.manageableSites = [{ id: "2", name: "Austin" }];
    render(
      <MemoryRouter>
        <TicketQueue />
      </MemoryRouter>,
    );

    expect(screen.queryByTestId("list-actions-trigger")).not.toBeInTheDocument();
    const checkboxes = screen.getAllByRole("checkbox");
    expect(checkboxes[checkboxes.length - 1]).toBeDisabled();
  });

  it("enables My tickets only after the current assignee resolves", () => {
    const view = render(
      <MemoryRouter>
        <TicketQueue />
      </MemoryRouter>,
    );
    const myTickets = screen.getByRole("button", { name: "My tickets" });
    expect(myTickets).toBeDisabled();
    fireEvent.click(myTickets);
    expect(queue.setFilter).not.toHaveBeenCalled();

    maintenanceOptions = {
      sites: [{ id: "1", name: "Denver" }],
      manageableSites: [{ id: "1", name: "Denver" }],
      assignees: [{ id: "42", username: "operator", roleName: "Technician" }],
      currentAssignee: { id: "42", username: "operator", roleName: "Technician" },
      loading: false,
    };
    view.rerender(
      <MemoryRouter>
        <TicketQueue />
      </MemoryRouter>,
    );

    expect(myTickets).toBeEnabled();
    fireEvent.click(myTickets);
    expect(queue.setFilter).toHaveBeenLastCalledWith(expect.objectContaining({ assigneeUserId: 42n }));
  });

  it("uses a single-ticket update to remove urgent status", () => {
    queue.data[0].urgent = true;
    render(
      <MemoryRouter>
        <TicketQueue />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByTestId("list-actions-trigger"));
    fireEvent.click(screen.getByText("Remove urgent"));
    expect(queue.setUrgent).toHaveBeenCalledWith("1", false);
    expect(queue.bulkUpdate).not.toHaveBeenCalledWith(["1"], { case: "markUrgent", value: false });
    queue.data[0].urgent = false;
  });

  it("keeps the opening version when a refresh updates a ticket", async () => {
    const openingVersion = queue.data[0].updatedAtSnapshot;
    queue.bulkUpdate.mockResolvedValue(true);
    const view = render(
      <MemoryRouter>
        <TicketQueue />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByTestId("list-actions-trigger"));
    fireEvent.click(screen.getByText("Close ticket"));
    queue.data[0].status = "in_progress";
    queue.data[0].updatedAtSnapshot = { ...openingVersion, nanos: 999999000 };
    view.rerender(
      <MemoryRouter>
        <TicketQueue />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("radio", { name: /Deferred/ }));
    fireEvent.click(screen.getByRole("button", { name: "Close tickets" }));

    await waitFor(() =>
      expect(queue.bulkUpdate).toHaveBeenCalledWith(["1"], expect.objectContaining({ case: "bulkClose" }), false, [
        { ticketId: 1n, updatedAt: openingVersion },
      ]),
    );
  });

  it("keeps the miner repair-location requirement when a refresh replaces the selected row", () => {
    const view = render(
      <MemoryRouter>
        <TicketQueue />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByTestId("list-actions-trigger"));
    fireEvent.click(screen.getByText("Close ticket"));
    queue.data = [{ ...queue.data[0], id: "2", ticketNumber: "TK-2", category: "infrastructure" }];
    view.rerender(
      <MemoryRouter>
        <TicketQueue />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("radio", { name: /Repaired/ }));

    expect(screen.getByLabelText("Repair location")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Close tickets" })).toBeDisabled();
  });

  it("offers no-action closure instead of ticket deletion", async () => {
    queue.bulkUpdate.mockResolvedValue(true);
    render(
      <MemoryRouter>
        <TicketQueue />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByTestId("list-actions-trigger"));
    expect(screen.queryByText("Delete ticket")).not.toBeInTheDocument();
    fireEvent.click(screen.getByText("Close ticket"));
    fireEvent.click(screen.getByRole("radio", { name: /No action needed/ }));
    fireEvent.click(screen.getByRole("button", { name: "Close tickets" }));
    await waitFor(() =>
      expect(queue.bulkUpdate).toHaveBeenCalledWith(
        ["1"],
        expect.objectContaining({
          case: "bulkClose",
          value: expect.objectContaining({ resolution: TicketResolution.NO_ACTION_NEEDED }),
        }),
        false,
        [{ ticketId: 1n, updatedAt: queue.data[0].updatedAtSnapshot }],
      ),
    );
  });

  it("keeps select-all scoped to the visible ticket page", () => {
    queue.total = 51;
    const { getByTestId } = render(
      <MemoryRouter>
        <TicketQueue />
      </MemoryRouter>,
    );

    const selectAll = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
    fireEvent.click(selectAll);

    expect(screen.queryByText(/^All .* selected$/)).not.toBeInTheDocument();
    expect(screen.getByText(/^1 .* selected$/)).toBeInTheDocument();
    queue.total = 1;
  });

  it("paginates list mode and keeps progressive loading in board mode", () => {
    queue.total = 51;
    queue.nextPageToken = "cursor-2";
    render(
      <MemoryRouter>
        <TicketQueue />
      </MemoryRouter>,
    );
    expect(screen.getByRole("button", { name: "Next page" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument();

    fireEvent.mouseDown(screen.getByRole("button", { name: "Board" }));
    expect(screen.getByRole("button", { name: "Load more" })).toBeInTheDocument();
    queue.total = 1;
    queue.nextPageToken = "";
  });

  it("captures ticket navigation when opening detail from a row action", () => {
    const second = { ...queue.data[0], id: "2", ticketNumber: "TK-2" };
    queue.data = [queue.data[0], second];
    render(
      <MemoryRouter>
        <TicketQueue />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getAllByTestId("list-actions-trigger")[0]);
    fireEvent.click(screen.getByText("Assign or update"));

    expect(screen.getByTestId("detail-navigation-ids")).toHaveTextContent("1,2");
  });

  it("keeps the modal navigation snapshot when refresh removes the open ticket", () => {
    const second = { ...queueRows[0], id: "2", ticketNumber: "TK-2" };
    queue.data = [queueRows[0], second];
    const view = render(
      <MemoryRouter>
        <TicketQueue />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByText("TK-1"));
    expect(screen.getByTestId("detail-navigation-ids")).toHaveTextContent("1,2");

    queue.data = [second];
    view.rerender(
      <MemoryRouter>
        <TicketQueue />
      </MemoryRouter>,
    );

    expect(screen.getByTestId("detail-navigation-ids")).toHaveTextContent("1,2");
  });

  it("offers explicit refresh for the queue", () => {
    render(
      <MemoryRouter>
        <TicketQueue />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    expect(queue.refresh).toHaveBeenCalledOnce();
    expect(refreshOptions).toHaveBeenCalledOnce();
  });

  it("refreshes the queue after a detail mutation", () => {
    render(
      <MemoryRouter>
        <TicketQueue />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByText("Fan: Broken"));
    fireEvent.click(screen.getByRole("button", { name: "Simulate detail mutation" }));
    expect(queue.refresh).toHaveBeenCalled();
  });
});
