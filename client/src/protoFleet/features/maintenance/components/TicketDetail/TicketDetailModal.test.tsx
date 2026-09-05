import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import TicketDetailModal from "./TicketDetailModal";
import { TicketStatus } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import type { TicketDetail } from "@/protoFleet/features/maintenance/types";
const update = vi.fn();
const addComment = vi.fn();
const removeComment = vi.fn();
const ticket: TicketDetail = {
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
const detailState: {
  data: TicketDetail | null;
  loading: boolean;
  error: string | null;
  update: typeof update;
  addComment: typeof addComment;
  removeComment: typeof removeComment;
} = { data: ticket, loading: false, error: null, update, addComment, removeComment };
vi.mock("@/protoFleet/features/maintenance/hooks/useTicketDetail", () => ({
  useTicketDetail: () => detailState,
}));
vi.mock("@/protoFleet/features/maintenance/hooks/useMaintenanceOptions", () => ({
  useMaintenanceOptions: () => ({ assignees: [] }),
}));
vi.mock("./CompletionForm", () => ({
  default: ({
    onSubmit,
  }: {
    onSubmit: (value: { partsSelection: []; resolution: number; repairLocation: number }) => Promise<boolean>;
  }) => (
    <button type="button" onClick={() => void onSubmit({ partsSelection: [], resolution: 1, repairLocation: 1 })}>
      Submit completion
    </button>
  ),
}));
vi.mock("@/protoFleet/store", () => ({ useHasPermission: () => true }));
describe("TicketDetailModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    detailState.data = ticket;
    detailState.loading = false;
    detailState.error = null;
    ticket.status = "in_progress";
    ticket.rmaVendor = null;
    ticket.rmaTracking = null;
    ticket.rmaEta = null;
    ticket.partsUsed = [];
  });

  it("uses an accessible spinner for the initial ticket-detail load", () => {
    detailState.data = null;
    detailState.loading = true;

    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );

    const status = screen.getByRole("status", { name: "Loading ticket" });
    expect(status.querySelector(".animate-spin")).toBeInTheDocument();
    expect(status).not.toHaveTextContent("Loading ticket");
  });

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
  it("uses compact accessible controls to navigate the visible ticket page", () => {
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" ticketIds={["0", "1", "2"]} onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    const previous = screen.getByRole("button", { name: "Previous ticket" });
    const next = screen.getByRole("button", { name: "Next ticket" });
    expect(previous).toBeEnabled();
    expect(next).toBeEnabled();
    expect(screen.queryByText("Previous")).not.toBeInTheDocument();
    expect(screen.queryByText("Next")).not.toBeInTheDocument();
    fireEvent.click(next);
    expect(screen.getByText("3 of 3 tickets")).toBeInTheDocument();
  });

  it("clears the comment editor and draft when navigating to another ticket", () => {
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" ticketIds={["1", "2"]} onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Add comment" }));
    fireEvent.change(screen.getByLabelText("Add a comment"), { target: { value: "Wrong ticket draft" } });
    expect(screen.getByDisplayValue("Wrong ticket draft")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Next ticket" }));

    expect(screen.queryByDisplayValue("Wrong ticket draft")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add comment" })).toBeInTheDocument();
  });

  it("clears the completion editor when navigating to another ticket", () => {
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" ticketIds={["1", "2"]} onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Complete repair" }));
    expect(screen.getByRole("button", { name: "Submit completion" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Next ticket" }));

    expect(screen.queryByRole("button", { name: "Submit completion" })).not.toBeInTheDocument();
  });

  it("clears the RMA editor and its draft when navigating to another ticket", () => {
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" ticketIds={["1", "2"]} onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Update status" }));
    fireEvent.click(screen.getByText("Sent to Vendor"));
    fireEvent.change(screen.getByLabelText("Vendor"), { target: { value: "Stale vendor" } });
    expect(screen.getByDisplayValue("Stale vendor")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Next ticket" }));

    expect(screen.queryByLabelText("Vendor")).not.toBeInTheDocument();
  });

  it("submits the loaded part reservations as the expected completion snapshot", async () => {
    ticket.partsUsed = [{ inventoryPartId: "7", partName: "Fan", quantity: 2 }];
    update.mockResolvedValueOnce(true);
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Complete repair" }));
    fireEvent.click(screen.getByRole("button", { name: "Submit completion" }));

    await waitFor(() =>
      expect(update).toHaveBeenCalledWith(
        expect.objectContaining({
          expectedPartsSelection: [{ inventoryPartId: 7n, partName: "Fan", quantity: 2 }],
        }),
      ),
    );
  });

  it("keeps the expected reservation snapshot fixed while completion is open", async () => {
    ticket.partsUsed = [{ inventoryPartId: "7", partName: "Fan", quantity: 2 }];
    update.mockResolvedValueOnce(true);
    const view = render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Complete repair" }));

    ticket.partsUsed = [{ inventoryPartId: "8", partName: "Cable", quantity: 1 }];
    view.rerender(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Submit completion" }));

    await waitFor(() =>
      expect(update).toHaveBeenCalledWith(
        expect.objectContaining({
          expectedPartsSelection: [{ inventoryPartId: 7n, partName: "Fan", quantity: 2 }],
        }),
      ),
    );
  });

  it("closes the completion editor after a successful completion", async () => {
    update.mockResolvedValueOnce(true);
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Complete repair" }));
    fireEvent.click(screen.getByRole("button", { name: "Submit completion" }));

    await waitFor(() => expect(screen.queryByRole("button", { name: "Submit completion" })).not.toBeInTheDocument());
  });

  it("does not expose terminal mutation controls", () => {
    ticket.status = "completed";
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    expect(screen.queryByRole("button", { name: "Complete repair" })).not.toBeInTheDocument();
  });

  it("hides an open mutation editor when polling reports terminal status", () => {
    const view = render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Complete repair" }));
    expect(screen.getByRole("button", { name: "Submit completion" })).toBeInTheDocument();

    ticket.status = "completed";
    view.rerender(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );

    expect(screen.queryByRole("button", { name: "Submit completion" })).not.toBeInTheDocument();
  });

  it("notifies the queue after a successful detail mutation", async () => {
    update.mockResolvedValueOnce(true);
    const onMutationSuccess = vi.fn();
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} onMutationSuccess={onMutationSuccess} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Update status" }));
    fireEvent.click(screen.getByText("Open"));
    await waitFor(() => expect(onMutationSuccess).toHaveBeenCalledTimes(1));
  });

  it("retains persisted RMA details when resending a ticket to the vendor", async () => {
    ticket.rmaVendor = "Repair Co";
    ticket.rmaTracking = "TRACK-1";
    ticket.rmaEta = new Date("2026-09-10T00:00:00Z");
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Update status" }));
    fireEvent.click(screen.getByText("Sent to Vendor"));

    expect(screen.getByLabelText("Vendor")).toHaveValue("Repair Co");
    expect(screen.getByLabelText("Tracking #")).toHaveValue("TRACK-1");
    expect(screen.getByLabelText("ETA")).toHaveValue("2026-09-10");

    update.mockResolvedValueOnce(true);
    fireEvent.change(screen.getByLabelText("ETA"), { target: { value: "" } });
    fireEvent.click(screen.getByRole("button", { name: "Send to vendor" }));

    await waitFor(() =>
      expect(update).toHaveBeenCalledWith({
        status: TicketStatus.SENT_TO_VENDOR,
        rmaVendor: "Repair Co",
        rmaTracking: "TRACK-1",
        clearRmaEta: true,
      }),
    );
  });

  it("edits persisted RMA details after vendor dispatch", async () => {
    ticket.status = "sent_to_vendor";
    ticket.rmaVendor = "Repair Co";
    ticket.rmaTracking = "TRACK-1";
    ticket.rmaEta = new Date("2026-09-10T00:00:00Z");
    const formatEta = vi.spyOn(ticket.rmaEta, "toLocaleDateString").mockReturnValue("9/10/2026");
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    expect(screen.getByText("RMA Details")).toBeInTheDocument();
    expect(screen.getByText("Vendor: Repair Co")).toBeInTheDocument();
    expect(screen.getByText("Tracking #: TRACK-1")).toBeInTheDocument();
    expect(formatEta).toHaveBeenCalledWith(undefined, { timeZone: "UTC" });

    update.mockResolvedValueOnce(true);
    fireEvent.click(screen.getByRole("button", { name: "Edit RMA details" }));
    expect(screen.getByLabelText("Vendor")).toHaveValue("Repair Co");
    expect(screen.getByLabelText("Tracking #")).toHaveValue("TRACK-1");
    expect(screen.getByLabelText("ETA")).toHaveValue("2026-09-10");
    fireEvent.change(screen.getByLabelText("Tracking #"), { target: { value: "TRACK-2" } });
    fireEvent.change(screen.getByLabelText("ETA"), { target: { value: "" } });
    fireEvent.click(screen.getByRole("button", { name: "Save RMA details" }));

    await waitFor(() =>
      expect(update).toHaveBeenCalledWith({
        rmaVendor: "Repair Co",
        rmaTracking: "TRACK-2",
        clearRmaEta: true,
      }),
    );
  });
});
