import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import ContainerSiteDialog from "./ContainerSiteDialog";
import { ContainerSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";

vi.mock("@/shared/components/Popover/usePopoverPosition", () => ({
  default: () => ({ popoverRef: { current: null }, popoverStyle: {}, popoverAnimation: "" }),
}));

const mocks = vi.hoisted(() => ({
  listSites: vi.fn(),
  assign: vi.fn(),
  listContainers: vi.fn(),
  bump: vi.fn(),
  auth: vi.fn(),
}));
vi.mock("@/protoFleet/api/clients", () => ({
  sitesClient: { listSites: mocks.listSites, assignContainersToSite: mocks.assign },
  containersClient: { listContainers: mocks.listContainers },
}));
vi.mock("@/protoFleet/store", () => ({ useAuthErrors: () => ({ handleAuthErrors: mocks.auth }) }));
vi.mock("@/protoFleet/store/useFleetStore", () => ({
  useFleetStore: (selector: (state: unknown) => unknown) =>
    selector({ ui: { sitesRevision: 0, bumpSitesRevision: mocks.bump } }),
}));
vi.mock("@/shared/features/toaster", () => ({ pushToast: vi.fn(), STATUSES: { success: "success" } }));

const container = create(ContainerSchema, {
  deviceIdentifier: "controller-1",
  name: "CT1",
  site: { id: 1n, label: "North" },
});
beforeEach(() => {
  vi.clearAllMocks();
  mocks.assign.mockResolvedValue({ reassignedCount: 1n });
  mocks.listSites.mockResolvedValue({
    sites: [{ site: { id: 1n, name: "North" } }, { site: { id: 2n, name: "South" } }],
  });
  mocks.listContainers.mockResolvedValue({
    containers: [container, create(ContainerSchema, { deviceIdentifier: "loose", name: "Unassigned CT1" })],
  });
});
const choose = async (name: string) => {
  const button = screen.getByRole("button", { name: "Site" });
  await waitFor(() => expect(button).toBeEnabled());
  fireEvent.click(button);
  fireEvent.click(await screen.findByRole("option", { name }));
};
describe("Container site assignment", () => {
  it("moves a container with the dedicated API and refreshes site data", async () => {
    const close = vi.fn();
    const saved = vi.fn();
    render(<ContainerSiteDialog container={container} onClose={close} onSaved={saved} />);
    await choose("South");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(close).toHaveBeenCalledOnce());
    expect(mocks.assign).toHaveBeenCalledWith({ containerIdentifiers: ["controller-1"], targetSiteId: 2n });
    expect(mocks.bump).toHaveBeenCalledOnce();
    expect(saved).toHaveBeenCalledOnce();
  });
  it("unassigns using an omitted site, and keeps a failed move open", async () => {
    mocks.assign.mockRejectedValueOnce(new Error("Source site permission denied"));
    const close = vi.fn();
    render(<ContainerSiteDialog container={container} onClose={close} />);
    await choose("Unassigned");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Source site permission denied");
    expect(mocks.assign).toHaveBeenCalledWith({ containerIdentifiers: ["controller-1"], targetSiteId: undefined });
    expect(close).not.toHaveBeenCalled();
    expect(mocks.bump).not.toHaveBeenCalled();
  });
  it.each(["South", "Unassigned"])("shows a styled, readable permission error for %s", async (target) => {
    mocks.assign.mockRejectedValueOnce(
      new ConnectError(JSON.stringify({ required: "site:manage", scope: { site_id: 2 } }), Code.PermissionDenied),
    );
    const close = vi.fn();
    render(<ContainerSiteDialog container={container} onClose={close} />);
    await choose(target);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Couldn't change site");
    expect(alert).toHaveTextContent("You don't have permission to change site assignments.");
    expect(alert).toHaveTextContent("Ask an administrator");
    expect(alert).not.toHaveTextContent("site:manage");
    expect(alert.querySelector(".text-intent-critical-fill")).toBeInTheDocument();
    expect(close).not.toHaveBeenCalled();
    expect(mocks.bump).not.toHaveBeenCalled();
    await choose("North");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
