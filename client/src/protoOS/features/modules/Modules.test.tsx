import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test } from "vitest";

import Modules from "./Modules";
import {
  asContainerId,
  asDeviceId,
  asHashboardId,
  asModuleId,
  asTankId,
  type CommandCapabilities,
  type HardwareState,
  type ModuleNode,
  type TankNode,
} from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

const CurrentPath = () => {
  const location = useLocation();
  return <span data-testid="current-path">{location.pathname}</span>;
};

const commands: CommandCapabilities = {
  startStop: "unknown",
  powerTarget: "unknown",
  locate: "unknown",
  reboot: "unknown",
  cooling: "unknown",
  logs: "unknown",
  firmwareUpdate: "unknown",
  poolEditing: "unknown",
  passwordManagement: "unknown",
};

describe("Container modules page", () => {
  beforeEach(() => {
    const containerId = asContainerId("container:ct1-01");
    const tankId = asTankId("tank:1");
    const moduleId = asModuleId("module:1");
    const hashboardId = asHashboardId("serial:HB-1");
    const tank: TankNode = {
      id: tankId,
      containerId,
      capabilities: commands,
      moduleIds: [moduleId],
      label: "Tank 1",
      slot: 1,
    };
    const module: ModuleNode = {
      id: moduleId,
      profile: "ct1",
      capabilities: { commands },
      tankId,
      hashboardIds: [hashboardId],
      controlBoardIds: [],
      psuIds: [],
      fanIds: [],
      location: { tankLabel: "Tank 1", moduleSlot: 1 },
    };
    const hardware: HardwareState = {
      resource: { status: "ready", updatedAtMs: 1_000 },
      topology: {
        kind: "container",
        deviceId: asDeviceId("device:ct1-01"),
        containerId,
        container: {
          id: containerId,
          capabilities: commands,
          tankIds: [tankId],
          controlBoardIds: [],
          fanIds: [],
          plantComponentIds: [],
        },
        tanks: new Map([[tankId, tank]]),
        modules: new Map([[moduleId, module]]),
        hashboards: new Map(),
      },
      controlBoards: new Map(),
      hashboards: new Map(),
      asics: new Map(),
      psus: new Map(),
      fans: new Map(),
      plant: new Map(),
    };

    useMinerStore.getState().resetDeviceData();
    useMinerStore.getState().hardware.replaceHardware(hardware);
  });

  test("drills from a tank module to the module route", () => {
    render(
      <MemoryRouter initialEntries={["/tanks/1"]}>
        <CurrentPath />
        <Routes>
          <Route path="/tanks/:tankId" element={<Modules />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(screen.getByRole("heading", { name: "Tank 1 modules" })).toBeInTheDocument();
    expect(screen.getByText("Module 1")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "View Module 1" }));

    expect(screen.getByTestId("current-path")).toHaveTextContent("/tanks/1/modules/1");
  });

  test("does not show all modules for an unknown tank route", () => {
    render(
      <MemoryRouter initialEntries={["/tanks/999"]}>
        <Routes>
          <Route path="/tanks/:tankId" element={<Modules />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(screen.getByRole("heading", { name: "Tank modules" })).toBeInTheDocument();
    expect(screen.getByText("No modules found.")).toBeInTheDocument();
    expect(screen.queryByText("Module 1")).not.toBeInTheDocument();
  });
});
