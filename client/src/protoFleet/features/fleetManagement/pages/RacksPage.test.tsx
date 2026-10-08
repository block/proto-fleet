import { MemoryRouter, useLocation, useSearchParams } from "react-router-dom";
import { render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";
import RacksPage from "./RacksPage";
import { DeviceSetSchema } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import {
  EntityKind,
  InventoryView,
  ListInventoryResponseSchema,
} from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

const {
  listInventory,
  listRacks,
  listRackZones,
  listSites,
  listAllBuildings,
  getDeviceSetStats,
  permissions,
  containers,
  tankOptions,
  setTankMining,
  updateToast,
} = vi.hoisted(() => ({
  updateToast: vi.fn(),
  tankOptions: vi.fn(),
  setTankMining: vi.fn(),
  listInventory: vi.fn(),
  listRacks: vi.fn(),
  listRackZones: vi.fn(),
  listSites: vi.fn(),
  listAllBuildings: vi.fn(),
  getDeviceSetStats: vi.fn(),
  permissions: new Set(["site:read", "fleet:read", "miner:read"]),
  containers: [
    { id: "7", label: "CT1 North" },
    { id: "8", label: "CT1 South" },
  ],
}));
vi.mock("@/shared/features/toaster", () => ({
  pushToast: () => 1,
  updateToast,
  STATUSES: { loading: "loading", success: "success", error: "error" },
}));
vi.mock("@/protoFleet/api/clients", () => ({
  fleetInventoryClient: { listInventory },
  containersClient: { getTankMiningOptions: tankOptions, setTankMining },
}));
vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => ({ listRacks, listRackZones, getDeviceSetStats }),
}));
vi.mock("@/protoFleet/api/sites", () => ({ useSites: () => ({ listSites }) }));
vi.mock("@/protoFleet/api/buildings", () => ({ useBuildings: () => ({ listAllBuildings }) }));
vi.mock("@/protoFleet/api/useContainerFilterOptions", () => ({ useContainerFilterOptions: () => containers }));
vi.mock("@/protoFleet/store", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/store")>()),
  useTemperatureUnit: () => "C",
  useHasPermission: (permission: string) => permissions.has(permission),
}));
vi.mock("@/protoFleet/features/fleetManagement/hooks/useCreateRack", () => ({ useCreateRack: () => ({}) }));
vi.mock("@/protoFleet/features/fleetManagement/hooks/useCreateRacks", () => ({ useCreateRacks: () => ({}) }));
vi.mock("@/shared/components/Popover/usePopoverPosition", () => ({
  default: () => ({ popoverRef: { current: null }, popoverStyle: {}, popoverAnimation: "" }),
}));

const Location = () => <output data-testid="location">{useLocation().search}</output>;

beforeEach(() => {
  vi.clearAllMocks();
  tankOptions.mockResolvedValue({ canStart: true, canStop: true, unavailableReason: "" });
  setTankMining.mockRejectedValue(new Error("Connection lost"));
  permissions.clear();
  ["site:read", "fleet:read", "miner:read"].forEach((permission) => permissions.add(permission));
  listRackZones.mockImplementation(({ onSuccess }) => onSuccess([]));
  listAllBuildings.mockImplementation(({ onSuccess }) => onSuccess([]));
  listSites.mockImplementation(({ onSuccess }) => onSuccess([]));
  listInventory.mockResolvedValue(
    create(ListInventoryResponseSchema, {
      items: [{ ref: { kind: EntityKind.TANK, id: 1n }, name: "Tank 1", facilityName: "CT1 North", assetCount: 2n }],
      totalCount: 1n,
    }),
  );
});

it("offers scoped tank mining in the bottom action bar and restores the bar after cancellation", async () => {
  const user = userEvent.setup();
  render(
    <MemoryRouter initialEntries={["/fleet/racks?display=list"]}>
      <RacksPage />
    </MemoryRouter>,
  );
  await user.click(await screen.findByRole("checkbox", { name: "Select tank:1" }));
  const bar = screen.getByTestId("action-bar");
  expect(bar).toHaveClass("fixed", "bottom-4");
  expect(bar.parentElement).toBe(document.body);
  expect(bar).toHaveTextContent("1 tank selected");
  await user.click(within(bar).getByRole("button", { name: "Actions for Tank 1" }));
  await waitFor(() => expect(screen.getByRole("button", { name: "Stop mining" })).toBeEnabled());
  await user.click(screen.getByRole("button", { name: "Stop mining" }));
  expect(bar).toHaveClass("invisible");
  expect(screen.getByTestId("tank-mining-dialog")).toHaveTextContent("All modules in this tank will stop hashing.");
  expect(setTankMining).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  expect(bar).not.toHaveClass("invisible");
  await user.click(within(bar).getByRole("button", { name: "Clear selection" }));
  expect(screen.queryByTestId("action-bar")).not.toBeInTheDocument();
  expect(screen.getByRole("checkbox", { name: "Select tank:1" })).not.toBeChecked();
});

it("uses one bar for mixed rack/tank selection and dispatches only the selected tanks", async () => {
  const user = userEvent.setup();
  listInventory.mockResolvedValue(
    create(ListInventoryResponseSchema, {
      items: [
        { ref: { kind: EntityKind.TANK, id: 1n }, name: "Tank 1" },
        { ref: { kind: EntityKind.TANK, id: 2n }, name: "Tank 2" },
        { ref: { kind: EntityKind.RACK, id: 1n }, details: { case: "rack", value: { id: 1n, label: "Rack 1" } } },
      ],
      totalCount: 3n,
    }),
  );
  render(
    <MemoryRouter initialEntries={["/fleet/racks?display=list"]}>
      <RacksPage />
    </MemoryRouter>,
  );
  await user.click(await screen.findByRole("checkbox", { name: "Select tank:1" }));
  const bar = screen.getByTestId("action-bar");
  await user.click(within(bar).getByRole("button", { name: "Select all visible" }));
  expect(screen.getAllByTestId("action-bar")).toHaveLength(1);
  expect(bar).toHaveTextContent("3 equipment groups selected");
  await user.click(within(bar).getByRole("button", { name: "Actions for 2 selected tanks" }));
  await waitFor(() => expect(screen.getByRole("button", { name: "Stop mining" })).toBeEnabled());
  await user.click(screen.getByRole("button", { name: "Stop mining" }));
  const dialog = screen.getByTestId("tank-mining-dialog");
  expect(dialog).toHaveTextContent("Stop mining on 2 tanks?");
  expect(dialog).not.toHaveTextContent("Rack 1");
  await user.click(within(dialog).getByRole("button", { name: "Stop mining" }));
  expect(screen.queryByTestId("tank-mining-dialog")).not.toBeInTheDocument();
  await waitFor(() =>
    expect(updateToast).toHaveBeenCalledWith(
      1,
      expect.objectContaining({ status: "error", message: expect.stringContaining("Connection lost") }),
    ),
  );
  expect(setTankMining).toHaveBeenCalledWith({ tankIds: [1n, 2n], mining: false }, expect.anything());
  expect(bar).not.toHaveClass("invisible");
  await user.click(within(bar).getByRole("button", { name: "Select none" }));
  expect(screen.queryByTestId("action-bar")).not.toBeInTheDocument();
});

const ApplyContainerScope = () => {
  const [, setSearchParams] = useSearchParams();
  return (
    <>
      <button onClick={() => setSearchParams("display=list&container=7")}>Apply container saved view</button>
      <button onClick={() => setSearchParams("display=list")}>Clear container scope</button>
    </>
  );
};

it("immediately clears conventional racks when applying container scope and restores them when clearing it", async () => {
  permissions.delete("fleet:read");
  const user = userEvent.setup();
  const rack = create(DeviceSetSchema, { id: 1n, label: "Conventional Rack" });
  listRacks.mockImplementation(async ({ onSuccess, onFinally }) => {
    onSuccess([rack], undefined, 1);
    onFinally?.();
  });
  getDeviceSetStats.mockImplementation(({ onSuccess }) => onSuccess([]));
  render(
    <MemoryRouter initialEntries={["/fleet/racks?display=list"]}>
      <RacksPage />
      <ApplyContainerScope />
      <Location />
    </MemoryRouter>,
  );

  expect(await screen.findByText("Conventional Rack")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Apply container saved view" }));
  await waitFor(() => expect(screen.queryByText("Conventional Rack")).not.toBeInTheDocument());
  expect(screen.getByText("No results")).toBeInTheDocument();
  expect(screen.getByTestId("location")).toHaveTextContent("container=7");

  await user.click(screen.getByRole("button", { name: "Clear container scope" }));
  expect(await screen.findByText("Conventional Rack")).toBeInTheDocument();
  expect(screen.getByTestId("location")).not.toHaveTextContent("container=");
  expect(listInventory).not.toHaveBeenCalled();
});

it("restores multiple containers, composes site scope, and keeps selections when changing display", async () => {
  const user = userEvent.setup();
  render(
    <MemoryRouter initialEntries={["/fleet/racks?container=7&container=8&site=1&sort=name&display=list"]}>
      <RacksPage />
      <Location />
    </MemoryRouter>,
  );
  await waitFor(() => expect(listInventory).toHaveBeenCalled());
  expect(listInventory.mock.calls[0][0]).toMatchObject({
    view: InventoryView.EQUIPMENT_GROUPS,
    ancestors: [
      { kind: EntityKind.CONTAINER, id: 7n },
      { kind: EntityKind.CONTAINER, id: 8n },
    ],
    siteIds: [1n],
    sort: 1,
  });
  const callsBeforeDisplayChange = listInventory.mock.calls.length;
  await user.click(screen.getAllByRole("button", { name: "View grid" })[0]);
  expect(listInventory).toHaveBeenCalledTimes(callsBeforeDisplayChange);
  expect(screen.getByTestId("location")).toHaveTextContent("container=7&container=8");
  await user.click(screen.getAllByRole("button", { name: "Clear Containers filter" })[0]);
  await waitFor(() => expect(screen.getByTestId("location")).not.toHaveTextContent("container="));
  expect(screen.getByTestId("location")).toHaveTextContent("site=1");
  expect(screen.getByTestId("location")).toHaveTextContent("sort=name");
});

it("selects a container from the filter menu and sends its typed ancestor", async () => {
  const user = userEvent.setup();
  render(
    <MemoryRouter initialEntries={["/fleet/racks?site=1&display=grid"]}>
      <RacksPage />
      <Location />
    </MemoryRouter>,
  );
  await waitFor(() => expect(listInventory).toHaveBeenCalled());
  await user.click(screen.getAllByRole("button", { name: "Add Filter" })[0]);
  await user.click(screen.getByText("Containers", { exact: true, selector: "button *" }));
  await user.click(screen.getByText("CT1 North", { exact: true }));
  await waitFor(() => expect(screen.getByTestId("location")).toHaveTextContent("container=7"));
  expect(screen.getByTestId("location")).toHaveTextContent("site=1");
  await waitFor(() =>
    expect(
      listInventory.mock.calls.some(
        ([request]) => request.ancestors?.[0]?.kind === EntityKind.CONTAINER && request.ancestors[0].id === 7n,
      ),
    ).toBe(true),
  );
});
