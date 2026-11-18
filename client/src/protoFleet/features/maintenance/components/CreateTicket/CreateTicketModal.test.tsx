import { MemoryRouter } from "react-router-dom";
import { act, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import CreateTicketModal from "./CreateTicketModal";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

const createTicket = vi.fn();
const listTicketAssets = vi.fn(async ({ onSuccess }) =>
  onSuccess({
    assets: [
      { kind: 3, id: 51n, label: "Module 51", model: "Model A", siteId: 8n },
      { kind: 4, id: 84n, moduleId: 51n, label: "HB-84", model: "Model A", siteId: 8n },
    ],
    nextPageToken: "",
  }),
);
vi.mock("@/protoFleet/api/maintenance", () => ({ useMaintenanceApi: () => ({ createTicket, listTicketAssets }) }));
vi.mock("@/protoFleet/features/maintenance/hooks/useMaintenanceOptions", () => ({
  useMaintenanceOptions: () => ({
    sites: [
      { id: "8", name: "Denver" },
      { id: "9", name: "Read-only site" },
    ],
    manageableSites: [{ id: "8", name: "Denver" }],
    assignees: [{ id: "3", username: "alex", roleName: "Technician" }],
  }),
}));
const renderModal = (onSuccess = vi.fn(), onDismiss = vi.fn()) =>
  render(
    <MemoryRouter>
      <CreateTicketModal onDismiss={onDismiss} onSuccess={onSuccess} />
    </MemoryRouter>,
  );
describe("CreateTicketModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useFleetStore.setState((state) => {
      state.ui.activeSite = DEFAULT_ACTIVE_SITE;
    });
    createTicket.mockImplementation(async ({ onSuccess }) => onSuccess({}));
  });
  it("selects a miner and submits canonical inputs", async () => {
    const user = userEvent.setup();
    const success = vi.fn();
    renderModal(success);
    expect(screen.queryByLabelText("Diagnosis")).not.toBeInTheDocument();
    await user.type(screen.getByRole("textbox", { name: "Miner ID" }), "miner-1");
    await user.click(screen.getByRole("button", { name: "Component" }));
    await user.click(screen.getByText("Fan"));
    expect(screen.getByRole("button", { name: "Create ticket" })).toBeDisabled();
    await user.type(screen.getByRole("textbox", { name: "Description" }), "Broken fan");
    await user.click(screen.getByRole("button", { name: "Create ticket" }));
    expect(createTicket).toHaveBeenCalledWith(
      expect.objectContaining({
        minerIdentifier: "miner-1",
        component: "Fan",
        description: "Broken fan",
        shareWithClients: false,
      }),
    );
    expect(success).toHaveBeenCalled();
  });
  it("reuses one idempotency key when a create response is lost and retried", async () => {
    createTicket.mockImplementation(async ({ onError, onFinally }) => {
      onError("response lost");
      onFinally();
    });
    const user = userEvent.setup();
    renderModal();
    await user.type(screen.getByRole("textbox", { name: "Miner ID" }), "miner-1");
    await user.click(screen.getByRole("button", { name: "Component" }));
    await user.click(screen.getByText("Fan"));
    await user.type(screen.getByRole("textbox", { name: "Description" }), "Broken fan");

    await user.click(screen.getByRole("button", { name: "Create ticket" }));
    await user.click(screen.getByRole("button", { name: "Create ticket" }));

    expect(createTicket).toHaveBeenCalledTimes(2);
    const firstKey = createTicket.mock.calls[0][0].idempotencyKey;
    const secondKey = createTicket.mock.calls[1][0].idempotencyKey;
    expect(firstKey).toMatch(/^[0-9a-f-]{36}$/);
    expect(secondKey).toBe(firstKey);
  });

  it("ignores close actions while ticket creation is pending", async () => {
    let finishRequest!: () => void;
    createTicket.mockImplementation(
      ({ onFinally }) =>
        new Promise<void>((resolve) => {
          finishRequest = () => {
            onFinally();
            resolve();
          };
        }),
    );
    const user = userEvent.setup();
    const dismiss = vi.fn();
    renderModal(vi.fn(), dismiss);
    await user.type(screen.getByRole("textbox", { name: "Miner ID" }), "miner-1");
    await user.click(screen.getByRole("button", { name: "Component" }));
    await user.click(screen.getByText("Fan"));
    await user.type(screen.getByRole("textbox", { name: "Description" }), "Broken fan");

    await user.click(screen.getByRole("button", { name: "Create ticket" }));
    await user.click(screen.getByRole("button", { name: "Close dialog" }));
    await user.keyboard("{Escape}");

    expect(dismiss).not.toHaveBeenCalled();
    await act(async () => finishRequest());
  });

  it("lets a maintenance manager enter a miner identifier or pick affected equipment", async () => {
    const user = userEvent.setup();
    renderModal();

    const input = screen.getByRole("textbox", { name: "Miner ID" });
    expect(input).toBeEnabled();
    expect(screen.getByRole("button", { name: "Select affected equipment" })).toBeEnabled();
    await user.type(input, "known-miner-42");
    await user.click(screen.getByRole("button", { name: "Component" }));
    await user.click(screen.getByText("Fan"));
    await user.type(screen.getByRole("textbox", { name: "Description" }), "Broken fan");
    await user.click(screen.getByRole("button", { name: "Create ticket" }));

    expect(createTicket).toHaveBeenCalledWith(expect.objectContaining({ minerIdentifier: "known-miner-42" }));
  });

  it("creates module tickets using the selected physical identity", async () => {
    const user = userEvent.setup();
    renderModal();
    await user.click(screen.getByRole("button", { name: "Select affected equipment" }));
    await screen.findByText(/^Module 51/);
    expect(listTicketAssets).toHaveBeenCalledWith(expect.objectContaining({ siteIds: [8n], includeUnassigned: true }));
    const checkboxes = within(screen.getByRole("dialog", { name: "Select affected assets" })).getAllByRole("checkbox");
    await user.click(checkboxes[0]);
    await user.click(checkboxes[1]);
    await user.click(screen.getByRole("button", { name: "Select assets" }));
    await user.click(screen.getByRole("button", { name: "Component" }));
    await user.click(screen.getByText("PSU", { exact: true }));
    await user.type(screen.getByLabelText("Description"), "Power supply failed");
    await user.click(screen.getByRole("button", { name: "Create ticket" }));
    expect(createTicket).toHaveBeenCalledWith(
      expect.objectContaining({
        category: 3,
        targets: [
          { kind: 3, id: 51n },
          { kind: 4, id: 84n, parentModuleId: 51n },
        ],
        minerIdentifier: undefined,
      }),
    );
  });
  it("keeps a selected hashboard's parent when creating a board-only ticket", async () => {
    const user = userEvent.setup();
    renderModal();
    await user.click(screen.getByRole("button", { name: "Select affected equipment" }));
    const picker = within(screen.getByRole("dialog", { name: "Select affected assets" }));
    const checkboxes = await picker.findAllByRole("checkbox");
    await user.click(checkboxes[1]);
    await user.click(picker.getByRole("button", { name: "Select assets" }));
    await user.type(screen.getByLabelText("Description"), "Board stopped reporting");
    await user.click(screen.getByRole("button", { name: "Create ticket" }));
    expect(createTicket).toHaveBeenCalledWith(
      expect.objectContaining({
        category: 4,
        component: "Unknown",
        targets: [{ kind: 4, id: 84n, parentModuleId: 51n }],
      }),
    );
  });
  it("uses live site IDs for infrastructure tickets", async () => {
    const user = userEvent.setup();
    renderModal();
    await user.click(screen.getByRole("button", { name: "Category" }));
    await user.click(screen.getByText("Infrastructure"));
    const sitePicker = screen.getByRole("button", { name: "Site" });
    expect(sitePicker).toHaveTextContent("Denver");
    await user.click(sitePicker);
    expect(screen.queryByText("Read-only site")).not.toBeInTheDocument();
  });
  it("creates Other tickets with a site and Unknown component by default", async () => {
    const user = userEvent.setup();
    renderModal();
    await user.click(screen.getByRole("button", { name: "Category" }));
    expect(screen.queryByText("Module", { exact: true })).not.toBeInTheDocument();
    expect(screen.queryByText("Hashboard", { exact: true })).not.toBeInTheDocument();
    await user.click(screen.getByText("Other", { exact: true }));
    expect(screen.getByRole("button", { name: "Site" })).toHaveTextContent("Denver");
    expect(screen.getByRole("button", { name: "Component" })).toHaveTextContent("Unknown");
    await user.type(screen.getByLabelText("Description"), "Site access issue");
    await user.click(screen.getByRole("button", { name: "Create ticket" }));
    expect(createTicket).toHaveBeenCalledWith(
      expect.objectContaining({
        category: 6,
        siteId: 8n,
        component: "Unknown",
        targets: [],
        shareWithClients: false,
      }),
    );
  });
  it("requires explicit affected clients before sharing an Other ticket", async () => {
    const user = userEvent.setup();
    renderModal();
    await user.click(screen.getByRole("button", { name: "Category" }));
    await user.click(screen.getByText("Other", { exact: true }));
    await user.type(screen.getByLabelText("Description"), "Site access issue");
    await user.click(screen.getByRole("button", { name: "Visibility" }));
    await user.click(screen.getByText("Shared with affected clients", { exact: true }));
    expect(screen.getByRole("button", { name: "Create ticket" })).toBeDisabled();
    await user.type(screen.getByLabelText("Affected client account IDs (optional, comma-separated)"), "client-one");
    await user.click(screen.getByRole("button", { name: "Create ticket" }));
    expect(createTicket).toHaveBeenCalledWith(
      expect.objectContaining({ clientAccountIds: ["client-one"], shareWithClients: true }),
    );
  });
});
