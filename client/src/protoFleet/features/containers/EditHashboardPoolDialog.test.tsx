import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import HashboardPoolAction from "./HashboardPoolAction";
import MiningActionsMenu from "./MiningActionsMenu";
import {
  EntityKind,
  HashboardSlotSchema,
  InventoryItemSchema,
} from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import {
  type GetStateResponse,
  GetStateResponseSchema,
} from "@/protoFleet/api/generated/mininggroups/v1/mininggroups_pb";

const mocks = vi.hoisted(() => ({
  permissions: [] as string[],
  verify: vi.fn(),
  options: vi.fn(),
  editor: vi.fn(),
  groups: vi.fn(),
}));
vi.mock("@/protoFleet/api/clients", () => ({
  miningGroupsClient: { getState: mocks.groups },
  authClient: { verifyCredentials: mocks.verify },
  containersClient: { getModuleMiningOptions: mocks.options },
}));
vi.mock("@/protoFleet/store", async (original) => ({
  ...(await original<typeof import("@/protoFleet/store")>()),
  usePermissions: () => mocks.permissions,
}));
vi.mock("@/protoFleet/features/poolConfigurations/api", () => ({
  usePoolConfigurationClient: () => ({
    accountId: "",
    readOnly: false,
    client: {
      listConfigurations: async () => ({ configurations: [], nextPageToken: "" }),
      getAssignments: mocks.editor,
    },
  }),
  usePoolConfigurationPermissions: () => ({
    read: true,
    manage: false,
    change: mocks.permissions.includes("miner:update_pools"),
  }),
}));
vi.mock("@/shared/components/Popover/usePopoverPosition", () => ({
  default: () => ({ popoverRef: { current: null }, popoverStyle: {}, popoverAnimation: "" }),
}));
beforeEach(() => {
  vi.clearAllMocks();
  mocks.permissions = ["miner:update_pools", "pool:read", "miner:start_mining", "miner:stop_mining"];
  mocks.options.mockResolvedValue({ canStart: false, canStop: false, unavailableReason: "" });
  mocks.verify.mockResolvedValue({});
  mocks.editor.mockResolvedValue({ assignments: [], selectedCount: 1n });
  mocks.groups.mockReset().mockResolvedValue({
    supported: true,
    enabled: true,
    observedAt: timestampFromDate(new Date()),
    status: "complete",
  });
});

function mountAction(kind: "module" | "hashboard") {
  if (kind === "module") {
    render(
      <MiningActionsMenu
        kind="module"
        items={[
          create(InventoryItemSchema, {
            ref: { kind: EntityKind.MODULE, id: 11n },
            name: "Module 11",
            workerNameSummary: "rack-one",
            deviceIdentifier: "ct1",
          }),
        ]}
      />,
    );
  } else {
    render(
      <HashboardPoolAction
        deviceIdentifier="ct1"
        slot={create(HashboardSlotSchema, {
          position: 1,
          occupancy: "occupied",
          observedAt: timestampFromDate(new Date()),
          board: { hashboardId: 101n, workerName: "rack-one", workerNameMode: "custom" },
        })}
      />,
    );
  }
}
async function openEditor() {
  fireEvent.click(screen.getByRole("button", { name: /Actions for/ }));
  const edit = await screen.findByRole("button", { name: "Edit pool" });
  await waitFor(() => expect(edit).toBeEnabled());
  fireEvent.click(edit);
  await screen.findByText("Log in to update your pool settings");
}

it.each(["module", "hashboard"] as const)(
  "requires a successful login before opening the %s pool editor",
  async (kind) => {
    mountAction(kind);
    await openEditor();
    expect(mocks.editor).not.toHaveBeenCalled();
    mocks.verify.mockRejectedValueOnce(new Error("invalid password"));
    fireEvent.change(screen.getByLabelText("Username"), { target: { value: "operator" } });
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "password" } });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByText("Invalid credentials entered.");
    expect(mocks.editor).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByRole("dialog", { name: "Edit pool" });
    expect(mocks.verify).toHaveBeenLastCalledWith({ username: "operator", password: "password" });
    expect(mocks.editor).toHaveBeenLastCalledWith(
      expect.objectContaining({
        boards: expect.objectContaining(kind === "module" ? { moduleIds: [11n] } : { hashboardIds: [101n] }),
      }),
      expect.any(Object),
    );
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await openEditor();
    expect(screen.getByLabelText("Username")).toHaveValue("");
    expect(screen.getByLabelText("Password")).toHaveValue("");
  },
);

it.each(["module", "hashboard"] as const)("canceling %s login does not open the editor", async (kind) => {
  mountAction(kind);
  await openEditor();
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByText("Log in to update your pool settings")).not.toBeInTheDocument());
  expect(mocks.editor).not.toHaveBeenCalled();
  expect(mocks.verify).not.toHaveBeenCalled();
});

it.each(["module", "hashboard"] as const)("disables %s Edit pool until activation completes", async (kind) => {
  mocks.groups.mockResolvedValue({ supported: true, enabled: false });
  mountAction(kind);
  fireEvent.click(screen.getByRole("button", { name: /Actions for/ }));
  await waitFor(() => expect(mocks.groups).toHaveBeenCalled());
  const edit = screen.getByRole("button", { name: "Edit pool" });
  expect(edit).toBeDisabled();
  await waitFor(() => expect(screen.queryByTestId("pool-readiness-spinner")).not.toBeInTheDocument());
  expect(screen.getByTestId("mining-pools-icon")).toBeInTheDocument();
  fireEvent.click(edit);
  expect(screen.queryByText("Log in to update your pool settings")).not.toBeInTheDocument();
  expect(mocks.editor).not.toHaveBeenCalled();
});

it.each(["module", "hashboard"] as const)("hides %s Edit pool on unsupported firmware", async (kind) => {
  mocks.groups.mockResolvedValue({ supported: false, enabled: false });
  mountAction(kind);
  fireEvent.click(screen.getByRole("button", { name: /Actions for/ }));
  await waitFor(() => expect(screen.queryByRole("button", { name: "Edit pool" })).not.toBeInTheDocument());
  expect(mocks.editor).not.toHaveBeenCalled();
});

it.each(["module", "hashboard"] as const)(
  "shows a spinner while checking %s pool readiness, then restores the pool icon",
  async (kind) => {
    let finish!: (value: GetStateResponse) => void;
    mocks.groups.mockReturnValue(
      new Promise((resolve) => {
        finish = resolve;
      }),
    );
    mountAction(kind);
    fireEvent.click(screen.getByRole("button", { name: /Actions for/ }));
    const edit = screen.getByRole("button", { name: "Edit pool" });
    expect(edit).toBeDisabled();
    expect(screen.getByTestId("pool-readiness-spinner")).toBeInTheDocument();
    expect(screen.queryByTestId("mining-pools-icon")).not.toBeInTheDocument();
    finish(
      create(GetStateResponseSchema, {
        supported: true,
        enabled: true,
        observedAt: timestampFromDate(new Date()),
        status: "complete",
      }),
    );
    await waitFor(() => expect(edit).toBeEnabled());
    expect(screen.queryByTestId("pool-readiness-spinner")).not.toBeInTheDocument();
    expect(screen.getByTestId("mining-pools-icon")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /Actions for/ }));
    mocks.groups.mockReturnValue(new Promise(() => {}));
    fireEvent.click(screen.getByRole("button", { name: /Actions for/ }));
    expect(screen.getByRole("button", { name: "Edit pool" })).toBeDisabled();
    expect(screen.getByTestId("pool-readiness-spinner")).toBeInTheDocument();
  },
);

it.each(["module", "hashboard"] as const)("stops the %s readiness spinner after a failed check", async (kind) => {
  mocks.groups.mockRejectedValue(new Error("controller unavailable"));
  mountAction(kind);
  fireEvent.click(screen.getByRole("button", { name: /Actions for/ }));
  await waitFor(() => expect(screen.queryByTestId("pool-readiness-spinner")).not.toBeInTheDocument());
  expect(screen.getByRole("button", { name: "Edit pool" })).toBeDisabled();
  expect(screen.getByTestId("mining-pools-icon")).toBeInTheDocument();
});

it.each(["module", "hashboard"] as const)("keeps %s Edit pool disabled during activation", async (kind) => {
  mocks.groups.mockResolvedValue({
    supported: true,
    enabled: true,
    status: "applying",
    observedAt: timestampFromDate(new Date()),
  });
  mountAction(kind);
  fireEvent.click(screen.getByRole("button", { name: /Actions for/ }));
  await waitFor(() => expect(mocks.groups).toHaveBeenCalled());
  expect(screen.getByRole("button", { name: "Edit pool" })).toBeDisabled();
});

it("preserves the selected hashboard and credentials across inventory changes", async () => {
  const slot = create(HashboardSlotSchema, {
    position: 1,
    occupancy: "occupied",
    observedAt: timestampFromDate(new Date()),
    board: { hashboardId: 101n, workerName: "rack-one", workerNameMode: "custom" },
  });
  const { rerender } = render(<HashboardPoolAction deviceIdentifier="ct1" slot={slot} />);
  await openEditor();
  fireEvent.change(screen.getByLabelText("Username"), { target: { value: "operator" } });
  fireEvent.change(screen.getByLabelText("Password"), { target: { value: "password" } });

  rerender(
    <HashboardPoolAction
      deviceIdentifier="ct1"
      slot={create(HashboardSlotSchema, { position: 1, occupancy: "unknown" })}
    />,
  );
  expect(screen.getByLabelText("Username")).toHaveValue("operator");
  expect(screen.getByLabelText("Password")).toHaveValue("password");
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  await screen.findByRole("dialog", { name: "Edit pool" });

  rerender(
    <HashboardPoolAction
      deviceIdentifier="ct1"
      slot={create(HashboardSlotSchema, {
        position: slot.position,
        occupancy: slot.occupancy,
        observedAt: slot.observedAt,
        board: { hashboardId: 102n },
      })}
    />,
  );
  expect(mocks.editor).toHaveBeenLastCalledWith(
    expect.objectContaining({
      boards: expect.objectContaining({ hashboardIds: [101n] }),
    }),
    expect.any(Object),
  );
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  await openEditor();
  expect(screen.getByLabelText("Username")).toHaveValue("");
});

it.each([
  { kind: "module", missing: "miner:start_mining" },
  { kind: "module", missing: "miner:stop_mining" },
  { kind: "hashboard", missing: "miner:start_mining" },
  { kind: "hashboard", missing: "miner:stop_mining" },
] as const)("hides $kind pool assignment without $missing", async ({ kind, missing }) => {
  mocks.permissions = mocks.permissions.filter((permission) => permission !== missing);
  mountAction(kind);
  const trigger = screen.queryByRole("button", { name: /Actions for/ });
  if (trigger) fireEvent.click(trigger);
  expect(screen.queryByRole("button", { name: "Edit pool" })).not.toBeInTheDocument();
  expect(mocks.editor).not.toHaveBeenCalled();
});

it.each(["module", "hashboard"] as const)("initializes worker names from the %s action", async (kind) => {
  mocks.permissions.push("miner:read", "miner:update_worker_names");
  mountAction(kind);
  fireEvent.click(screen.getByRole("button", { name: /Actions for/ }));
  const update = await screen.findByRole("button", {
    name: kind === "module" ? "Update worker names" : "Update worker name",
  });
  await waitFor(() => expect(update).toBeEnabled());
  fireEvent.click(update);
  fireEvent.change(screen.getByLabelText("Username"), { target: { value: "operator" } });
  fireEvent.change(screen.getByLabelText("Password"), { target: { value: "password" } });
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  await screen.findByRole("dialog", { name: "Update worker names" });
  const selection = screen.getByRole("button", { name: "Worker name" });
  if (kind === "module") {
    expect(selection).toHaveTextContent("Select worker name");
    expect(screen.getByRole("button", { name: "Review changes" })).toBeDisabled();
  } else {
    expect(selection).toHaveTextContent("Custom worker name");
    expect(screen.getByLabelText("Custom worker name")).toHaveValue("rack-one");
  }
});
