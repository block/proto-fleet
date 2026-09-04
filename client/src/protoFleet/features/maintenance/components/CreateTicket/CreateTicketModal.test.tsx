import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import CreateTicketModal from "./CreateTicketModal";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

const createTicket = vi.fn();
vi.mock("@/protoFleet/api/maintenance", () => ({ useMaintenanceApi: () => ({ createTicket }) }));
vi.mock("@/protoFleet/features/maintenance/hooks/useMaintenanceOptions", () => ({
  useMaintenanceOptions: () => ({
    sites: [{ id: "8", name: "Denver" }],
    assignees: [{ id: "3", username: "alex", roleName: "Technician" }],
  }),
}));
vi.mock("./MinerTicketPicker", () => ({
  default: ({ onSelect }: { onSelect: (id: string) => void }) => (
    <button onClick={() => onSelect("miner-1")}>Choose miner-1</button>
  ),
}));
const renderModal = (onSuccess = vi.fn()) => render(<CreateTicketModal onDismiss={vi.fn()} onSuccess={onSuccess} />);
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
  it("uses live site IDs for infrastructure tickets", async () => {
    const user = userEvent.setup();
    renderModal();
    await user.click(screen.getByRole("button", { name: "Category" }));
    await user.click(screen.getByText("Infrastructure"));
    expect(screen.getByRole("button", { name: "Site" })).toHaveTextContent("Denver");
  });
});
