import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import ModuleActionsMenu from "./ModuleActionsMenu";
import { EntityKind, InventoryItemSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

const mocks = vi.hoisted(() => ({
  options: vi.fn(),
  submit: vi.fn(),
  results: vi.fn(),
  toast: vi.fn(),
  updateToast: vi.fn(),
}));
vi.mock("@/protoFleet/api/clients", () => ({
  containersClient: {
    getModuleMiningOptions: mocks.options,
    setModuleMining: mocks.submit,
    getEquipmentCommandResult: mocks.results,
  },
}));
vi.mock("@/shared/features/toaster", () => ({
  pushToast: mocks.toast,
  updateToast: mocks.updateToast,
  STATUSES: { loading: "loading", success: "success", error: "error" },
}));
vi.mock("@/shared/components/Popover/usePopoverPosition", () => ({
  default: () => ({ popoverRef: { current: null }, popoverStyle: {}, popoverAnimation: "" }),
}));
const modules = [
  create(InventoryItemSchema, { ref: { kind: EntityKind.MODULE, id: 11n }, name: "North T1 M2" }),
  create(InventoryItemSchema, { ref: { kind: EntityKind.MODULE, id: 12n }, name: "South T2 M2" }),
];
beforeEach(() => {
  vi.clearAllMocks();
  mocks.toast.mockReturnValue(1);
  mocks.options.mockResolvedValue({ canStart: true, canStop: true, unavailableReason: "" });
  mocks.submit.mockResolvedValue({
    commands: [
      { deviceIdentifier: "north", moduleIds: [11n], batchIdentifier: "a" },
      { deviceIdentifier: "south", moduleIds: [12n], batchIdentifier: "b" },
    ],
  });
  mocks.results.mockImplementation(async ({ deviceIdentifier }) => ({
    result: {
      status: "finished",
      successCount: deviceIdentifier === "north" ? 1 : 0,
      failureCount: deviceIdentifier === "south" ? 1 : 0,
    },
  }));
});
it("confirms named modules and preserves exact selection and known successes across controllers", async () => {
  const refresh = vi.fn();
  render(<ModuleActionsMenu modules={modules} onRefresh={refresh} />);
  fireEvent.click(screen.getByRole("button", { name: "Actions for 2 selected modules" }));
  const stop = await screen.findByRole("button", { name: "Stop mining" });
  await waitFor(() => expect(stop).toBeEnabled());
  fireEvent.click(stop);
  const dialog = screen.getByTestId("module-mining-dialog");
  expect(dialog).toHaveTextContent("North T1 M2");
  expect(dialog).toHaveTextContent("South T2 M2");
  expect(mocks.submit).not.toHaveBeenCalled();
  fireEvent.click(within(dialog).getByRole("button", { name: "Stop mining" }));
  await waitFor(() => expect(refresh).toHaveBeenCalledOnce());
  expect(mocks.submit).toHaveBeenCalledExactlyOnceWith({ moduleIds: [11n, 12n], mining: false }, expect.anything());
  expect(mocks.updateToast).toHaveBeenLastCalledWith(
    1,
    expect.objectContaining({
      status: "error",
      message: expect.stringContaining("Stopped mining on 1 out of 2 modules"),
    }),
  );
});
it("shows unsupported or stale scope without offering a container operation", async () => {
  mocks.options.mockResolvedValue({
    canStart: false,
    canStop: false,
    unavailableReason: "Module identity unavailable",
  });
  render(<ModuleActionsMenu modules={[modules[0]]} onRefresh={vi.fn()} />);
  fireEvent.click(screen.getByRole("button", { name: "Actions for North T1 M2" }));
  await screen.findByRole("button", { name: "Module identity unavailable" });
  expect(screen.getByRole("button", { name: "Start mining" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Stop mining" })).toBeDisabled();
  expect(screen.queryByRole("button", { name: "Reboot" })).not.toBeInTheDocument();
  expect(mocks.submit).not.toHaveBeenCalled();
});
it("hides mining operations that are not authorized", async () => {
  mocks.options.mockResolvedValue({ canStart: true, canStop: false, unavailableReason: "" });
  render(<ModuleActionsMenu modules={[modules[0]]} onRefresh={vi.fn()} />);
  fireEvent.click(screen.getByRole("button", { name: "Actions for North T1 M2" }));
  await waitFor(() => expect(screen.getByRole("button", { name: "Start mining" })).toBeEnabled());
  expect(screen.queryByRole("button", { name: "Stop mining" })).not.toBeInTheDocument();
});
