import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test } from "vitest";

import Tanks from "./Tanks";
import {
  asContainerId,
  asDeviceId,
  asTankId,
  type CommandCapabilities,
  type HardwareState,
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

describe("Container tanks page", () => {
  beforeEach(() => {
    const containerId = asContainerId("container:ct1-01");
    const tankId = asTankId("tank:slotless");
    const tank: TankNode = {
      id: tankId,
      containerId,
      capabilities: commands,
      moduleIds: [],
      label: "Tank A",
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
        modules: new Map(),
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

  test("links slotless tanks by id instead of an unresolved positional fallback", () => {
    render(
      <MemoryRouter initialEntries={["/tanks"]}>
        <CurrentPath />
        <Routes>
          <Route path="/tanks" element={<Tanks />} />
        </Routes>
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "View Tank A" }));

    expect(screen.getByTestId("current-path")).toHaveTextContent("/tanks/tank%3Aslotless");
  });
});
