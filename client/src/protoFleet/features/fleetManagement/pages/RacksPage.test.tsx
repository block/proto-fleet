import { MemoryRouter, useLocation, useSearchParams } from "react-router-dom";
import { render, screen, waitFor } from "@testing-library/react";
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
} = vi.hoisted(() => ({
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
vi.mock("@/protoFleet/api/clients", () => ({ fleetInventoryClient: { listInventory } }));
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

const Location = () => <output data-testid="location">{useLocation().search}</output>;

beforeEach(() => {
  vi.clearAllMocks();
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
  await user.click(screen.getAllByRole("button", { name: "Clear Buildings filter" })[0]);
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
  await user.click(screen.getByText("Buildings", { exact: true, selector: "button *" }));
  await user.click(screen.getByText("CT1 North (Container)", { exact: true }));
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

it("combines colliding building and container IDs and clears both from one chip", async () => {
  const user = userEvent.setup();
  listAllBuildings.mockImplementation(({ onSuccess }) =>
    onSuccess([{ building: { id: 7n, name: "Conventional Building", siteId: 1n } }]),
  );
  render(
    <MemoryRouter initialEntries={["/fleet/racks?building=7&container=7&site=1&sort=name&display=list"]}>
      <RacksPage />
      <Location />
    </MemoryRouter>,
  );
  await waitFor(() =>
    expect(
      listInventory.mock.calls.some(
        ([request]) =>
          request.ancestors?.some(
            (ancestor: { kind: EntityKind; id: bigint }) => ancestor.kind === EntityKind.BUILDING && ancestor.id === 7n,
          ) &&
          request.ancestors?.some(
            (ancestor: { kind: EntityKind; id: bigint }) =>
              ancestor.kind === EntityKind.CONTAINER && ancestor.id === 7n,
          ),
      ),
    ).toBe(true),
  );
  await user.click(screen.getAllByRole("button", { name: "Clear Buildings filter" })[0]);
  await waitFor(() => expect(screen.getByTestId("location")).not.toHaveTextContent("container="));
  expect(screen.getByTestId("location")).not.toHaveTextContent("building=");
  expect(screen.getByTestId("location")).toHaveTextContent("site=1");
  expect(screen.getByTestId("location")).toHaveTextContent("sort=name");
});
