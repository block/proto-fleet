import { act, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import CreateTicketModal from "./CreateTicketModal";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

const createTicket = vi.fn();
let canReadMiners = true;
vi.mock("@/protoFleet/api/maintenance", () => ({ useMaintenanceApi: () => ({ createTicket }) }));
vi.mock("@/protoFleet/store", () => ({
  useHasPermission: (permission: string) => permission !== "miner:read" || canReadMiners,
}));
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
vi.mock("./MinerTicketPicker", () => ({
  default: ({ onSelect }: { onSelect: (id: string) => void }) => (
    <button onClick={() => onSelect("miner-1")}>Choose miner-1</button>
  ),
}));
const renderModal = (onSuccess = vi.fn(), onDismiss = vi.fn()) =>
  render(<CreateTicketModal onDismiss={onDismiss} onSuccess={onSuccess} />);
describe("CreateTicketModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    canReadMiners = true;
    useFleetStore.setState((state) => {
      state.ui.activeSite = DEFAULT_ACTIVE_SITE;
    });
    createTicket.mockImplementation(async ({ onSuccess }) => onSuccess({}));
  });
  it("selects a miner and submits canonical inputs", async () => {
    const user = userEvent.setup();
    const success = vi.fn();
    renderModal(success);
    await user.click(screen.getByRole("button", { name: "Select miner" }));
    await user.click(screen.getByRole("button", { name: "Choose miner-1" }));
    await user.click(screen.getByRole("button", { name: "Component" }));
    await user.click(screen.getByText("Fan"));
    await user.type(screen.getByRole("textbox", { name: "Issue description" }), "Broken fan");
    await user.click(screen.getByRole("button", { name: "Create ticket" }));
    expect(createTicket).toHaveBeenCalledWith(
      expect.objectContaining({ minerIdentifier: "miner-1", component: "Fan", diagnosis: "Broken fan" }),
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
    await user.type(screen.getByRole("textbox", { name: "Issue description" }), "Broken fan");

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
    await user.type(screen.getByRole("textbox", { name: "Issue description" }), "Broken fan");

    await user.click(screen.getByRole("button", { name: "Create ticket" }));
    await user.click(screen.getByRole("button", { name: "Close dialog" }));
    await user.keyboard("{Escape}");

    expect(dismiss).not.toHaveBeenCalled();
    await act(async () => finishRequest());
  });

  it("allows a maintenance-only manager to enter a miner identifier", async () => {
    canReadMiners = false;
    const user = userEvent.setup();
    renderModal();

    const input = screen.getByRole("textbox", { name: "Miner ID" });
    expect(input).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Select miner" })).not.toBeInTheDocument();
    await user.type(input, "known-miner-42");
    await user.click(screen.getByRole("button", { name: "Component" }));
    await user.click(screen.getByText("Fan"));
    await user.type(screen.getByRole("textbox", { name: "Issue description" }), "Broken fan");
    await user.click(screen.getByRole("button", { name: "Create ticket" }));

    expect(createTicket).toHaveBeenCalledWith(expect.objectContaining({ minerIdentifier: "known-miner-42" }));
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
});
