import { MemoryRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
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
  stats: { overdueCount: 0 },
  loading: false,
  error: null,
  total: 1,
  nextPageToken: "",
  setFilter: vi.fn(),
  setSort: vi.fn(),
  bulkUpdate: vi.fn(),
  refresh: vi.fn(),
  loadMore: vi.fn(),
};
vi.mock("@/protoFleet/features/maintenance/hooks/useTicketQueue", () => ({ useTicketQueue: () => queue }));
vi.mock("@/protoFleet/features/maintenance/hooks/useMaintenanceOptions", () => ({
  useMaintenanceOptions: () => ({ sites: [{ id: "1", name: "Denver" }], assignees: [], currentAssignee: null }),
}));
vi.mock("@/protoFleet/store", () => ({ useHasPermission: () => true }));
vi.mock("../TicketDetail/TicketDetailModal", () => ({ default: () => null }));
vi.mock("../CreateTicket/CreateTicketModal", () => ({ default: () => null }));

describe("TicketQueue", () => {
  it("renders live active status lanes in board view", () => {
    render(
      <MemoryRouter>
        <TicketQueue initialViewMode="kanban" />
      </MemoryRouter>,
    );
    expect(screen.getByText(/^Open \(/)).toBeInTheDocument();
    expect(screen.getByText(/^In Progress \(/)).toBeInTheDocument();
    expect(screen.getByText(/^On Hold \(/)).toBeInTheDocument();
    expect(screen.getByText("Sent to Vendor (1)")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create ticket" })).toBeInTheDocument();
  });
});
