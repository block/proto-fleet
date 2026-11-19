import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";

import ModuleModal from "./ModuleModal";
import { moduleFixture } from "@/protoOS/api/adapters/mdkV2/moduleFixtures";
import { toCanonicalContainer, toCanonicalSystemInfo } from "@/protoOS/api/adapters/mdkV2/normalize";
import type { ContainerTopology } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

vi.mock("@/protoOS/api/hooks/useHashboardStatus", () => ({ useHashboardStatus: () => ({}) }));
const poolAssignment = vi.hoisted(() => ({ inspect: vi.fn(), inspectWorkerNames: vi.fn() }));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ minerRoot: "", adapter: { poolAssignment } }),
}));
vi.mock("./ModulePowerFeed", () => ({
  default: ({ module }: { module: { id: string } }) => <div data-testid="module-power-feed">{module.id}</div>,
}));
// Stands in for the voltage section so the test controls when a write is in flight.
vi.mock("./ModuleVoltageSection", () => ({
  default: ({ onWritingChange }: { onWritingChange: (writing: boolean) => void }) => (
    <>
      <button onClick={() => onWritingChange(true)}>start write</button>
      <button onClick={() => onWritingChange(false)}>finish write</button>
    </>
  ),
}));

beforeEach(() => {
  useMinerStore.getState().resetDeviceData();
});

const Path = () => <p data-testid="path">{useLocation().pathname}</p>;

test("keeps the module on screen until a voltage write settles", () => {
  const fixture = moduleFixture();
  const store = useMinerStore.getState();
  store.systemInfo.setSystemInfo(toCanonicalSystemInfo({ info: { id: "ct1-story", product: "CT1" } }));
  store.hardware.replaceHardware(fixture.hardware);
  const topology = fixture.hardware.topology as ContainerTopology;
  const tank = [...topology.tanks.values()][0];
  const module = [...topology.modules.values()][0];
  render(
    <MemoryRouter initialEntries={["/tanks/1/modules/1"]}>
      <Routes>
        <Route path="*" element={<ModuleModal module={module} tank={tank} topology={topology} />} />
      </Routes>
      <Path />
    </MemoryRouter>,
  );
  const exits = () => [
    screen.getByRole("button", { name: "Module 1" }),
    screen.getAllByRole("button", { name: "Done" })[0],
    ...screen.getAllByRole("button", { name: "View" }),
  ];
  exits().forEach((button) => expect(button).toBeEnabled());

  fireEvent.click(screen.getByRole("button", { name: "start write" }));
  exits().forEach((button) => expect(button).toBeDisabled());
  expect(screen.getByText(/^Changing the module voltage\./)).toHaveAttribute("role", "status");
  fireEvent.keyDown(document, { key: "Escape" });
  fireEvent.click(screen.getByRole("button", { name: "Close dialog" }));
  expect(screen.getByTestId("path")).toHaveTextContent("/tanks/1/modules/1");

  fireEvent.click(screen.getByRole("button", { name: "finish write" }));
  exits().forEach((button) => expect(button).toBeEnabled());
  fireEvent.keyDown(document, { key: "Escape" });
  expect(screen.getByTestId("path")).toHaveTextContent(/^\/tanks\/1$/);
});

test("mounts the module's power feed", () => {
  const { hardware } = moduleFixture();
  useMinerStore.getState().hardware.replaceHardware(hardware);
  const topology = hardware.topology as ContainerTopology;
  const tank = [...topology.tanks.values()][0];
  const module = [...topology.modules.values()][0];
  render(
    <MemoryRouter>
      <ModuleModal module={module} tank={tank} topology={topology} />
    </MemoryRouter>,
  );
  expect(screen.getByTestId("module-power-feed")).toHaveTextContent(module.id);
});

test("uses the action bar to select the installed boards and open either pool action", async () => {
  const boardOne = "aa:bb:cc:00:00:01";
  const boardThree = "aa:bb:cc:00:00:03";
  const installed = [boardOne, boardThree];
  const slot = (position: number, macAddress: string) => ({
    occupied: true,
    hashboard: { macAddress, location: { hashboardPosition: position } },
  });
  const { hardware } = toCanonicalContainer(
    {
      containerMap: {
        tanks: [
          {
            id: 1,
            modules: [
              { id: 1, slots: [slot(1, boardOne), { occupied: false }, slot(3, boardThree)] },
              { id: 2, slots: [slot(1, "aa:bb:cc:00:00:04")] },
            ],
          },
        ],
      },
    },
    {},
    {},
    "ct1-story",
  );
  const store = useMinerStore.getState();
  store.systemInfo.setSystemInfo(toCanonicalSystemInfo({ info: { id: "ct1-story", product: "CT1" } }));
  store.hardware.replaceHardware(hardware);
  const topology = hardware.topology as ContainerTopology;
  const tank = [...topology.tanks.values()][0];
  const module = [...topology.modules.values()][0];
  poolAssignment.inspect.mockResolvedValue({
    savedPools: [],
    mixedConfigurations: false,
    capacity: { groupsUsed: 1, maxGroups: 5, maxBoards: 9, maxPools: 3 },
  });
  poolAssignment.inspectWorkerNames.mockResolvedValue([]);
  render(
    <MemoryRouter>
      <ModuleModal module={module} tank={tank} topology={topology} />
    </MemoryRouter>,
  );
  expect(screen.queryByTestId("action-bar")).not.toBeInTheDocument();
  const first = screen.getByRole("checkbox", { name: "Select Board 1" });
  const third = screen.getByRole("checkbox", { name: "Select Board 3" });
  fireEvent.click(first);
  const bar = within(await screen.findByTestId("action-bar"));
  expect(bar.getByText("1 hashboard selected")).toBeInTheDocument();
  const selectAll = bar.getByRole("button", { name: "Select all" });
  fireEvent.click(selectAll);
  expect(selectAll).toBeInTheDocument();
  expect(bar.getByText("2 hashboards selected")).toBeInTheDocument();
  expect(first).toBeChecked();
  expect(third).toBeChecked();
  expect(screen.queryByRole("checkbox", { name: "Select Board 2" })).not.toBeInTheDocument();

  const menu = () => bar.getByRole("button", { name: "Actions for selected hashboards" });
  const target = { macs: installed, modules: [installed] };
  fireEvent.click(menu());
  const actions = within(await screen.findByTestId("selected-hashboard-actions-popover"));
  expect(actions.getByText("Update worker names")).toBeInTheDocument();
  fireEvent.click(actions.getByText("Edit pool"));
  await waitFor(() => expect(poolAssignment.inspect).toHaveBeenCalledWith(target, expect.anything()));
  const picker = await screen.findByRole("dialog", { name: "Edit pool" });
  fireEvent.click(within(picker).getByRole("button", { name: "Cancel" }));

  fireEvent.click(menu());
  fireEvent.click(
    within(await screen.findByTestId("selected-hashboard-actions-popover")).getByText("Update worker names"),
  );
  await waitFor(() => expect(poolAssignment.inspectWorkerNames).toHaveBeenCalledWith(target, expect.anything()));
  const editor = await screen.findByRole("dialog", { name: "Update worker names" });
  fireEvent.click(within(editor).getByRole("button", { name: "Close update worker names" }));
  fireEvent.click(bar.getByRole("button", { name: "Select none" }));
  expect(screen.queryByTestId("action-bar")).not.toBeInTheDocument();
  expect(first).not.toBeChecked();
  expect(third).not.toBeChecked();
});
