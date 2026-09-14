import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import Tanks from "./Tanks";
import { tanksFixture } from "./Tanks.fixtures";
import { asEntityKey, asTankId } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

const mocks = vi.hoisted(() => ({
  minerRoot: "",
  error: undefined as string | undefined,
  startScopedMining: vi.fn(),
  stopScopedMining: vi.fn(),
  getCurrentTelemetry: vi.fn(),
}));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ minerRoot: mocks.minerRoot, isFleetHosted: true, adapter: mocks }),
}));
vi.mock("@/protoOS/api/hooks/useTelemetry", () => ({ useTelemetry: () => ({ error: mocks.error }) }));

const CurrentPath = () => <span data-testid="current-path">{useLocation().pathname}</span>;
const showPage = (path = "/tanks") =>
  render(
    <MemoryRouter initialEntries={[path]}>
      <CurrentPath />
      <Routes>
        <Route path={`${mocks.minerRoot}/tanks/:tankId?`} element={<Tanks />} />
        <Route path={`${mocks.minerRoot}/diagnostics/:serial`} element={<p>Hashboard diagnostics</p>} />
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
    useMinerStore.getState().hardware.replaceHardware(fixture.hardware);
    useMinerStore.getState().telemetry.mergeTelemetry(fixture.telemetry);
  });

  test("renders each tank with its own metrics, health counts and missing sensor values", () => {
    showPage();
    const tank = within(screen.getByRole("region", { name: "Tank 1" }));
    expect(screen.getAllByRole("region")).toHaveLength(2);
    expect(tank.getByText("24,000 TH/s")).toBeInTheDocument();
    expect(tank.getByText("338.4 kW")).toBeInTheDocument();
    expect(tank.getByText("Hashboard power only; excludes cooling and other plant loads.")).toBeVisible();
    expect(tank.getByText("14.1 J/TH")).toBeInTheDocument();
    expect(tank.getAllByText("—")).toHaveLength(4);
    expect(tank.getByText("Healthy").parentElement).toHaveTextContent("14 modules");
    expect(tank.getByText("Needs attention").parentElement).toHaveTextContent("1 module");
    expect(tank.getByText("Offline").parentElement).toHaveTextContent("1 module");
    expect(tank.getAllByRole("button", { name: /^Module/ })).toHaveLength(16);
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

  test("opens a module modal from the popover without navigating away", async () => {
    showPage();
    fireEvent.click(screen.getAllByRole("button", { name: "Module 11, Needs attention" })[0]);
    expect(await screen.findByRole("link", { name: "View hashboard (mid)" })).toHaveAttribute(
      "href",
      "/diagnostics/T1-M11-HB2",
    );
    fireEvent.click(screen.getByRole("button", { name: "View module" }));
    const dialog = screen.getByRole("dialog", { name: "Module 11" });
    expect(dialog).toHaveAttribute("aria-modal", "true");
    expect(within(dialog).getByText("Module 11")).toBeInTheDocument();
    expect(screen.getByTestId("current-path")).toHaveTextContent("/tanks");
    fireEvent.click(within(dialog).getByRole("button", { name: "Close dialog" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
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
    expect(screen.getByTestId("current-path")).toHaveTextContent("/miners/ct1/diagnostics/T1-M11-HB2");
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
