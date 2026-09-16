import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import Tanks from "./Tanks";
import { tanksFixture } from "./Tanks.fixtures";
import { CONTAINER_MODULE_LAYOUT } from "@/protoOS/features/diagnostic/hashboardLayout/layouts";
import { asEntityKey, asTankId } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

const mocks = vi.hoisted(() => ({
  minerRoot: "",
  error: undefined as string | undefined,
  startScopedMining: vi.fn(),
  stopScopedMining: vi.fn(),
  getCurrentTelemetry: vi.fn(),
  hashboardStatus: vi.fn(() => ({ error: undefined, pending: false })),
}));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ minerRoot: mocks.minerRoot, isFleetHosted: true, adapter: mocks }),
}));
vi.mock("@/protoOS/api/hooks/useHashboardStatus", () => ({ useHashboardStatus: mocks.hashboardStatus }));
vi.mock("@/protoOS/features/diagnostic/hashboardLayout", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useHashboardLayout: () => CONTAINER_MODULE_LAYOUT,
}));
vi.mock("@/protoOS/api/hooks/useTelemetry", () => ({ useTelemetry: () => ({ error: mocks.error }) }));

const CurrentPath = () => <span data-testid="current-path">{useLocation().pathname}</span>;
const showPage = (path = "/tanks") =>
  render(
    <MemoryRouter initialEntries={[path]}>
      <CurrentPath />
      <Routes>
        <Route path={`${mocks.minerRoot}/tanks/:tankId?`} element={<Tanks />} />
        <Route path={`${mocks.minerRoot}/tanks/:tankId/modules/:moduleId`} element={<Tanks />} />
        <Route
          path={`${mocks.minerRoot}/tanks/:tankId/modules/:moduleId/:serial`}
          element={<p>Hashboard diagnostics</p>}
        />
      </Routes>
    </MemoryRouter>,
  );

const scrollIntoView = vi.fn();
let fixture: ReturnType<typeof tanksFixture>;

describe("Container tanks page", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(0, 0, 100, 40));
    mocks.minerRoot = "";
    mocks.error = undefined;
    fixture = tanksFixture();
    mocks.getCurrentTelemetry.mockReset().mockResolvedValue(fixture.telemetry);
    mocks.startScopedMining.mockReset().mockResolvedValue(undefined);
    mocks.stopScopedMining.mockReset().mockResolvedValue(undefined);
    Element.prototype.scrollIntoView = scrollIntoView;
    useMinerStore.getState().resetDeviceData();
    useMinerStore.getState().ui.setTemperatureUnit("C");
    useMinerStore.getState().hardware.replaceHardware(fixture.hardware);
    useMinerStore.getState().telemetry.mergeTelemetry(fixture.telemetry);
  });

  test("renders each tank with its own metrics, health counts and missing sensor values", () => {
    showPage();
    const tank = within(screen.getByRole("region", { name: "Tank 1" }));
    expect(screen.getAllByRole("region")).toHaveLength(2);
    expect(tank.getByText("24,000 TH/s")).toBeInTheDocument();
    expect(tank.getByText("338.4 kW")).toBeInTheDocument();
    expect(tank.getByText("14.1 J/TH")).toBeInTheDocument();
    expect(tank.getAllByText("—")).toHaveLength(4);
    expect(tank.getByText("Healthy").parentElement).toHaveTextContent("14 modules");
    expect(tank.getByText("Needs attention").parentElement).toHaveTextContent("1 module");
    expect(tank.getByText("Offline").parentElement).toHaveTextContent("1 module");
    expect(tank.getAllByRole("button", { name: /^Module/ })).toHaveLength(16);
  });

  test("shares A readings across tanks 1–3 and B readings across tanks 4–6, with a common flow and inlet", () => {
    fixture = tanksFixture(6, 1, true);
    useMinerStore.getState().hardware.replaceHardware(fixture.hardware);
    useMinerStore.getState().telemetry.mergeTelemetry(fixture.telemetry);
    showPage();
    for (let slot = 1; slot <= 6; slot++) {
      const tank = within(screen.getByRole("region", { name: `Tank ${slot}` }));
      const side = slot <= 3 ? "Left" : "Right";
      expect(tank.getByText("Flow").parentElement).toHaveTextContent("1200.0 L/min");
      expect(tank.getByText(`${side} tank inlet temperature`).parentElement).toHaveTextContent("35.0 °C");
      expect(tank.getByText(`${side} tank outlet temperature`).parentElement).toHaveTextContent(
        slot <= 3 ? "48.0 °C" : "42.0 °C",
      );
      expect(tank.getByText("Fluid level").parentElement).toHaveTextContent(slot <= 3 ? "480.0 mm" : "460.0 mm");
    }

    act(() => useMinerStore.getState().ui.setTemperatureUnit("F"));
    expect(screen.getAllByText("95.0 °F")).toHaveLength(6);
    expect(screen.getAllByText("118.4 °F")).toHaveLength(3);
    expect(screen.getAllByText("107.6 °F")).toHaveLength(3);

    // The store must clear these readings along with the existing live metrics.
    act(() => useMinerStore.getState().telemetry.clearLatestData());
    for (let slot = 1; slot <= 6; slot++) {
      const tank = within(screen.getByRole("region", { name: `Tank ${slot}` }));
      expect(tank.getByText("Fluid level").parentElement).toHaveTextContent("—");
      expect(tank.getByText(`${slot <= 3 ? "Left" : "Right"} tank outlet temperature`).parentElement).toHaveTextContent(
        "—",
      );
    }
  });

  test("updates live plant readings, preserves zero depth, and clears unavailable sensors without borrowing the other side", () => {
    fixture = tanksFixture(6, 1, true);
    useMinerStore.getState().hardware.replaceHardware(fixture.hardware);
    useMinerStore.getState().telemetry.mergeTelemetry(fixture.telemetry);
    showPage();
    const [id, plant] = [...fixture.telemetry.plant.entries()][0];
    fixture.telemetry.plant.set(id, {
      ...plant,
      flowRate: { latest: { value: 0, units: "L/min" } },
      inletTemperature: { latest: { value: Number.NaN, units: "°C" } },
      outletATemperature: { latest: { value: null, units: "°C" } },
      tankALiquidLevel: { latest: { value: 0, units: "mm" } },
      tankBLiquidLevel: { latest: { value: Infinity, units: "mm" } },
    });
    act(() => useMinerStore.getState().telemetry.mergeTelemetry(fixture.telemetry));
    const left = within(screen.getByRole("region", { name: "Tank 1" }));
    const right = within(screen.getByRole("region", { name: "Tank 4" }));
    expect(left.getByText("Flow").parentElement).toHaveTextContent("0.0 L/min");
    expect(left.getByText("Fluid level").parentElement).toHaveTextContent("0.0 mm");
    expect(left.getByText("Left tank inlet temperature").parentElement).toHaveTextContent("—");
    expect(left.getByText("Left tank outlet temperature").parentElement).toHaveTextContent("—");
    expect(right.getByText("Right tank outlet temperature").parentElement).toHaveTextContent("42.0 °C");
    expect(right.getByText("Fluid level").parentElement).toHaveTextContent("—");
  });

  test.each([undefined, 0, 7, 1.5])("does not infer a side for an unknown tank slot %s", (slot) => {
    fixture = tanksFixture(1, 1, true);
    const tank = [...fixture.topology.tanks.values()][0];
    fixture.topology.tanks.set(tank.id, { ...tank, slot });
    useMinerStore.getState().hardware.replaceHardware(fixture.hardware);
    useMinerStore.getState().telemetry.mergeTelemetry(fixture.telemetry);
    showPage();
    expect(screen.getByText("Tank outlet temperature").parentElement).toHaveTextContent("—");
    expect(screen.getByText("Fluid level").parentElement).toHaveTextContent("—");
    expect(screen.getByText("Flow").parentElement).toHaveTextContent("1200.0 L/min");
  });

  test("shades individual hashboards by their issue counts and keeps unquantified faults red", () => {
    const module = [...fixture.topology.modules.values()].find((module) => module.location?.moduleSlot === 11)!;
    const counts = [1, 2, 4];
    module.hashboardIds.forEach((id, index) => {
      const board = fixture.telemetry.hashboards.get(id)!;
      fixture.telemetry.hashboards.set(id, { ...board, health: "fault", reportedIssueCount: counts[index] });
    });
    useMinerStore.getState().telemetry.mergeTelemetry(fixture.telemetry);
    showPage();
    const tile = screen.getAllByRole("button", { name: "Module 11, Needs attention" })[0];
    const bars = tile.querySelector('[aria-hidden="true"]')!.children;
    expect(bars[0]).toHaveClass("bg-intent-critical-fill/20");
    expect(bars[1]).toHaveClass("bg-intent-critical-fill/50");
    expect(bars[2]).toHaveClass("bg-intent-critical-fill");
    expect(tile).toHaveAccessibleDescription(
      "left hashboard: Needs attention, 1 reported issue. mid hashboard: Needs attention, 2 reported issues. right hashboard: Needs attention, 4 reported issues.",
    );

    module.hashboardIds.forEach((id, index) => {
      const board = fixture.telemetry.hashboards.get(id)!;
      fixture.telemetry.hashboards.set(id, { ...board, reportedIssueCount: index === 0 ? 0 : null });
    });
    act(() => useMinerStore.getState().telemetry.mergeTelemetry(fixture.telemetry));
    for (const bar of bars) expect(bar).toHaveClass("bg-intent-critical-fill");
  });

  test("deep links scroll to the requested tank without removing other tanks or re-scrolling on telemetry updates", () => {
    showPage("/tanks/2");
    expect(scrollIntoView).toHaveBeenCalledOnce();
    expect(scrollIntoView.mock.instances[0]).toBe(screen.getByRole("region", { name: "Tank 2" }));
    act(() => useMinerStore.getState().telemetry.mergeTelemetry(fixture.telemetry));
    expect(scrollIntoView).toHaveBeenCalledOnce();
    expect(screen.getAllByRole("region")).toHaveLength(2);
  });

  test("resolves a deep link when topology arrives after the initial render", () => {
    useMinerStore.getState().resetDeviceData();
    showPage("/tanks/2");
    expect(screen.getByText("Loading tanks…")).toBeInTheDocument();
    act(() => useMinerStore.getState().hardware.replaceHardware(fixture.hardware));
    expect(scrollIntoView).toHaveBeenCalledOnce();
  });

  test("keeps hosted and slotless tank links resolvable", () => {
    mocks.minerRoot = "/miners/ct1";
    fixture = tanksFixture();
    const first = [...fixture.topology.tanks.values()][0];
    fixture.topology.tanks.clear();
    const id = asTankId("tank:slotless");
    fixture.topology.tanks.set(id, { ...first, id, slot: undefined, label: "Tank A" });
    useMinerStore.getState().hardware.replaceHardware(fixture.hardware);
    showPage("/miners/ct1/tanks");
    fireEvent.click(screen.getByRole("link", { name: "View Tank A" }));
    expect(screen.getByTestId("current-path")).toHaveTextContent("/miners/ct1/tanks/tank%3Aslotless");
    expect(screen.queryByText(/Tank not found/)).not.toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Tank A power" })).toBeDisabled();
  });

  test("opens the module modal with a deep link and returns to its tank", async () => {
    showPage();
    fireEvent.click(screen.getAllByRole("button", { name: "Module 11, Needs attention" })[0]);
    expect(await screen.findByRole("link", { name: "View hashboard (mid)" })).toHaveAttribute(
      "href",
      "/tanks/1/modules/11/T1-M11-HB2",
    );
    fireEvent.click(screen.getByRole("button", { name: "View module" }));
    const dialog = screen.getByRole("dialog", { name: "Modules" });
    expect(dialog).toHaveAttribute("aria-modal", "true");
    expect(within(dialog).getByText("Module 11")).toBeInTheDocument();
    expect(screen.getByTestId("current-path")).toHaveTextContent("/tanks/1/modules/11");
    fireEvent.click(within(dialog).getByRole("button", { name: "Close dialog" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  test("deep links show three boards, scope ASIC polling, and switch modules", async () => {
    showPage("/tanks/1/modules/1");
    const dialogElement = screen.getByRole("dialog", { name: "Modules" });
    const dialog = within(dialogElement);
    expect(dialog.getAllByRole("button", { name: "View" })).toHaveLength(3);
    expect(dialog.getByText("Board 1")).toBeInTheDocument();
    expect(dialog.getAllByText("Efficiency")).toHaveLength(3);
    expect(mocks.hashboardStatus).toHaveBeenLastCalledWith({
      hashboardSerialNumbers: ["T1-M1-HB1", "T1-M1-HB2", "T1-M1-HB3"],
      poll: true,
    });
    fireEvent.click(dialog.getByRole("button", { name: "Module 1" }));
    fireEvent.click(await screen.findByRole("button", { name: "Module 2" }));
    expect(screen.getByTestId("current-path")).toHaveTextContent("/tanks/1/modules/2");
    expect(screen.getByRole("dialog", { name: "Modules" })).toBe(dialogElement);
    expect(dialog.getByRole("button", { name: "Module 2" })).toHaveFocus();
    expect(mocks.hashboardStatus).toHaveBeenLastCalledWith({
      hashboardSerialNumbers: ["T1-M2-HB1", "T1-M2-HB2", "T1-M2-HB3"],
      poll: true,
    });
    fireEvent.click(screen.getAllByRole("button", { name: "View" })[0]);
    expect(screen.getByTestId("current-path")).toHaveTextContent("/tanks/1/modules/2/T1-M2-HB1");
  });

  test("Done closes a directly opened modal and invalid module links show a status", () => {
    showPage("/tanks/1/modules/1");
    fireEvent.click(within(screen.getByRole("dialog")).getAllByRole("button", { name: "Done" })[0]);
    expect(screen.getByTestId("current-path")).toHaveTextContent(/^\/tanks\/1$/);
  });

  test("invalid modules do not open a modal or poll ASICs", () => {
    showPage("/tanks/1/modules/missing");
    expect(screen.getByRole("status")).toHaveTextContent("Module not found");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(mocks.hashboardStatus).not.toHaveBeenCalled();
  });

  test("toggles module menus with mouse clicks and dismisses them outside the grid", async () => {
    const user = userEvent.setup();
    showPage();
    const first = screen.getAllByRole("button", { name: "Module 11, Needs attention" })[0];
    const next = screen.getAllByRole("button", { name: "Module 12, Healthy" })[0];
    const otherTank = screen.getAllByRole("button", { name: "Module 11, Needs attention" })[1];

    await user.click(first);
    expect(first).toHaveAttribute("aria-expanded", "true");
    await user.click(first);
    expect(first).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByRole("button", { name: "View module" })).not.toBeInTheDocument();

    await user.click(first);
    await user.click(next);
    expect(first).toHaveAttribute("aria-expanded", "false");
    expect(next).toHaveAttribute("aria-expanded", "true");
    expect(screen.getAllByRole("button", { name: "View module" })).toHaveLength(1);

    await user.click(otherTank);
    expect(next).toHaveAttribute("aria-expanded", "false");
    expect(otherTank).toHaveAttribute("aria-expanded", "true");
    expect(screen.getAllByRole("button", { name: "View module" })).toHaveLength(1);

    await user.click(screen.getByRole("heading", { name: "Tanks" }));
    expect(otherTank).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByRole("button", { name: "View module" })).not.toBeInTheDocument();
  });

  test("hashboard navigation stays within the hosted miner", async () => {
    mocks.minerRoot = "/miners/ct1";
    showPage("/miners/ct1/tanks");
    fireEvent.click(screen.getAllByRole("button", { name: "Module 11, Needs attention" })[0]);
    fireEvent.click(await screen.findByRole("link", { name: "View hashboard (mid)" }));
    expect(screen.getByTestId("current-path")).toHaveTextContent("/miners/ct1/tanks/1/modules/11/T1-M11-HB2");
  });

  test("confirms power off and sends only this tank's explicit module scope", async () => {
    showPage();
    fireEvent.click(screen.getByRole("checkbox", { name: "Tank 2 power" }));
    expect(mocks.stopScopedMining).not.toHaveBeenCalled();
    expect(screen.getByText(/stop mining on 16 modules in Tank 2/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Turn off" }));
    await waitFor(() =>
      expect(mocks.stopScopedMining).toHaveBeenCalledExactlyOnceWith(
        { modules: Array.from({ length: 16 }, (_, index) => ({ tank: 2, module: index + 1 })) },
        {},
      ),
    );
    await waitFor(() => expect(mocks.getCurrentTelemetry).toHaveBeenCalledOnce());
  });

  test("starts stopped tanks and never sends a command on cancel", async () => {
    const key = asEntityKey([...fixture.topology.tanks.keys()][0]);
    fixture.telemetry.rollupsByEntity.set(key, { miningStatus: "stopped" });
    useMinerStore.getState().telemetry.mergeTelemetry(fixture.telemetry);
    showPage();
    fireEvent.click(screen.getByRole("checkbox", { name: "Tank 1 power" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(mocks.startScopedMining).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("checkbox", { name: "Tank 1 power" }));
    fireEvent.click(screen.getByRole("button", { name: "Turn on" }));
    await waitFor(() => expect(mocks.startScopedMining).toHaveBeenCalledOnce());
  });

  test("surfaces command errors and retains observed power state", async () => {
    mocks.stopScopedMining.mockRejectedValue(new Error("offline"));
    showPage();
    fireEvent.click(screen.getByRole("checkbox", { name: "Tank 1 power" }));
    fireEvent.click(screen.getByRole("button", { name: "Turn off" }));
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("Could not change tank power"));
    expect(screen.getByRole("checkbox", { name: "Tank 1 power" })).toBeChecked();
    expect(mocks.getCurrentTelemetry).not.toHaveBeenCalled();
  });

  test("does not resend a successful command when the status refresh fails", async () => {
    mocks.getCurrentTelemetry.mockRejectedValue(new Error("offline"));
    showPage();
    fireEvent.click(screen.getByRole("checkbox", { name: "Tank 1 power" }));
    fireEvent.click(screen.getByRole("button", { name: "Turn off" }));
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("Command sent"));
    expect(mocks.stopScopedMining).toHaveBeenCalledOnce();
  });

  test("drops a late command completion after the miner connection changes", async () => {
    let finish!: () => void;
    mocks.stopScopedMining.mockReturnValue(
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
    );
    showPage();
    fireEvent.click(screen.getByRole("checkbox", { name: "Tank 1 power" }));
    fireEvent.click(screen.getByRole("button", { name: "Turn off" }));
    expect(screen.getByRole("checkbox", { name: "Tank 1 power" })).toBeDisabled();
    act(() => useMinerStore.getState().resetDeviceData());
    await act(async () => finish());
    expect(mocks.getCurrentTelemetry).not.toHaveBeenCalled();
    expect(useMinerStore.getState().telemetry.rollupsByEntity.size).toBe(0);
  });

  test("shows invalid tank and telemetry error states while keeping the last readings", () => {
    mocks.error = "network unavailable";
    showPage("/tanks/missing");
    expect(screen.getByRole("status")).toHaveTextContent("Tank not found");
    expect(screen.getByRole("alert")).toHaveTextContent("out of date");
    expect(screen.getAllByRole("region")).toHaveLength(2);
  });

  test("shows empty tanks and disables controls without a safe scope", () => {
    const empty = tanksFixture(1, 0);
    useMinerStore.getState().hardware.replaceHardware(empty.hardware);
    showPage();
    expect(screen.getByText("No modules found.")).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Tank 1 power" })).toBeDisabled();
  });
});
