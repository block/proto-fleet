import { MemoryRouter } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import TicketTargets from "./TicketTargets";
import { MinerIdentifierType } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { RepairTicketDetailSchema, TicketCategory } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import { toTicketDetail } from "@/protoFleet/features/maintenance/mappers";

const updateTarget = vi.fn();
const updateTicket = vi.fn();
const listTicketAssets = vi.fn();
const lookupMiner = vi.fn();
vi.mock("@/protoFleet/api/lookupMinerByIdentifier", () => ({
  lookupMinerByIdentifier: (...args: unknown[]) => lookupMiner(...args),
}));
vi.mock("@/protoFleet/api/maintenance", () => ({
  useMaintenanceApi: () => ({ updateTarget, updateTicket, listTicketAssets }),
}));
const version = { $typeName: "google.protobuf.Timestamp" as const, seconds: 1700000000n, nanos: 123456789 };
const incident = () =>
  toTicketDetail(
    create(RepairTicketDetailSchema, {
      ticket: {
        id: 7n,
        updatedAt: version,
        targets: [{ kind: TicketCategory.MODULE, id: 51n, label: "Module 51", model: "Model A", siteId: 1n }],
      },
    }),
  );
beforeEach(() => {
  vi.clearAllMocks();
  lookupMiner.mockResolvedValue({ status: "notFound" });
});
it("records evidence against the original physical target and exact version", async () => {
  updateTarget.mockImplementation(async ({ onSuccess }) => onSuccess());
  render(
    <MemoryRouter>
      <TicketTargets ticket={incident()} canManage canReadMiners={false} onOpenMiner={vi.fn()} onSaved={vi.fn()} />
    </MemoryRouter>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Edit repair details for Module 51" }));
  fireEvent.change(screen.getByLabelText("Diagnosis"), { target: { value: "Power connector failed" } });
  fireEvent.click(screen.getByRole("button", { name: "Save repair details" }));
  await waitFor(() =>
    expect(updateTarget).toHaveBeenCalledWith(
      expect.objectContaining({
        ticketId: 7n,
        kind: TicketCategory.MODULE,
        assetId: 51n,
        expectedUpdatedAt: version,
        diagnosis: "Power connector failed",
      }),
    ),
  );
});
it("requires a distinct replacement module with the same actual model", () => {
  render(
    <MemoryRouter>
      <TicketTargets ticket={incident()} canManage canReadMiners={false} onOpenMiner={vi.fn()} onSaved={vi.fn()} />
    </MemoryRouter>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Edit repair details for Module 51" }));
  fireEvent.change(screen.getByLabelText("Replacement module ID (optional)"), { target: { value: "51" } });
  fireEvent.change(screen.getByLabelText("Replacement module model"), { target: { value: "Model A" } });
  expect(screen.getByRole("button", { name: "Save repair details" })).toBeDisabled();
  fireEvent.change(screen.getByLabelText("Replacement module ID (optional)"), { target: { value: "52" } });
  fireEvent.change(screen.getByLabelText("Replacement module model"), { target: { value: "Model B" } });
  expect(screen.getByRole("button", { name: "Save repair details" })).toBeDisabled();
  fireEvent.change(screen.getByLabelText("Replacement module model"), { target: { value: "Model A" } });
  expect(screen.getByRole("button", { name: "Save repair details" })).toBeEnabled();
});
it("uses the incident's historical boards when adding affected boards", async () => {
  listTicketAssets.mockImplementation(async ({ onSuccess }) =>
    onSuccess([
      { kind: TicketCategory.HASHBOARD, id: 84n, label: "HB-84", moduleId: 51n, siteId: 1n, model: "Model A" },
    ]),
  );
  render(
    <MemoryRouter>
      <TicketTargets ticket={incident()} canManage canReadMiners={false} onOpenMiner={vi.fn()} onSaved={vi.fn()} />
    </MemoryRouter>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Add boards" }));
  await screen.findByText(/HB-84/);
  expect(listTicketAssets).toHaveBeenCalledWith(expect.objectContaining({ ticketId: 7n }));
  expect(listTicketAssets.mock.calls[0][0]).not.toHaveProperty("siteIds");
  fireEvent.click(screen.getByRole("checkbox"));
  fireEvent.click(screen.getByRole("button", { name: "Select assets" }));
  expect(updateTicket).toHaveBeenCalledWith(
    expect.objectContaining({
      id: 7n,
      expectedUpdatedAt: version,
      affectedBoards: [{ kind: TicketCategory.HASHBOARD, id: 84n, parentModuleId: 51n }],
    }),
  );
});

it("records the actual parent module model directly on a standalone board target", async () => {
  updateTarget.mockImplementation(async ({ onSuccess }) => onSuccess());
  const ticket = incident();
  ticket.targets[0].kind = TicketCategory.HASHBOARD;
  ticket.targets[0].id = 84n;
  ticket.targets[0].label = "HB-84";
  ticket.targets[0].moduleId = 51n;
  ticket.targets[0].model = "";
  render(
    <MemoryRouter>
      <TicketTargets ticket={ticket} canManage canReadMiners={false} onOpenMiner={vi.fn()} onSaved={vi.fn()} />
    </MemoryRouter>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Edit repair details for HB-84" }));
  expect(screen.queryByLabelText("Replacement module ID (optional)")).not.toBeInTheDocument();
  fireEvent.change(screen.getByLabelText("Actual module model"), { target: { value: "Model A" } });
  fireEvent.click(screen.getByRole("button", { name: "Save repair details" }));
  await waitFor(() =>
    expect(updateTarget).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: TicketCategory.HASHBOARD,
        assetId: 84n,
        moduleModel: "Model A",
        expectedUpdatedAt: version,
      }),
    ),
  );
});

const minerIncident = () =>
  toTicketDetail(
    create(RepairTicketDetailSchema, {
      ticket: {
        id: 8n,
        updatedAt: version,
        targets: [
          { kind: TicketCategory.MINER, id: 1n, label: "SN-1", minerIdentifier: "device-1" },
          { kind: TicketCategory.MINER, id: 2n, label: "SN-2", minerIdentifier: "device-2" },
        ],
      },
    }),
  );

it("opens each affected miner through its identifier and existing equipment label", async () => {
  const snapshots = [{ deviceIdentifier: "device-1" }, { deviceIdentifier: "device-2" }];
  lookupMiner.mockImplementation(async (identifier) => ({
    status: "found",
    snapshot: snapshots.find((snapshot) => snapshot.deviceIdentifier === identifier),
  }));
  const onOpenMiner = vi.fn();
  render(
    <TicketTargets
      ticket={minerIncident()}
      canManage={false}
      canReadMiners
      onOpenMiner={onOpenMiner}
      onSaved={vi.fn()}
    />,
  );
  const first = screen.getByRole("button", { name: "Miner SN-1 · ID 1 · Model unknown" });
  const second = screen.getByRole("button", { name: "Miner SN-2 · ID 2 · Model unknown" });
  expect(first).toBeEnabled();
  expect(second).toBeEnabled();
  expect(lookupMiner).not.toHaveBeenCalled();
  fireEvent.click(first);
  await waitFor(() => expect(onOpenMiner).toHaveBeenCalledWith(snapshots[0]));
  fireEvent.click(second);
  await waitFor(() => expect(onOpenMiner).toHaveBeenCalledWith(snapshots[1]));
  for (const identifier of ["device-1", "device-2"]) {
    expect(lookupMiner).toHaveBeenCalledWith(
      identifier,
      MinerIdentifierType.DEVICE_IDENTIFIER,
      expect.any(AbortSignal),
    );
  }
  expect(onOpenMiner.mock.calls).toEqual([[snapshots[0]], [snapshots[1]]]);
  fireEvent.click(first);
  expect(lookupMiner).toHaveBeenCalledTimes(2);
  expect(onOpenMiner).toHaveBeenLastCalledWith(snapshots[0]);
});

it("keeps equipment labels without looking up miners when miner access is unavailable", () => {
  render(
    <TicketTargets
      ticket={minerIncident()}
      canManage={false}
      canReadMiners={false}
      onOpenMiner={vi.fn()}
      onSaved={vi.fn()}
    />,
  );
  expect(screen.getByText("Miner SN-1 · ID 1 · Model unknown")).toBeInTheDocument();
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
  expect(lookupMiner).not.toHaveBeenCalled();
});

it.each([
  [{ status: "notFound" }, "Miner is no longer available."],
  [{ status: "error", message: "Cannot read this miner." }, "Cannot read this miner."],
])("preserves lookup failures for affected miners: %j", async (result, message) => {
  lookupMiner.mockResolvedValue(result);
  const ticket = minerIncident();
  ticket.targets = ticket.targets.slice(0, 1);
  render(<TicketTargets ticket={ticket} canManage={false} canReadMiners onOpenMiner={vi.fn()} onSaved={vi.fn()} />);
  fireEvent.click(screen.getByRole("button", { name: /Miner SN-1/ }));
  expect(await screen.findByRole("alert")).toHaveTextContent(message as string);
  expect(screen.getByRole("button", { name: /Miner SN-1/ })).toBeEnabled();
});

it("cancels pending lookups on ticket navigation and dismissal and ignores stale results", async () => {
  const requests: { resolve: (value: unknown) => void; signal: AbortSignal }[] = [];
  lookupMiner.mockImplementation(
    (_identifier, _type, signal) =>
      new Promise((resolve) => {
        requests.push({ resolve, signal });
      }),
  );
  const ticket = minerIncident();
  ticket.targets = ticket.targets.slice(0, 1);
  const onOpenMiner = vi.fn();
  const props = { canManage: false, canReadMiners: true, onOpenMiner, onSaved: vi.fn() };
  const { rerender, unmount } = render(<TicketTargets key={ticket.id} ticket={ticket} {...props} />);
  fireEvent.click(screen.getByRole("button", { name: /Miner SN-1/ }));
  expect(screen.getByRole("button", { name: /Miner SN-1/ })).toBeDisabled();
  const next = minerIncident();
  next.id = "9";
  next.targets = next.targets.slice(1);
  rerender(<TicketTargets key={next.id} ticket={next} {...props} />);
  expect(requests[0].signal.aborted).toBe(true);
  await act(async () => requests[0].resolve({ status: "found", snapshot: { deviceIdentifier: "device-1" } }));
  expect(screen.getByRole("button", { name: /Miner SN-2/ })).toBeEnabled();
  expect(onOpenMiner).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: /Miner SN-2/ }));
  unmount();
  expect(requests[1].signal.aborted).toBe(true);
  await act(async () => requests[1].resolve({ status: "error", message: "Late failure" }));
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});

it("blocks miner navigation during a ticket mutation, including a pending lookup", async () => {
  let resolveLookup: (value: unknown) => void = () => {};
  lookupMiner.mockImplementation(
    () =>
      new Promise((resolve) => {
        resolveLookup = resolve;
      }),
  );
  const ticket = minerIncident();
  ticket.targets = ticket.targets.slice(0, 1);
  const onOpenMiner = vi.fn();
  const props = { ticket, canManage: false, canReadMiners: true, onOpenMiner, onSaved: vi.fn() };
  const { rerender } = render(<TicketTargets {...props} disabled />);
  const button = screen.getByRole("button", { name: /Miner SN-1/ });
  expect(button).toBeDisabled();
  fireEvent.click(button);
  expect(lookupMiner).not.toHaveBeenCalled();
  rerender(<TicketTargets {...props} disabled={false} />);
  fireEvent.click(button);
  rerender(<TicketTargets {...props} disabled />);
  await act(async () => resolveLookup({ status: "found", snapshot: { deviceIdentifier: "device-1" } }));
  expect(onOpenMiner).not.toHaveBeenCalled();
  rerender(<TicketTargets {...props} disabled={false} />);
  fireEvent.click(button);
  expect(onOpenMiner).toHaveBeenCalledWith({ deviceIdentifier: "device-1" });
  expect(lookupMiner).toHaveBeenCalledTimes(1);
});

it("sends evidence saves through the shared mutation gate and retains a rejected draft", async () => {
  const submitMutation = vi.fn(async (_mutation: () => Promise<boolean>) => false);
  render(
    <MemoryRouter>
      <TicketTargets
        ticket={incident()}
        canManage
        canReadMiners={false}
        onOpenMiner={vi.fn()}
        onSaved={vi.fn()}
        submitMutation={submitMutation}
      />
    </MemoryRouter>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Edit repair details for Module 51" }));
  fireEvent.change(screen.getByLabelText("Diagnosis"), { target: { value: "Keep evidence draft" } });
  fireEvent.click(screen.getByRole("button", { name: "Save repair details" }));
  await waitFor(() => expect(submitMutation).toHaveBeenCalledOnce());
  expect(updateTarget).not.toHaveBeenCalled();
  expect(screen.getByLabelText("Diagnosis")).toHaveValue("Keep evidence draft");
});

it("keeps a target draft across a refresh and only adopts the current version after review", async () => {
  updateTarget.mockImplementation(async ({ onError }) => onError("Ticket changed"));
  const props = { canManage: true, canReadMiners: false, onOpenMiner: vi.fn(), onSaved: vi.fn() };
  const { rerender } = render(
    <MemoryRouter>
      <TicketTargets ticket={incident()} {...props} />
    </MemoryRouter>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Edit repair details for Module 51" }));
  fireEvent.change(screen.getByLabelText("Diagnosis"), { target: { value: "Keep evidence draft" } });
  fireEvent.click(screen.getByRole("button", { name: "Save repair details" }));
  await screen.findByText("Ticket changed");
  const refreshed = incident();
  refreshed.updatedAtSnapshot = { ...version, nanos: version.nanos + 1 };
  refreshed.targets[0].diagnosis = "Other host evidence";
  rerender(
    <MemoryRouter>
      <TicketTargets ticket={refreshed} {...props} />
    </MemoryRouter>,
  );
  expect(screen.getByLabelText("Diagnosis")).toHaveValue("Keep evidence draft");
  expect(screen.getByText("Current diagnosis: Other host evidence")).toBeInTheDocument();
  expect(updateTarget).toHaveBeenCalledOnce();
  fireEvent.click(screen.getByRole("button", { name: "Use refreshed repair version" }));
  fireEvent.click(screen.getByRole("button", { name: "Save repair details" }));
  await waitFor(() =>
    expect(updateTarget).toHaveBeenLastCalledWith(
      expect.objectContaining({ expectedUpdatedAt: refreshed.updatedAtSnapshot, diagnosis: "Keep evidence draft" }),
    ),
  );
});

it("routes board additions through the same gate and disables target controls while another action saves", async () => {
  listTicketAssets.mockImplementation(async ({ onSuccess }) =>
    onSuccess([{ kind: TicketCategory.HASHBOARD, id: 84n, label: "HB-84", moduleId: 51n }]),
  );
  const submitMutation = vi.fn(async (mutation: () => Promise<boolean>) => mutation());
  updateTicket.mockImplementation(async ({ onSuccess }) => onSuccess());
  const props = {
    ticket: incident(),
    canManage: true,
    canReadMiners: false,
    onOpenMiner: vi.fn(),
    onSaved: vi.fn(),
    submitMutation,
  };
  const { rerender } = render(
    <MemoryRouter>
      <TicketTargets {...props} disabled />
    </MemoryRouter>,
  );
  expect(screen.getByRole("button", { name: "Add boards" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Edit repair details for Module 51" })).toBeDisabled();
  rerender(
    <MemoryRouter>
      <TicketTargets {...props} />
    </MemoryRouter>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Add boards" }));
  await screen.findByText(/HB-84/);
  fireEvent.click(screen.getByRole("checkbox"));
  fireEvent.click(screen.getByRole("button", { name: "Select assets" }));
  await waitFor(() => expect(submitMutation).toHaveBeenCalledOnce());
  expect(updateTicket).toHaveBeenCalledOnce();
});

it.each([
  [TicketCategory.MODULE, "Replacement module ID (optional)", "clearReplacementModule"],
  [TicketCategory.HASHBOARD, "Replacement hashboard ID (optional)", "clearReplacementHashboard"],
] as const)(
  "sends an explicit clear when the optional replacement input is emptied for kind %s",
  async (kind, label, flag) => {
    updateTarget.mockImplementation(async ({ onSuccess }) => onSuccess());
    const ticket = incident();
    ticket.targets[0].kind = kind;
    if (kind === TicketCategory.MODULE) ticket.targets[0].replacementModuleId = 52n;
    else {
      ticket.targets[0].moduleId = 50n;
      ticket.targets[0].replacementHashboardId = 85n;
    }
    render(
      <MemoryRouter>
        <TicketTargets ticket={ticket} canManage canReadMiners={false} onOpenMiner={vi.fn()} onSaved={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Edit repair details for Module 51" }));
    fireEvent.change(screen.getByLabelText(label), { target: { value: "" } });
    fireEvent.click(screen.getByRole("button", { name: "Save repair details" }));
    await waitFor(() => expect(updateTarget).toHaveBeenCalledOnce());
    expect(updateTarget).toHaveBeenCalledWith(
      expect.objectContaining({
        kind,
        assetId: 51n,
        expectedUpdatedAt: version,
        moduleModel: "Model A",
        replacementModuleId: undefined,
        replacementModuleModel: undefined,
        replacementHashboardId: undefined,
        clearReplacementModule: kind === TicketCategory.MODULE,
        clearReplacementHashboard: kind === TicketCategory.HASHBOARD,
        [flag]: true,
      }),
    );
  },
);

it("keeps a blank replacement draft explicit after reviewing a conflicting update", async () => {
  updateTarget.mockImplementation(async ({ onError }) => onError("Ticket changed"));
  const props = { canManage: true, canReadMiners: false, onOpenMiner: vi.fn(), onSaved: vi.fn() };
  const { rerender } = render(
    <MemoryRouter>
      <TicketTargets ticket={incident()} {...props} />
    </MemoryRouter>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Edit repair details for Module 51" }));
  fireEvent.click(screen.getByRole("button", { name: "Save repair details" }));
  await screen.findByText("Ticket changed");
  const refreshed = incident();
  refreshed.updatedAtSnapshot = { ...version, nanos: version.nanos + 1 };
  refreshed.targets[0].replacementModuleId = 52n;
  rerender(
    <MemoryRouter>
      <TicketTargets ticket={refreshed} {...props} />
    </MemoryRouter>,
  );
  expect(screen.getByLabelText("Replacement module ID (optional)")).toHaveValue("");
  expect(screen.getByText("Current replacement module id: 52")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Use refreshed repair version" }));
  fireEvent.click(screen.getByRole("button", { name: "Save repair details" }));
  await waitFor(() =>
    expect(updateTarget).toHaveBeenLastCalledWith(
      expect.objectContaining({
        expectedUpdatedAt: refreshed.updatedAtSnapshot,
        replacementModuleId: undefined,
        clearReplacementModule: true,
        clearReplacementHashboard: false,
      }),
    ),
  );
});
