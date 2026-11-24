import type { ComponentProps } from "react";
import { MemoryRouter } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import TicketDetailModal from "./TicketDetailModal";
import { MinerIdentifierType } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  RepairLocation,
  TicketAssetSchema,
  TicketCategory,
  TicketStatus,
} from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import type { TicketDetail } from "@/protoFleet/features/maintenance/types";
let targetGate: ((mutation: () => Promise<boolean>) => Promise<boolean>) | undefined;
vi.mock("./TicketTargets", () => ({
  default: ({ submitMutation }: { submitMutation: typeof targetGate }) => {
    targetGate = submitMutation;
    return null;
  },
}));
const refresh = vi.fn();
const update = vi.fn();
const addComment = vi.fn();
const removeComment = vi.fn();
const share = vi.fn();
const setNoteVisibility = vi.fn();
let actualCompletionForm = false;
const { lookupMinerByIdentifier, openMinerView } = vi.hoisted(() => ({
  lookupMinerByIdentifier: vi.fn(),
  openMinerView: vi.fn(),
}));
vi.mock("@/protoFleet/api/lookupMinerByIdentifier", () => ({ lookupMinerByIdentifier }));
vi.mock("@/protoFleet/components/SingleMinerWrapper/useOpenMinerView", () => ({
  useOpenMinerView: () => openMinerView,
}));
const version = { $typeName: "google.protobuf.Timestamp" as const, seconds: 1700000000n, nanos: 123456000 };
const ticket: TicketDetail = {
  id: "1",
  ticketNumber: "TK-1",
  category: "miner",
  status: "in_progress",
  urgent: false,
  component: "Fan",
  diagnosis: "Broken",
  description: "",
  targets: [],
  audiences: [],
  sharedWithClients: false,
  createdByAccountId: "",
  minerIdentifier: "01a0fdc7-5872-7db7-bd0e-78c982fb09c6",
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
  updatedAtSnapshot: version,
  alertId: null,
  warrantyStatus: "unknown",
  resolution: "unknown",
  resolutionNote: "",
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
      internal: false,
      accountId: "",
      accountName: "",
    },
  ],
  partsUsed: [],
};
const detailState: {
  data: TicketDetail | null;
  loading: boolean;
  error: string | null;
  refresh: typeof refresh;
  update: typeof update;
  addComment: typeof addComment;
  removeComment: typeof removeComment;
  share: typeof share;
  setNoteVisibility: typeof setNoteVisibility;
} = { data: ticket, loading: false, error: null, refresh, update, addComment, removeComment, share, setNoteVisibility };
vi.mock("@/protoFleet/features/maintenance/hooks/useTicketDetail", () => ({
  useTicketDetail: () => detailState,
}));
const maintenanceOptions = {
  assignees: [] as { id: string; username: string }[],
  manageableSites: [{ id: "1", name: "Denver" }],
  loading: false,
  error: null as string | null,
};
const refreshOptions = vi.fn();
vi.mock("@/protoFleet/features/maintenance/hooks/useMaintenanceOptions", () => ({
  useMaintenanceOptions: () => ({ ...maintenanceOptions, refresh: refreshOptions }),
}));
vi.mock("@/protoFleet/api/inventory", () => ({
  useInventoryApi: () => ({
    listPartsBySite: async ({ onSuccess }: { onSuccess: (parts: never[]) => void }) => onSuccess([]),
  }),
}));
vi.mock("./CompletionForm", async (importOriginal) => {
  const { default: CompletionForm } = await importOriginal<typeof import("./CompletionForm")>();
  return {
    default: (props: ComponentProps<typeof CompletionForm>) =>
      actualCompletionForm ? <CompletionForm {...props} /> : <MockCompletionForm {...props} />,
  };
});
const MockCompletionForm = ({
  onSubmit,
  initialParts = [],
}: ComponentProps<typeof import("./CompletionForm").default>) => (
  <>
    <span data-testid="completion-reservations">{initialParts.map((part) => part.inventoryPartId).join(",")}</span>
    <button
      type="button"
      onClick={() =>
        void onSubmit({ partsSelection: [], resolution: 1, repairLocation: 1, resolutionNote: "Repair verified" })
      }
    >
      Submit completion
    </button>
  </>
);
vi.mock("@/protoFleet/store", () => ({ useHasPermission: () => true }));
describe("TicketDetailModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    actualCompletionForm = false;
    lookupMinerByIdentifier.mockResolvedValue({ status: "notFound" });
    detailState.data = ticket;
    detailState.loading = false;
    detailState.error = null;
    ticket.status = "in_progress";
    ticket.component = "Fan";
    ticket.notes = "";
    ticket.diagnosis = "Broken";
    ticket.targets = [];
    ticket.resolutionNote = "";
    ticket.sharedWithClients = false;
    ticket.audiences = [];
    ticket.category = "miner";
    ticket.rmaVendor = null;
    ticket.rmaTracking = null;
    ticket.rmaEta = null;
    ticket.partsUsed = [];
    ticket.updatedAtSnapshot = version;
    ticket.assigneeUserId = null;
    ticket.assigneeName = null;
    ticket.siteId = "1";
    maintenanceOptions.assignees = [];
    maintenanceOptions.manageableSites = [{ id: "1", name: "Denver" }];
    maintenanceOptions.loading = false;
    maintenanceOptions.error = null;
  });

  it.each([false, true])("labels ticket visibility when sharedWithClients is %s", (sharedWithClients) => {
    ticket.sharedWithClients = sharedWithClients;
    render(<TicketDetailModal ticketId="1" onDismiss={vi.fn()} />);
    expect(screen.getByText(sharedWithClients ? "Shared with clients" : "Internal")).toBeInTheDocument();
  });

  it("corrects an unknown component during triage without requiring a diagnosis", async () => {
    ticket.component = "Unknown";
    ticket.diagnosis = "";
    update.mockResolvedValue(true);
    render(<TicketDetailModal ticketId="1" onDismiss={vi.fn()} />);
    // The shared popover hides triggers with jsdom's default zero-size bounds.
    vi.spyOn(screen.getByRole("button", { name: "Component" }).parentElement!, "getBoundingClientRect").mockReturnValue(
      new DOMRect(20, 20, 240, 40),
    );
    fireEvent.click(screen.getByRole("button", { name: "Component" }));
    fireEvent.click(await screen.findByRole("option", { name: "Fan" }));
    await waitFor(() => expect(update).toHaveBeenCalledWith({ component: "Fan", expectedUpdatedAt: version }));
  });

  it("preserves custom component labels while offering named choices, Unknown, and Other", async () => {
    ticket.component = "Controller connector";
    render(<TicketDetailModal ticketId="1" onDismiss={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Component" })).toHaveTextContent("Controller connector");
    // The shared popover hides triggers with jsdom's default zero-size bounds.
    vi.spyOn(screen.getByRole("button", { name: "Component" }).parentElement!, "getBoundingClientRect").mockReturnValue(
      new DOMRect(20, 20, 240, 40),
    );
    fireEvent.click(screen.getByRole("button", { name: "Component" }));
    expect(await screen.findByRole("option", { name: "Controller connector" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(screen.getByRole("option", { name: "Fan" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Unknown" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Other" })).toBeInTheDocument();
  });

  it("keeps component triage read-only outside manageable sites", () => {
    maintenanceOptions.manageableSites = [];
    render(<TicketDetailModal ticketId="1" onDismiss={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Component" })).toBeDisabled();
  });

  it("forwards note-publication errors into the note confirmation dialog", async () => {
    const originalComments = ticket.comments;
    ticket.comments = originalComments.map((comment) => ({ ...comment, internal: true }));
    ticket.sharedWithClients = true;
    detailState.error = "An earlier update failed";
    setNoteVisibility.mockImplementationOnce(async (_id, _internal, _version, onError) => {
      onError("Sharing was denied");
      return false;
    });
    try {
      render(<TicketDetailModal ticketId="1" onDismiss={vi.fn()} />);
      fireEvent.click(screen.getByRole("checkbox", { name: "Internal only" }));
      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "Share note" }));
      await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("Sharing was denied"));
      expect(setNoteVisibility).toHaveBeenCalledWith("4", false, version, expect.any(Function));
    } finally {
      ticket.comments = originalComments;
    }
  });

  it("adds an optional diagnosis after ticket creation", async () => {
    ticket.diagnosis = "";
    update.mockResolvedValue(true);
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Add diagnosis" }));
    fireEvent.change(screen.getByLabelText("Diagnosis (internal, optional)"), { target: { value: "Loose connector" } });
    fireEvent.click(screen.getByRole("button", { name: "Save diagnosis" }));
    await waitFor(() =>
      expect(update).toHaveBeenCalledWith({ diagnosis: "Loose connector", expectedUpdatedAt: version }),
    );
  });
  it("retains a failed diagnosis draft and refreshes before explicitly accepting the current version", async () => {
    update.mockResolvedValueOnce(false).mockResolvedValueOnce(true);
    const nextVersion = { ...version, nanos: version.nanos + 1 };
    const view = render(<TicketDetailModal ticketId="1" onDismiss={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Edit diagnosis" }));
    fireEvent.change(screen.getByLabelText("Diagnosis (internal, optional)"), { target: { value: "Draft diagnosis" } });
    fireEvent.click(screen.getByRole("button", { name: "Save diagnosis" }));
    await waitFor(() => expect(update).toHaveBeenCalledOnce());
    detailState.error = "Ticket changed. Refresh and try again.";
    view.rerender(<TicketDetailModal ticketId="1" onDismiss={vi.fn()} />);
    expect(screen.getByLabelText("Diagnosis (internal, optional)")).toHaveValue("Draft diagnosis");
    expect(screen.getByRole("region", { name: "Internal diagnosis" })).toHaveTextContent(detailState.error);
    expect(screen.getByRole("button", { name: "Refresh" })).toBeEnabled();
    refresh.mockImplementationOnce(async () => {
      ticket.updatedAtSnapshot = nextVersion;
      ticket.diagnosis = "Another host's diagnosis";
      return ticket;
    });
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Refresh" })));
    view.rerender(<TicketDetailModal ticketId="1" onDismiss={vi.fn()} />);
    expect(screen.getByLabelText("Diagnosis (internal, optional)")).toHaveValue("Draft diagnosis");
    expect(screen.getByText("Current diagnosis: Another host's diagnosis")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Use refreshed ticket version" }));
    expect(update).toHaveBeenCalledOnce();
    fireEvent.click(screen.getByRole("button", { name: "Save diagnosis" }));
    await waitFor(() =>
      expect(update).toHaveBeenLastCalledWith({ diagnosis: "Draft diagnosis", expectedUpdatedAt: nextVersion }),
    );
  });

  it("retains resolution input after a failed completion", async () => {
    actualCompletionForm = true;
    update.mockResolvedValue(false);
    render(<TicketDetailModal ticketId="1" onDismiss={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Complete repair" }));
    fireEvent.change(screen.getByLabelText("Resolution note (visible to clients)"), {
      target: { value: "Repair draft" },
    });
    fireEvent.click(screen.getAllByRole("button", { name: "Complete repair" }).slice(-1)[0]);
    await waitFor(() => expect(update).toHaveBeenCalledOnce());
    expect(screen.getByLabelText("Resolution note (visible to clients)")).toHaveValue("Repair draft");
    expect(screen.getByText("Unable to complete repair")).toHaveAttribute("role", "alert");
  });

  it("retains RMA inputs and displays the failure next to the editor", async () => {
    update.mockResolvedValue(false);
    const view = render(<TicketDetailModal ticketId="1" onDismiss={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Update status" }));
    fireEvent.click(screen.getByText("Sent to Vendor"));
    fireEvent.change(screen.getByLabelText("Vendor"), { target: { value: "Draft vendor" } });
    fireEvent.change(screen.getByLabelText("Tracking #"), { target: { value: "DRAFT-1" } });
    fireEvent.click(screen.getByRole("button", { name: "Send to vendor" }));
    await waitFor(() => expect(update).toHaveBeenCalledOnce());
    detailState.error = "Vendor save failed";
    view.rerender(<TicketDetailModal ticketId="1" onDismiss={vi.fn()} />);
    expect(screen.getByLabelText("Vendor")).toHaveValue("Draft vendor");
    expect(screen.getByLabelText("Tracking #")).toHaveValue("DRAFT-1");
    expect(screen.getAllByRole("alert").some((node) => node.closest('[aria-label="RMA editor"]'))).toBe(true);
  });

  it("leaves new equipment diagnosis in the existing target repair editor", () => {
    ticket.diagnosis = "";
    ticket.targets = [create(TicketAssetSchema, { kind: TicketCategory.MINER, id: 1n })];
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    expect(screen.queryByRole("button", { name: "Add diagnosis" })).not.toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Internal diagnosis" })).not.toBeInTheDocument();
  });

  it("edits and clears an optional internal diagnosis using the captured ticket version", async () => {
    update.mockResolvedValue(true);
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Edit diagnosis" }));
    expect(screen.getByLabelText("Diagnosis (internal, optional)")).toHaveValue("Broken");
    fireEvent.change(screen.getByLabelText("Diagnosis (internal, optional)"), { target: { value: "" } });
    fireEvent.click(screen.getByRole("button", { name: "Save diagnosis" }));
    await waitFor(() => expect(update).toHaveBeenCalledWith({ diagnosis: "", expectedUpdatedAt: version }));
  });
  it("previews recipients before sharing a private ticket", async () => {
    ticket.audiences = [{ $typeName: "maintenance.v1.TicketAudience", accountId: "client", name: "Demo Client" }];
    share.mockResolvedValue(true);
    render(<TicketDetailModal ticketId="1" onDismiss={vi.fn()} />);
    expect(screen.getByText("Internal", { exact: true })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Share with clients" }));
    expect(share).not.toHaveBeenCalled();
    expect(screen.getByText("Visible to: Demo Client")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Share ticket" }));
    await waitFor(() => expect(share).toHaveBeenCalledWith(version));
  });

  it.each([
    ["module", "Repaired"],
    ["module", "Replaced"],
    ["hashboard", "Repaired"],
    ["hashboard", "Replaced"],
  ] as const)("completes a %s as %s without a miner-only repair location", async (category, resolution) => {
    actualCompletionForm = true;
    ticket.category = category;
    ticket.partsUsed = [{ inventoryPartId: "7", partName: "Fan", quantity: 2, consumedAt: null }];
    update.mockResolvedValue(true);
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Complete repair" }));
    if (resolution === "Replaced") {
      fireEvent.click(screen.getByRole("button", { name: "Mark as" }));
      fireEvent.click(screen.getByText("Replaced", { exact: true }));
    }
    fireEvent.change(screen.getByLabelText("Resolution note (visible to clients)"), {
      target: { value: "Hardware repaired and verified" },
    });
    const buttons = screen.getAllByRole("button", { name: "Complete repair" });
    fireEvent.click(buttons[buttons.length - 1]);
    await waitFor(() =>
      expect(update).toHaveBeenCalledWith(
        expect.objectContaining({
          status: TicketStatus.COMPLETED,
          repairLocation: RepairLocation.UNSPECIFIED,
          resolutionNote: "Hardware repaired and verified",
          partsSelection: [{ inventoryPartId: 7n, partName: "Fan", quantity: 2 }],
        }),
      ),
    );
  });

  it("keeps target and conversation keys distinct across ticket refreshes", () => {
    ticket.targets = [create(TicketAssetSchema, { kind: TicketCategory.MODULE, id: 51n, label: "Module 51" })];
    const errors = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      const view = render(<TicketDetailModal ticketId="1" onDismiss={vi.fn()} />);
      view.rerender(<TicketDetailModal ticketId="1" onDismiss={vi.fn()} />);
      expect(errors.mock.calls.filter(([message]) => String(message).includes("same key"))).toHaveLength(0);
    } finally {
      errors.mockRestore();
    }
  });
  it("renders the closing resolution note separately from operational notes", () => {
    ticket.status = "completed";
    ticket.notes = "Private diagnostic details";
    ticket.resolutionNote = "Equipment repaired and verified";
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );

    const resolution = screen.getByRole("table", { name: "Ticket resolution" });
    expect(resolution).toHaveTextContent("Equipment repaired and verified");
    expect(resolution).not.toHaveTextContent("Private diagnostic details");
    expect(screen.getByRole("region", { name: "Operational notes" })).toHaveTextContent("Private diagnostic details");
  });

  it("shows legacy completion notes only as host operational notes", () => {
    ticket.status = "completed";
    ticket.notes = "Legacy private repair evidence";
    render(<TicketDetailModal ticketId="1" onDismiss={vi.fn()} />);
    expect(screen.getByRole("region", { name: "Operational notes" })).toHaveTextContent(ticket.notes);
    expect(screen.getByRole("table", { name: "Ticket resolution" })).not.toHaveTextContent(ticket.notes);
    expect(ticket.resolutionNote).toBe("");
  });

  it("reopens a completed ticket with the precise displayed version", async () => {
    ticket.status = "completed";
    update.mockResolvedValue(true);
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    expect(screen.queryByRole("button", { name: "Complete repair" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Reopen ticket" }));
    await waitFor(() => expect(update).toHaveBeenCalledWith({ status: TicketStatus.OPEN, expectedUpdatedAt: version }));
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

  it("offers explicit refresh for ticket detail", async () => {
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Refresh" })));
    expect(refresh).toHaveBeenCalledOnce();
    expect(refreshOptions).toHaveBeenCalledOnce();
  });

  it("shows loading assignment options while preserving Unassign", async () => {
    ticket.siteId = null;
    ticket.assigneeUserId = "2";
    maintenanceOptions.manageableSites = [];
    maintenanceOptions.loading = true;

    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Assign" }));

    expect(screen.getByRole("status", { name: "Loading assignment options" })).toBeInTheDocument();
    expect(screen.queryByText("No assignees available.")).not.toBeInTheDocument();
    fireEvent.click(screen.getByText("Unassign"));
    await waitFor(() => expect(update).toHaveBeenCalledWith({ clearAssignee: true, expectedUpdatedAt: version }));
  });

  it("shows an empty state when no assignees are available", () => {
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Assign" }));

    expect(screen.getByText("No assignees available.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
  });

  it.each([
    ["Sites could not be loaded.", "Sites could not be loaded."],
    ["", "Couldn't load maintenance options."],
  ])("retries options error %j in the open editor and accepts recovered assignees", async (error, message) => {
    maintenanceOptions.error = error;
    const modal = (
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>
    );
    const { rerender } = render(modal);
    fireEvent.click(screen.getByRole("button", { name: "Assign" }));

    expect(screen.getByRole("alert")).toHaveTextContent(message);
    expect(screen.queryByText("No assignees available.")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Refresh" })).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(refreshOptions).toHaveBeenCalledOnce();

    maintenanceOptions.loading = true;
    rerender(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    expect(screen.getByRole("status", { name: "Loading assignment options" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();

    maintenanceOptions.loading = false;
    maintenanceOptions.error = null;
    maintenanceOptions.assignees = [{ id: "3", username: "sam" }];
    rerender(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByText("sam"));
    await waitFor(() => expect(update).toHaveBeenCalledWith({ assigneeUserId: 3n, expectedUpdatedAt: version }));
  });

  it("keeps existing assignees and Unassign available when an options refresh fails", async () => {
    ticket.assigneeUserId = "2";
    maintenanceOptions.error = "Options could not be refreshed.";
    maintenanceOptions.assignees = [{ id: "3", username: "sam" }];
    update.mockResolvedValueOnce(true).mockResolvedValueOnce(true);
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Assign" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Options could not be refreshed.");
    expect(screen.getByText("Unassign")).toBeInTheDocument();
    fireEvent.click(screen.getByText("sam"));
    await waitFor(() => expect(update).toHaveBeenCalledWith({ assigneeUserId: 3n, expectedUpdatedAt: version }));

    fireEvent.click(screen.getByRole("button", { name: "Assign" }));
    fireEvent.click(screen.getByText("Unassign"));
    await waitFor(() => expect(update).toHaveBeenCalledWith({ clearAssignee: true, expectedUpdatedAt: version }));
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
  it("hides ticket and comment mutations outside the manageable site scope", () => {
    maintenanceOptions.manageableSites = [];

    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );

    expect(screen.queryByRole("button", { name: "Assign" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Update status" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Complete repair" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add comment" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Delete comment" })).not.toBeInTheDocument();
  });

  it("opens linked miners through the supported miner viewer", async () => {
    const snapshot = {
      deviceIdentifier: ticket.minerIdentifier,
      embeddedWebViewAvailable: false,
      url: "http://miner.test",
    };
    lookupMinerByIdentifier.mockResolvedValue({ status: "found", snapshot });
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );

    await waitFor(() =>
      expect(lookupMinerByIdentifier).toHaveBeenCalledWith(
        ticket.minerIdentifier,
        MinerIdentifierType.DEVICE_IDENTIFIER,
        expect.any(AbortSignal),
      ),
    );
    const minerButton = screen.getByRole("button", { name: `Miner ${ticket.minerIdentifier}` });
    await waitFor(() => expect(minerButton).toBeEnabled());
    fireEvent.click(minerButton);

    expect(openMinerView).toHaveBeenCalledWith(snapshot);
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
    fireEvent.change(screen.getByLabelText("Internal note"), { target: { value: "Wrong ticket draft" } });
    expect(screen.getByDisplayValue("Wrong ticket draft")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Next ticket" }));

    expect(screen.queryByDisplayValue("Wrong ticket draft")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add comment" })).toBeInTheDocument();
  });

  it("shows only one ticket editor at a time", () => {
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Complete repair" }));
    expect(screen.getByRole("button", { name: "Submit completion" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Assign" }));
    expect(screen.queryByRole("button", { name: "Submit completion" })).not.toBeInTheDocument();
  });

  it("keeps the dialog in place during a comment submission", async () => {
    let finish!: (value: boolean) => void;
    addComment.mockImplementationOnce(
      () =>
        new Promise<boolean>((resolve) => {
          finish = resolve;
        }),
    );
    const dismiss = vi.fn();
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" ticketIds={["1", "2"]} onDismiss={dismiss} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Add comment" }));
    fireEvent.change(screen.getByLabelText("Internal note"), { target: { value: "Checked" } });
    fireEvent.click(screen.getByRole("button", { name: "Post" }));
    fireEvent.click(screen.getByRole("button", { name: "Close dialog" }));
    expect(dismiss).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Next ticket" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Assign" })).toBeDisabled();
    await act(async () => finish(true));
    expect(screen.getByRole("button", { name: "Next ticket" })).toBeEnabled();
  });

  it("arbitrates target saves synchronously before comments or navigation can start", async () => {
    ticket.targets = [create(TicketAssetSchema, { kind: TicketCategory.MINER, id: 1n })];
    let finish!: (ok: boolean) => void;
    const saveTarget = vi.fn(
      () =>
        new Promise<boolean>((resolve) => {
          finish = resolve;
        }),
    );
    const dismiss = vi.fn();
    render(<TicketDetailModal ticketId="1" ticketIds={["1", "2"]} onDismiss={dismiss} />);
    fireEvent.click(screen.getByRole("button", { name: "Add comment" }));
    fireEvent.change(screen.getByLabelText("Internal note"), { target: { value: "Retain comment" } });
    act(() => {
      void targetGate!(saveTarget);
      fireEvent.click(screen.getByRole("button", { name: "Post" }));
      fireEvent.click(screen.getByRole("button", { name: "Next ticket" }));
      fireEvent.click(screen.getByRole("button", { name: "Close dialog" }));
    });
    expect(saveTarget).toHaveBeenCalledOnce();
    expect(addComment).not.toHaveBeenCalled();
    expect(dismiss).not.toHaveBeenCalled();
    expect(screen.getByText("1 of 2 tickets")).toBeInTheDocument();
    expect(screen.getByLabelText("Internal note")).toHaveValue("Retain comment");
    expect(screen.getByRole("button", { name: "Post" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Delete comment" })).toBeDisabled();
    expect(screen.getByRole("alert")).toHaveTextContent("Another ticket action is still saving");
    await act(async () => finish(true));
    expect(screen.getByRole("button", { name: "Post" })).toBeEnabled();
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

  it("keeps consumed history visible and sends only active reservations to completion", () => {
    ticket.partsUsed = [
      { inventoryPartId: "7", partName: "Previously used fan", quantity: 2, consumedAt: new Date() },
      { inventoryPartId: "8", partName: "New cable", quantity: 1, consumedAt: null },
    ];
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    expect(screen.getByText("Previously used fan ×2")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Complete repair" }));
    expect(screen.getByTestId("completion-reservations")).toHaveTextContent(/^8$/);
  });

  it("submits one ticket version with completion", async () => {
    ticket.partsUsed = [{ inventoryPartId: "7", partName: "Fan", quantity: 2, consumedAt: null }];
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
          expectedUpdatedAt: version,
        }),
      ),
    );
  });

  it("keeps the ticket version fixed while completion is open", async () => {
    ticket.partsUsed = [{ inventoryPartId: "7", partName: "Fan", quantity: 2, consumedAt: null }];
    update.mockResolvedValueOnce(true);
    const view = render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Complete repair" }));

    ticket.partsUsed = [{ inventoryPartId: "8", partName: "Cable", quantity: 1, consumedAt: null }];
    ticket.updatedAtSnapshot = { ...version, nanos: 999999000 };
    view.rerender(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Submit completion" }));

    await waitFor(() =>
      expect(update).toHaveBeenCalledWith(
        expect.objectContaining({
          expectedUpdatedAt: version,
        }),
      ),
    );
  });

  it("submits the version captured when the completion editor opened", async () => {
    update.mockResolvedValueOnce(false);
    const view = render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Complete repair" }));

    ticket.status = "sent_to_vendor";
    view.rerender(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Submit completion" }));

    await waitFor(() =>
      expect(update).toHaveBeenCalledWith(
        expect.objectContaining({
          status: TicketStatus.COMPLETED,
          expectedUpdatedAt: version,
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

  it("preserves the completion draft without allowing submission after another host closes the ticket", () => {
    actualCompletionForm = true;
    const view = render(<TicketDetailModal ticketId="1" onDismiss={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Complete repair" }));
    fireEvent.change(screen.getByLabelText("Resolution note (visible to clients)"), {
      target: { value: "Keep this closure draft" },
    });
    ticket.status = "completed";
    view.rerender(<TicketDetailModal ticketId="1" onDismiss={vi.fn()} />);
    expect(screen.getByLabelText("Resolution note (visible to clients)")).toHaveValue("Keep this closure draft");
    expect(screen.getByRole("button", { name: "Complete repair" })).toBeDisabled();
    expect(update).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByLabelText("Resolution note (visible to clients)")).not.toBeInTheDocument();
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
    expect(update).toHaveBeenCalledWith({
      status: TicketStatus.OPEN,
      expectedUpdatedAt: version,
    });
  });

  it("keeps the version captured when the workflow menu opened", async () => {
    update.mockResolvedValueOnce(false);
    const view = render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Update status" }));

    ticket.status = "on_hold";
    ticket.updatedAtSnapshot = { ...version, nanos: 999999000 };
    view.rerender(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByText("Open"));

    await waitFor(() =>
      expect(update).toHaveBeenCalledWith({
        status: TicketStatus.OPEN,
        expectedUpdatedAt: version,
      }),
    );
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
        expectedUpdatedAt: version,
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
        status: TicketStatus.SENT_TO_VENDOR,
        expectedUpdatedAt: version,
      }),
    );
  });

  it("submits the opening RMA version instead of silently rebasing a draft", async () => {
    update.mockResolvedValueOnce(false);
    ticket.status = "sent_to_vendor";
    ticket.rmaVendor = "Repair Co";
    ticket.rmaTracking = "TRACK-1";
    ticket.rmaEta = null;
    const view = render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Edit RMA details" }));
    fireEvent.change(screen.getByLabelText("Tracking #"), { target: { value: "MY-DRAFT" } });

    ticket.rmaTracking = "TRACK-LIVE";
    ticket.updatedAtSnapshot = { ...version, nanos: 999999000 };
    ticket.rmaEta = new Date("2026-09-12T00:00:00Z");

    view.rerender(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" onDismiss={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Save RMA details" }));

    await waitFor(() =>
      expect(update).toHaveBeenCalledWith(
        expect.objectContaining({ expectedUpdatedAt: version, rmaTracking: "MY-DRAFT" }),
      ),
    );
    await waitFor(() => expect(screen.getByRole("button", { name: "Refresh" })).toBeEnabled());
  });
});
