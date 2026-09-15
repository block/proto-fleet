import { useState } from "react";
import { MemoryRouter } from "react-router-dom";
import { act, fireEvent, render, renderHook, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import StatusModal from "./StatusModal";
import { toCanonicalErrors as rigErrors } from "@/protoOS/api/adapters/mdkV1/normalize";
import { moduleFixture } from "@/protoOS/api/adapters/mdkV2/moduleFixtures";
import { toCanonicalSystemInfo } from "@/protoOS/api/adapters/mdkV2/normalize";
import ModuleModal from "@/protoOS/features/tanks/components/ModuleModal/ModuleModal";
import type { ContainerTopology, MinerError } from "@/protoOS/store/contract";
import { useDetailedErrors } from "@/protoOS/store/hooks/useMinerStatus";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { getHashboardId } from "@/protoOS/store/utils/entityIds";
import { __resetEscapeStackForTests } from "@/shared/hooks/useEscapeDismiss";

vi.mock("@/protoOS/api/hooks/useTelemetry", () => ({ useTelemetry: () => ({}) }));
vi.mock("@/protoOS/api/hooks/useCoolingStatus", () => ({ useCoolingStatus: () => ({}) }));
vi.mock("@/protoOS/api/hooks/useErrors", () => ({ useErrors: () => ({}) }));
vi.mock("@/protoOS/api/hooks/useHashboardStatus", () => ({ useHashboardStatus: () => ({}) }));
vi.mock("@/protoOS/hooks/useWakeMiner", () => ({ useWakeMiner: () => ({}) }));
vi.mock("@/protoOS/hooks/status", () => ({ useMinerStatusTitle: () => ({ title: "Miner" }) }));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({ useMinerHosting: () => ({ minerRoot: "" }) }));

beforeEach(() => {
  useMinerStore.getState().resetDeviceData();
  __resetEscapeStackForTests();
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
  // jsdom has no layout; model visible controls for the focus trap.
  vi.spyOn(HTMLElement.prototype, "getClientRects").mockReturnValue([{}] as unknown as DOMRectList);
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

const seed = () => {
  const fixture = moduleFixture();
  const store = useMinerStore.getState();
  store.systemInfo.setSystemInfo(toCanonicalSystemInfo({ info: { id: "ct1-story", product: "CT1" } }));
  store.hardware.replaceHardware(fixture.hardware);
  store.telemetry.mergeTelemetry(fixture.telemetry);
  return fixture;
};
const fault = (component: MinerError["component"], message: string): MinerError => ({
  id: message,
  code: "fault",
  severity: "warning",
  owner: component!.owner,
  component,
  message,
});

test("module info opens the exact board and PSU, traps focus, and restores the module opener", () => {
  const fixture = seed();
  const topology = fixture.hardware.topology as ContainerTopology;
  const tank = [...topology.tanks.values()][0];
  const module = [...topology.modules.values()][1];
  const board = fixture.hardware.hashboards.get(module.hashboardIds[0])!;
  const psu = fixture.hardware.psus.get(module.psuIds[0])!;
  useMinerStore
    .getState()
    .minerStatus.setErrors([
      fault(board.ref, "Selected board warning"),
      fault(psu.ref, "Selected PSU warning"),
      fault([...fixture.hardware.hashboards.values()][0].ref, "Other module fault"),
    ]);
  render(
    <MemoryRouter>
      <ModuleModal module={module} tank={tank} topology={topology} />
    </MemoryRouter>,
  );
  const moduleDialog = screen.getByRole("dialog", { name: "Modules" });
  const openers = within(moduleDialog).getAllByRole("button", { name: "More info" });
  openers[0].focus();
  fireEvent.click(openers[0]);
  const details = screen.getByRole("dialog", { name: /Hashboard status.*Tank 1.*Module 2.*Board 1/ });
  expect(details).toHaveTextContent("T1-M2-HB1");
  expect(details).toHaveTextContent("Selected board warning");
  expect(details).not.toHaveTextContent("Other module fault");
  expect(details).not.toHaveTextContent("Selected PSU warning");
  expect(details.contains(document.activeElement)).toBe(true);
  const controls = within(details).getAllByRole("button");
  controls[controls.length - 1].focus();
  fireEvent.keyDown(document, { key: "Tab" });
  expect(controls[0]).toHaveFocus();
  fireEvent.keyDown(document, { key: "Escape" });
  expect(openers[0]).toHaveFocus();
  expect(moduleDialog).toBeInTheDocument();
  openers[3].focus();
  fireEvent.click(openers[3]);
  const psuDetails = screen.getByRole("dialog", { name: /PSU status.*Module 2/ });
  expect(psuDetails).toHaveTextContent("PSU-2");
  expect(psuDetails).toHaveTextContent("Selected PSU warning");
  act(() => useMinerStore.getState().minerStatus.setErrors([]));
  expect(psuDetails).not.toHaveTextContent("Selected PSU warning");
  fireEvent.click(within(psuDetails).getAllByRole("button", { name: "Done" })[0]);
  expect(openers[3]).toHaveFocus();
});

test("resolves rig error navigation to canonical IDs and preserves hardware and readings", () => {
  const store = useMinerStore.getState();
  store.hardware.setHashboards([{ serial: "RIG-HB-3", slot: 3, board: "R2" }]);
  store.minerStatus.setErrors(rigErrors([{ source: "hashboard", slot: 3, message: "Rig board warning" }]));
  useMinerStore.setState((state) => {
    const hashboardId = getHashboardId("RIG-HB-3");
    state.telemetry.hashboards.set(hashboardId, { hashboardId, hashrate: { latest: { value: 123.4, units: "TH/s" } } });
  });
  const Wrapper = () => {
    const [open, setOpen] = useState(true);
    return <StatusModal open={open} onClose={() => setOpen(false)} />;
  };
  render(<Wrapper />);
  fireEvent.click(screen.getByText("Rig board warning"));
  const details = screen.getByRole("dialog", { name: /Hashboard status.*Hashboard 3/ });
  expect(details).toHaveTextContent("RIG-HB-3");
  expect(details).toHaveTextContent("123.4 TH/S");
  expect(details).toHaveTextContent("Rig board warning");
  fireEvent.click(within(details).getByRole("button", { name: "Go back" }));
  expect(screen.getByRole("dialog", { name: "Miner status" })).toBeInTheDocument();
});

test("replaces all owners' active errors on refresh", () => {
  const fixture = seed();
  const boards = [...fixture.hardware.hashboards.values()];
  const { result } = renderHook(() => useDetailedErrors());
  act(() => useMinerStore.getState().minerStatus.setErrors([fault(boards[0].ref, "one"), fault(boards[3].ref, "two")]));
  expect(result.current.map((error) => error.message)).toEqual(["one", "two"]);
  act(() => useMinerStore.getState().minerStatus.setErrors([fault(boards[3].ref, "two")]));
  expect(result.current.map((error) => error.message)).toEqual(["two"]);
  act(() => useMinerStore.getState().minerStatus.setErrors([]));
  expect(result.current).toEqual([]);
});

test("identifies identical faults by component before navigating to the selected board", () => {
  const fixture = seed();
  const boards = [...fixture.hardware.hashboards.values()];
  useMinerStore.getState().minerStatus.setErrors([
    { ...fault(boards[0].ref, "Hashrail fault"), id: "module-1-fault" },
    { ...fault(boards[3].ref, "Hashrail fault"), id: "module-2-fault" },
  ]);
  render(<StatusModal onClose={() => {}} />);
  expect(screen.getByRole("button", { name: /Hashrail fault.*Tank 1.*Module 1.*Board 1/ })).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: /Hashrail fault.*Tank 1.*Module 2.*Board 1/ }));
  expect(screen.getByRole("dialog", { name: /Hashboard status.*Module 2.*Board 1/ })).toHaveTextContent("T1-M2-HB1");
});

test.each(["fault", "degraded"] as const)(
  "preserves %s health without error messages and clears it on recovery",
  (health) => {
    const fixture = seed();
    const board = [...fixture.hardware.hashboards.values()][0];
    useMinerStore.setState((state) => {
      state.telemetry.hashboards.set(board.ref.id, { hashboardId: board.ref.id, health });
    });
    render(<StatusModal componentAddress={board.ref} onClose={() => {}} showBackButton={false} />);
    const details = screen.getByRole("dialog", { name: /Hashboard status/ });
    const label = health === "fault" ? "Fault" : "Degraded";
    expect(details).toHaveTextContent(`Tank 1 · Module 1 · Board 1 · ${label}`);
    act(() =>
      useMinerStore.setState((state) => {
        state.telemetry.hashboards.set(board.ref.id, { hashboardId: board.ref.id, health: "ok" });
      }),
    );
    expect(details).not.toHaveTextContent(label);
  },
);

test("shows unknown CT1 health and unavailable readings without enabling commands", () => {
  const fixture = seed();
  const board = [...fixture.hardware.hashboards.values()][0];
  useMinerStore.setState((state) => {
    state.telemetry.hashboards.set(board.ref.id, { hashboardId: board.ref.id, health: "unknown" });
  });
  render(<StatusModal componentAddress={board.ref} onClose={() => {}} showBackButton={false} />);
  const details = screen.getByRole("dialog", { name: /Hashboard status/ });
  expect(details).toHaveTextContent("Status unknown");
  expect(within(details).getAllByText("N/A")).toHaveLength(5);
  expect(within(details).queryByRole("button", { name: /wake|reboot|locate/i })).not.toBeInTheDocument();
});
