import { MemoryRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import TicketDetailModal from "./TicketDetailModal";
const update = vi.fn();
const addComment = vi.fn();
const removeComment = vi.fn();
const ticket = {
  id: "1",
  ticketNumber: "TK-1",
  category: "miner",
  status: "in_progress",
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
  createdAt: null,
  updatedAt: null,
  alertId: null,
  warrantyStatus: "unknown",
  resolution: "unknown",
  repairLocation: "unknown",
  notes: "",
  dailyImpactUsd: 0,
  rmaVendor: null,
  rmaTracking: null,
  rmaEta: null,
  completedAt: null,
  comments: [
    {
      id: "4",
      ticketId: "1",
      userId: "2",
      userName: "alex",
      text: "Checked fan",
      createdAt: null,
      authoredByCaller: true,
    },
  ],
  partsUsed: [],
};
vi.mock("@/protoFleet/features/maintenance/hooks/useTicketDetail", () => ({
  useTicketDetail: () => ({ data: ticket, loading: false, error: null, update, addComment, removeComment }),
}));
vi.mock("@/protoFleet/features/maintenance/hooks/useMaintenanceOptions", () => ({
  useMaintenanceOptions: () => ({ assignees: [] }),
}));
vi.mock("@/protoFleet/store", () => ({ useHasPermission: () => true }));
describe("TicketDetailModal", () => {
  it("renders server-backed detail and author-aware comments", () => {
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    expect(screen.getByText("TK-1")).toBeInTheDocument();
    expect(screen.getByText("Fan: Broken")).toBeInTheDocument();
    expect(screen.getByText("Checked fan")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Delete comment" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Complete repair" })).toBeInTheDocument();
  });
  it("does not expose terminal mutation controls", () => {
    ticket.status = "completed";
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    expect(screen.queryByRole("button", { name: "Complete repair" })).not.toBeInTheDocument();
    ticket.status = "in_progress";
  });
});
