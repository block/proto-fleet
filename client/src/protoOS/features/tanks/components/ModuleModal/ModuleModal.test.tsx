import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";

import ModuleModal from "./ModuleModal";
import { moduleFixture } from "@/protoOS/api/adapters/mdkV2/moduleFixtures";
import { toCanonicalSystemInfo } from "@/protoOS/api/adapters/mdkV2/normalize";
import type { ContainerTopology } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { __resetEscapeStackForTests } from "@/shared/hooks/useEscapeDismiss";

vi.mock("@/protoOS/api/hooks/useHashboardStatus", () => ({ useHashboardStatus: () => ({}) }));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({ useMinerHosting: () => ({ minerRoot: "" }) }));
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
  __resetEscapeStackForTests();
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
