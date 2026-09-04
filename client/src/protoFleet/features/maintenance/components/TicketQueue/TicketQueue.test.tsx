import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import TicketQueue from "./TicketQueue";

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
vi.mock("@/protoFleet/features/maintenance/hooks/useTicketQueue", () => ({ useTicketQueue: () => queue }));
vi.mock("@/protoFleet/features/maintenance/hooks/useMaintenanceOptions", () => ({
  useMaintenanceOptions: () => ({ sites: [{ id: "1", name: "Denver" }], assignees: [], currentAssignee: null }),
}));
vi.mock("@/protoFleet/store", () => ({ useHasPermission: () => true }));
vi.mock("../TicketDetail/TicketDetailModal", () => ({
  default: ({ onMutationSuccess }: { onMutationSuccess: () => void }) => (
    <button type="button" onClick={onMutationSuccess}>
      Simulate detail mutation
    </button>
  ),
}));
vi.mock("../CreateTicket/CreateTicketModal", () => ({ default: () => null }));

describe("TicketQueue", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    queue.data[0].urgent = false;
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
