import { useState } from "react";
import { MemoryRouter } from "react-router-dom";
import { act, fireEvent, render, renderHook, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import StatusModal from "./StatusModal";
import { toCanonicalErrors as rigErrors } from "@/protoOS/api/adapters/mdkV1/normalize";
import { moduleFixture } from "@/protoOS/api/adapters/mdkV2/moduleFixtures";
import { toCanonicalSystemInfo } from "@/protoOS/api/adapters/mdkV2/normalize";
import ModuleModal from "@/protoOS/features/tanks/components/ModuleModal/ModuleModal";
import { type ContainerTopology, type MinerError, ROOT_MODULE_KEY } from "@/protoOS/store/contract";
import { useDetailedErrors } from "@/protoOS/store/hooks/useMinerStatus";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { getHashboardId } from "@/protoOS/store/utils/entityIds";
import { __resetEscapeStackForTests } from "@/shared/hooks/useEscapeDismiss";

vi.mock("@/protoOS/api/hooks/useTelemetry", () => ({ useTelemetry: () => ({}) }));
vi.mock("@/protoOS/api/hooks/useCoolingStatus", () => ({ useCoolingStatus: () => ({}) }));
vi.mock("@/protoOS/api/hooks/useErrors", () => ({ useErrors: () => ({}) }));
vi.mock("@/protoOS/api/hooks/useHashboardStatus", () => ({ useHashboardStatus: () => ({}) }));
const wakeMiner = vi.hoisted(() => vi.fn());
vi.mock("@/protoOS/hooks/useWakeMiner", () => ({ useWakeMiner: () => ({ wakeMiner }) }));
vi.mock("@/protoOS/hooks/status", () => ({ useMinerStatusTitle: () => ({ title: "Miner" }) }));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({ useMinerHosting: () => ({ minerRoot: "" }) }));

beforeEach(() => {
  wakeMiner.mockReset();
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
  const boardOpener = within(moduleDialog).getByRole("button", { name: "Board 1 info" });
  const psuOpener = within(moduleDialog).getByRole("button", { name: "More info" });
  boardOpener.focus();
  fireEvent.click(boardOpener);
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
  expect(boardOpener).toHaveFocus();
  expect(moduleDialog).toBeInTheDocument();
  psuOpener.focus();
  fireEvent.click(psuOpener);
  const psuDetails = screen.getByRole("dialog", { name: /PSU status.*Module 2/ });
  expect(psuDetails).toHaveTextContent("PSU-2");
  expect(psuDetails).toHaveTextContent("Selected PSU warning");
  act(() => useMinerStore.getState().minerStatus.setErrors([]));
  expect(psuDetails).not.toHaveTextContent("Selected PSU warning");
  fireEvent.click(within(psuDetails).getAllByRole("button", { name: "Done" })[0]);
  expect(psuOpener).toHaveFocus();
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
  expect(within(details).queryByText("Mining status")).not.toBeInTheDocument();
  expect(within(details).queryByText("Isolation")).not.toBeInTheDocument();
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

test("module summary keeps a single PSU fault clickable and dismiss returns to its refreshed scope", () => {
  const fixture = seed();
  const module = [...fixture.hardware.topology!.modules.values()][1];
  const psu = fixture.hardware.psus.get(module.psuIds[0])!;
  const outside = [...fixture.hardware.hashboards.values()][0];
  useMinerStore
    .getState()
    .minerStatus.setErrors([fault(psu.ref, "PSU temperature fault"), fault(outside.ref, "Outside fault")]);
  const onClose = vi.fn();
  render(<StatusModal scope={{ kind: "module", id: module.id }} onClose={onClose} />);
  const summary = screen.getByRole("dialog", { name: "Tank 1 · Module 2 status" });
  expect(summary).toHaveTextContent("1 component affected");
  expect(summary).not.toHaveTextContent("Outside fault");
  fireEvent.click(within(summary).getByRole("button", { name: /PSU temperature fault.*Module 2.*PSU 1/ }));
  expect(screen.getByRole("dialog", { name: /PSU status.*Module 2/ })).toHaveTextContent("PSU-2");
  act(() => useMinerStore.getState().minerStatus.setErrors([]));
  fireEvent.keyDown(document, { key: "Escape" });
  expect(onClose).not.toHaveBeenCalled();
  expect(screen.getByRole("dialog", { name: "Tank 1 · Module 2 status" })).toHaveTextContent(
    "All components operational",
  );
  expect(screen.queryByText("PSU temperature fault")).not.toBeInTheDocument();
});

test("container summary retains the wake action without offering container power in a tank summary", () => {
  const fixture = seed();
  const tank = [...(fixture.hardware.topology as ContainerTopology).tanks.values()][0];
  useMinerStore.setState((state) => {
    state.minerStatus.operationalByOwner.set(ROOT_MODULE_KEY, {
      owner: { kind: "container", id: (fixture.hardware.topology as ContainerTopology).containerId },
      resource: { status: "ready", updatedAtMs: 1 },
      miningStatus: "stopped",
    });
  });
  const onClose = vi.fn();
  const { rerender } = render(<StatusModal onClose={onClose} />);
  expect(screen.getByText("Container is asleep")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Wake miner" }));
  expect(onClose).toHaveBeenCalledOnce();
  expect(wakeMiner).toHaveBeenCalledOnce();
  rerender(<StatusModal scope={{ kind: "tank", id: tank.id }} onClose={onClose} />);
  expect(screen.queryByRole("button", { name: "Wake miner" })).not.toBeInTheDocument();
});

test("tank drill-down restores the summary scroll position and reopen starts at the summary", () => {
  const fixture = seed();
  const tank = [...(fixture.hardware.topology as ContainerTopology).tanks.values()][0];
  const board = [...fixture.hardware.hashboards.values()][0];
  useMinerStore.getState().minerStatus.setErrors([fault(board.ref, "Board fault")]);
  const { rerender } = render(<StatusModal scope={{ kind: "tank", id: tank.id }} open onClose={() => {}} />);
  const summary = screen.getByRole("dialog", { name: "Tank 1 status" });
  const scroller = within(summary).getByTestId("modal");
  fireEvent.scroll(scroller, { target: { scrollTop: 240 } });
  fireEvent.click(within(summary).getByRole("button", { name: /Board fault/ }));
  expect(scroller.scrollTop).toBe(0);
  fireEvent.click(screen.getByRole("button", { name: "Back to Tank 1" }));
  expect(scroller.scrollTop).toBe(240);
  fireEvent.click(screen.getByRole("button", { name: /Board fault/ }));
  rerender(<StatusModal scope={{ kind: "tank", id: tank.id }} open={false} onClose={() => {}} />);
  rerender(<StatusModal scope={{ kind: "tank", id: tank.id }} open onClose={() => {}} />);
  expect(screen.getByRole("dialog", { name: "Tank 1 status" })).toBeInTheDocument();
});

test("module health button opens its own scope and Done in details returns to it", () => {
  const fixture = seed();
  const topology = fixture.hardware.topology as ContainerTopology;
  const tank = [...topology.tanks.values()][0];
  const module = [...topology.modules.values()][1];
  const psu = fixture.hardware.psus.get(module.psuIds[0])!;
  useMinerStore.getState().minerStatus.setErrors([fault(psu.ref, "PSU fault")]);
  render(
    <MemoryRouter>
      <ModuleModal module={module} tank={tank} topology={topology} />
    </MemoryRouter>,
  );
  const opener = screen.getByRole("button", { name: "Tank 1 · Module 2 health: Needs attention" });
  opener.focus();
  fireEvent.click(opener);
  fireEvent.click(screen.getByRole("button", { name: /PSU fault.*Module 2/ }));
  const detail = screen.getByRole("dialog", { name: /PSU status/ });
  fireEvent.click(within(detail).getAllByRole("button", { name: "Done" })[0]);
  const summary = screen.getByRole("dialog", { name: "Tank 1 · Module 2 status" });
  expect(summary).toHaveTextContent("PSU fault");
  fireEvent.click(within(summary).getByRole("button", { name: "Close dialog" }));
  expect(opener).toHaveFocus();
  act(() => useMinerStore.getState().minerStatus.setErrors([]));
  expect(opener).toHaveAccessibleName("Tank 1 · Module 2 health: All components operational");
});

test("uses the container modal while container topology is still loading", () => {
  useMinerStore.getState().systemInfo.setSystemInfo(toCanonicalSystemInfo({ info: { id: "ct1", product: "CT1" } }));
  render(<StatusModal onClose={() => {}} />);
  expect(screen.getByRole("dialog", { name: "Container status" })).toBeInTheDocument();
  expect(screen.queryByRole("dialog", { name: "Miner status" })).not.toBeInTheDocument();
});

test.each([
  { observation: "Sleeping", miningStatus: "stopped", isolated: false, statusLabel: "Stopped" },
  { observation: "Sleeping", miningStatus: "curtailed", isolated: false, statusLabel: "Curtailed" },
  { observation: "Isolated", miningStatus: "stopped", isolated: true, statusLabel: "Stopped" },
] as const)(
  "retains $statusLabel / $observation details and updates them on recovery",
  ({ observation, miningStatus, isolated, statusLabel }) => {
    const fixture = seed();
    const module = [...fixture.hardware.topology!.modules.values()][0];
    const boardId = module.hashboardIds[0];
    useMinerStore.setState((state) => {
      Object.assign(state.telemetry.hashboards.get(boardId)!, { miningStatus, isolated });
    });
    render(<StatusModal scope={{ kind: "module", id: module.id }} onClose={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: new RegExp(`${observation}.*Board 1`) }));
    const details = screen.getByRole("dialog", { name: /Hashboard status/ });
    expect(within(details).getByText("Mining status").parentElement).toHaveTextContent(statusLabel);
    expect(within(details).getByText("Isolation").parentElement).toHaveTextContent(
      isolated ? "Isolated" : "Not isolated",
    );

    act(() =>
      useMinerStore.setState((state) => {
        Object.assign(state.telemetry.hashboards.get(boardId)!, { miningStatus: "mining", isolated: false });
      }),
    );
    expect(within(details).getByText("Mining status").parentElement).toHaveTextContent("Mining");
    expect(details).not.toHaveTextContent(statusLabel);
    expect(within(details).getByText("Isolation").parentElement).toHaveTextContent("Not isolated");
    fireEvent.click(within(details).getByRole("button", { name: /Back to/ }));
    expect(screen.getByRole("dialog", { name: "Tank 1 · Module 1 status" })).toHaveTextContent(
      "All components operational",
    );
  },
);

test("missing CT1 operational readings remain unknown in component details", () => {
  const fixture = seed();
  const board = [...fixture.hardware.hashboards.values()][0];
  useMinerStore.setState((state) => {
    state.telemetry.hashboards.delete(board.ref.id);
  });
  render(<StatusModal componentAddress={board.ref} onClose={() => {}} showBackButton={false} />);
  const details = screen.getByRole("dialog", { name: /Hashboard status/ });
  expect(within(details).getByText("Mining status").parentElement).toHaveTextContent("Unknown");
  expect(within(details).getByText("Isolation").parentElement).toHaveTextContent("Unknown");
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
