import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import ContainerDiagnosticView from "./ContainerDiagnosticView";
import type { MinerAdapter } from "@/protoOS/api/adapters/types";
import { useTelemetry } from "@/protoOS/api/hooks/useTelemetry";
import { tanksFixture } from "@/protoOS/features/tanks/Tanks.fixtures";
import { asComponentId, asEntityKey, type PlantTelemetry } from "@/protoOS/store/contract";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { createEmptyTelemetryState } from "@/protoOS/store/utils/createEmptyTelemetryState";

const hosting = vi.hoisted(() => ({ adapter: null as MinerAdapter | null, isFleetHosted: true }));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({ useMinerHosting: () => hosting }));
vi.mock("@/protoOS/api/hooks/useTelemetry", () => ({ useTelemetry: vi.fn() }));
vi.mock("@/shared/features/toaster", () => ({ pushToast: vi.fn(() => 1), updateToast: vi.fn() }));

const deferred = <T,>() => {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
};

const telemetryWithTarget = (value: number) => {
  const telemetry = createEmptyTelemetryState();
  telemetry.plant.set(plantId, {
    componentId: plantId,
    setpoints: [{ name: "dry_cooler_auto_temp_c", value, unit: "°C" }],
  });
  return telemetry;
};

const openSettingsAndSave = () => {
  render(<ContainerDiagnosticView />);
  fireEvent.click(screen.getByRole("button", { name: "Manage" }));
  fireEvent.click(screen.getAllByRole("button", { name: "Save" })[0]);
};

const plantId = asComponentId("container:ct1:plant");
const fanId = asComponentId("container:ct1:fan:dry-cooler-fan-1");
const pduId = asComponentId("container:ct1:distribution:pdu-1");

describe("Container diagnostics", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    hosting.adapter = null;
    useMinerStore.getState().ui.setTemperatureUnit("C");
    const fixture = tanksFixture(6);
    const owner = { kind: "container" as const, id: fixture.topology.containerId };
    fixture.hardware.plant.set(plantId, { ref: { id: plantId, kind: "plant", owner }, kind: "plant", name: "Plant" });
    fixture.hardware.plant.set(pduId, {
      ref: { id: pduId, kind: "plant", owner },
      kind: "distribution",
      name: "PDU 1",
    });
    fixture.hardware.fans.set(fanId, { ref: { id: fanId, kind: "fan", owner }, name: "dry-cooler-fan-1" });
    fixture.topology.container.plantComponentIds.push(plantId, pduId);
    fixture.topology.container.fanIds.push(fanId);
    fixture.telemetry.rollupsByEntity.set(asEntityKey(fixture.topology.containerId), {
      miningStatus: "mining",
      power: { latest: { value: 124_800, units: "W" } },
    });
    fixture.topology.tanks.forEach((tank) => {
      const key = asEntityKey(tank.id);
      fixture.telemetry.rollupsByEntity.set(key, {
        ...fixture.telemetry.rollupsByEntity.get(key),
        boardCount: 48,
        onlineBoardCount: 48,
        temperature: { latest: { value: 60, units: "°C" } },
      });
    });
    fixture.telemetry.plant.set(plantId, {
      componentId: plantId,
      powered: true,
      heatRejectionActive: true,
      totalPower: { latest: { value: 124_800, units: "W" } },
      ambientTemperature: { latest: { value: 25.5, units: "°C" } },
      inletTemperature: { latest: { value: 33.3, units: "°C" } },
      outletTemperature: { latest: { value: 43.3, units: "°C" } },
      outletATemperature: { latest: { value: 43.1, units: "°C" } },
      outletBTemperature: { latest: { value: 43.5, units: "°C" } },
      flowRate: { latest: { value: 161.3, units: "L/min" } },
      coolantPumpSpeed: { latest: { value: 50, units: "Hz" } },
      pumpInletPressure: { latest: { value: 194_430, units: "Pa" } },
      pumpOutletPressure: { latest: { value: 293_710, units: "Pa" } },
      heatRejectionInletPressure: { latest: { value: 194_430, units: "Pa" } },
      heatRejectionOutletPressure: { latest: { value: 293_710, units: "Pa" } },
      tankALiquidLevel: { latest: { value: 860, units: "mm" } },
      tankBLiquidLevel: { latest: { value: 870, units: "mm" } },
      alarmActive: false,
      controls: [
        { name: "dry_cooler_auto", enabled: true, stateKnown: true },
        { name: "coolant_pump", enabled: true, stateKnown: true },
      ],
      setpoints: [{ name: "dry_cooler_auto_temp_c", value: 18, unit: "°C" }],
    });
    fixture.telemetry.plant.set(pduId, { componentId: pduId, powered: true, health: "ok" });
    fixture.telemetry.fans.set(fanId, {
      componentId: fanId,
      speedPercent: { latest: { value: 85, units: "%" } },
      health: "ok",
    });

    useMinerStore.getState().resetDeviceData();
    useMinerStore.getState().hardware.replaceHardware(fixture.hardware);
    useMinerStore.getState().telemetry.mergeTelemetry(fixture.telemetry);
    vi.mocked(useTelemetry).mockReturnValue({
      pending: false,
      error: undefined,
      data: useMinerStore.getState().telemetry,
    });
  });

  afterEach(() => {
    useMinerStore.getState().ui.setTemperatureUnit("C");
  });

  test.each<{ reading: Partial<PlantTelemetry> | undefined; expected: string }>([
    { reading: { activeFeedCount: 0, health: "ok" }, expected: "Offline" },
    { reading: { activeFeedCount: 2, health: "ok" }, expected: "Online" },
    { reading: { activeFeedCount: 0, health: "fault" }, expected: "Fault" },
    { reading: { activeFeedCount: 2, health: "offline" }, expected: "Offline" },
    { reading: { powered: false, activeFeedCount: 2 }, expected: "Offline" },
    { reading: { powered: true }, expected: "Online" },
    { reading: { health: "ok" }, expected: "—" },
    { reading: {}, expected: "—" },
    { reading: undefined, expected: "—" },
  ])("shows PDU status $expected for $reading", ({ reading, expected }) => {
    useMinerStore.getState().telemetry.clearAllData();
    if (reading) {
      const telemetry = createEmptyTelemetryState();
      telemetry.plant.set(pduId, { componentId: pduId, ...reading });
      useMinerStore.getState().telemetry.mergeTelemetry(telemetry);
    }
    render(<ContainerDiagnosticView />);
    fireEvent.mouseDown(screen.getByRole("button", { name: "Circuit breakers" }));
    expect(screen.getByText("PDU 1").nextElementSibling).toHaveTextContent(expected);
  });

  test.each([
    ["C", "60.0°C"],
    ["F", "140.0°F"],
  ] as const)("uses %s for tank card temperatures", (unit, expected) => {
    useMinerStore.getState().ui.setTemperatureUnit(unit);
    render(<ContainerDiagnosticView />);
    expect(within(screen.getAllByRole("article")[0]).getByText(expected)).toBeInTheDocument();
  });

  test.each([
    [true, "Active"],
    [false, "Inactive"],
    [undefined, "—"],
  ] as const)("shows alarm activity %s without inventing arming state", (alarmActive, expected) => {
    const telemetry = createEmptyTelemetryState();
    telemetry.plant.set(plantId, { componentId: plantId, alarmActive });
    useMinerStore.getState().telemetry.mergeTelemetry(telemetry);
    render(<ContainerDiagnosticView />);
    fireEvent.mouseDown(screen.getByRole("button", { name: "Controls" }));
    expect(screen.getByText("Alarm").parentElement).toHaveTextContent(expected);
    expect(screen.queryByText("Armed")).not.toBeInTheDocument();
    expect(screen.queryByRole("switch", { name: "Alarm" })).not.toBeInTheDocument();
  });

  test.each<Partial<PlantTelemetry>>([
    { controls: [] },
    { controls: [{ name: "another_control", enabled: true, stateKnown: true }] },
    { controls: [{ name: "dry_cooler_auto", enabled: true, stateKnown: false }] },
    { controls: [{ name: "dry_cooler_auto", stateKnown: true }] },
    { setpoints: [] },
    { setpoints: [{ name: "another_setpoint", value: 18 }] },
  ])("disables cooler management for unsupported or unknown settings: %j", (reading) => {
    hosting.adapter = {
      setContainerControl: vi.fn(),
      setContainerSetpoint: vi.fn(),
    } as unknown as MinerAdapter;
    const telemetry = createEmptyTelemetryState();
    telemetry.plant.set(plantId, { componentId: plantId, ...reading });
    useMinerStore.getState().telemetry.mergeTelemetry(telemetry);
    render(<ContainerDiagnosticView />);
    expect(screen.getByRole("button", { name: "Manage" })).toBeDisabled();
    if (reading.controls) {
      expect(screen.queryByText("Auto")).not.toBeInTheDocument();
      expect(screen.queryByText(/target inlet temp/)).not.toBeInTheDocument();
    }
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(hosting.adapter.setContainerControl).not.toHaveBeenCalled();
  });

  test("opens advertised manual mode without defaulting it to auto", () => {
    hosting.adapter = { setContainerControl: vi.fn(), setContainerSetpoint: vi.fn() } as unknown as MinerAdapter;
    const telemetry = createEmptyTelemetryState();
    telemetry.plant.set(plantId, {
      componentId: plantId,
      controls: [{ name: "dry_cooler_auto", enabled: false, stateKnown: true }],
    });
    useMinerStore.getState().telemetry.mergeTelemetry(telemetry);
    render(<ContainerDiagnosticView />);
    fireEvent.click(screen.getByRole("button", { name: "Manage" }));
    expect(screen.getByLabelText("Pump speed (RPM)")).toBeInTheDocument();
    expect(screen.queryByLabelText("Target °C")).not.toBeInTheDocument();
    screen.getAllByRole("button", { name: "Save" }).forEach((button) => expect(button).toBeDisabled());
  });

  test("closes settings if advertised support disappears", () => {
    hosting.adapter = { setContainerControl: vi.fn(), setContainerSetpoint: vi.fn() } as unknown as MinerAdapter;
    render(<ContainerDiagnosticView />);
    fireEvent.click(screen.getByRole("button", { name: "Manage" }));
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    act(() => {
      const telemetry = createEmptyTelemetryState();
      telemetry.plant.set(plantId, { componentId: plantId, setpoints: [] });
      useMinerStore.getState().telemetry.mergeTelemetry(telemetry);
    });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Manage" })).toBeDisabled();
  });

  test("refreshes telemetry after saving settings", async () => {
    hosting.adapter = {
      setContainerSetpoint: vi.fn().mockResolvedValue(undefined),
      setContainerControl: vi.fn().mockResolvedValue(undefined),
      getCurrentTelemetry: vi.fn().mockResolvedValue(telemetryWithTarget(19)),
    } as unknown as MinerAdapter;
    openSettingsAndSave();
    await waitFor(() => expect(screen.getByText("19.0°C target inlet temp")).toBeInTheDocument());
  });

  test("discards a settings refresh that resolves after switching devices", async () => {
    const response = deferred<ReturnType<typeof createEmptyTelemetryState>>();
    const getCurrentTelemetry = vi.fn(() => response.promise);
    hosting.adapter = {
      setContainerSetpoint: vi.fn().mockResolvedValue(undefined),
      setContainerControl: vi.fn().mockResolvedValue(undefined),
      getCurrentTelemetry,
    } as unknown as MinerAdapter;
    openSettingsAndSave();
    await waitFor(() => expect(getCurrentTelemetry).toHaveBeenCalled());
    await act(async () => {
      useMinerStore.getState().resetDeviceData();
      response.resolve(telemetryWithTarget(19));
    });
    expect(useMinerStore.getState().telemetry.plant.size).toBe(0);
  });

  test("does not start an old adapter refresh when devices change during the save", async () => {
    const control = deferred<void>();
    const setContainerControl = vi.fn(() => control.promise);
    const getCurrentTelemetry = vi.fn().mockResolvedValue(telemetryWithTarget(19));
    hosting.adapter = {
      setContainerSetpoint: vi.fn().mockResolvedValue(undefined),
      setContainerControl,
      getCurrentTelemetry,
    } as unknown as MinerAdapter;
    openSettingsAndSave();
    await waitFor(() => expect(setContainerControl).toHaveBeenCalled());
    await act(async () => {
      useMinerStore.getState().resetDeviceData();
      control.resolve();
    });
    expect(getCurrentTelemetry).not.toHaveBeenCalled();
    expect(useMinerStore.getState().telemetry.plant.size).toBe(0);
  });

  test("does not overwrite a newer polling response with an older settings refresh", async () => {
    const response = deferred<ReturnType<typeof createEmptyTelemetryState>>();
    const getCurrentTelemetry = vi.fn(() => response.promise);
    hosting.adapter = {
      setContainerSetpoint: vi.fn().mockResolvedValue(undefined),
      setContainerControl: vi.fn().mockResolvedValue(undefined),
      getCurrentTelemetry,
    } as unknown as MinerAdapter;
    openSettingsAndSave();
    await waitFor(() => expect(getCurrentTelemetry).toHaveBeenCalled());
    await act(async () => {
      const accept = beginStoreWrite("telemetry:current:hashboard,miner");
      if (accept()) useMinerStore.getState().telemetry.mergeTelemetry(telemetryWithTarget(20), accept.requestId);
      response.resolve(telemetryWithTarget(19));
    });
    expect(screen.getByText("20.0°C target inlet temp")).toBeInTheDocument();
  });

  test("renders the aggregate summary and both three-tank banks on All", () => {
    render(<ContainerDiagnosticView />);

    expect(screen.getByText("124.8 kW")).toBeInTheDocument();
    expect(screen.getByText("33.3°C → 43.3°C")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Left tanks" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Right tanks" })).toBeInTheDocument();
    expect(screen.getAllByRole("article")).toHaveLength(6);
    expect(within(screen.getAllByRole("article")[0]).getByText("48/48")).toBeInTheDocument();
  });

  test("switches among the dry cooler, power, and controls designs", () => {
    render(<ContainerDiagnosticView />);

    fireEvent.mouseDown(screen.getByRole("button", { name: "Dry cooler" }));
    expect(screen.getByRole("heading", { name: "Dry cooler" })).toBeInTheDocument();
    expect(screen.getByText("50.0 Hz")).toBeInTheDocument();
    expect(screen.getByText("85% PWM")).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Tanks" })).not.toBeInTheDocument();

    fireEvent.mouseDown(screen.getByRole("button", { name: "Circuit breakers" }));
    expect(screen.getByRole("heading", { name: "Circuit breakers" })).toBeInTheDocument();
    expect(screen.getByText("PDU 1")).toBeInTheDocument();

    fireEvent.mouseDown(screen.getByRole("button", { name: "Controls" }));
    expect(screen.getByRole("heading", { name: "Controls" })).toBeInTheDocument();
    expect(screen.getByText("Coolant pump")).toBeInTheDocument();
    expect(screen.getByText("Inactive")).toBeInTheDocument();
  });

  test("shows each hashboard's issue intensity in physical slot order", () => {
    const fixture = tanksFixture(1, 1);
    const module = [...fixture.topology.modules.values()][0];
    module.hashboardIds.forEach((id, index) => {
      fixture.telemetry.hashboards.set(id, {
        hashboardId: id,
        health: "fault",
        reportedIssueCount: index + 1,
      });
    });
    module.hashboardIds.reverse();
    useMinerStore.getState().hardware.replaceHardware(fixture.hardware);
    useMinerStore.getState().telemetry.mergeTelemetry(fixture.telemetry);

    render(<ContainerDiagnosticView />);

    const segments = within(screen.getByRole("group", { name: "Module 1" })).getAllByRole("img");
    expect(segments).toHaveLength(3);
    expect(segments[0]).toHaveAccessibleName("left hashboard: Needs attention, 1 reported issue");
    expect(segments[0]).toHaveClass("bg-intent-critical-fill/20");
    expect(segments[1]).toHaveAccessibleName("mid hashboard: Needs attention, 2 reported issues");
    expect(segments[1]).toHaveClass("bg-intent-critical-fill/50");
    expect(segments[2]).toHaveAccessibleName("right hashboard: Needs attention, 3 reported issues");
    expect(segments[2]).toHaveClass("bg-intent-critical-fill");
  });

  test("uses plant-only telemetry outside All and Tanks", () => {
    render(<ContainerDiagnosticView />);
    expect(useTelemetry).toHaveBeenLastCalledWith({ level: ["hashboard", "miner"] });
    for (const name of ["Dry cooler", "Circuit breakers", "Controls"]) {
      fireEvent.mouseDown(screen.getByRole("button", { name }));
      expect(useTelemetry).toHaveBeenLastCalledWith({ level: ["miner"] });
    }
    fireEvent.mouseDown(screen.getByRole("button", { name: "Tanks" }));
    expect(useTelemetry).toHaveBeenLastCalledWith({ level: ["hashboard", "miner"] });
  });

  test("preserves column-major module positions and empty slots", () => {
    const fixture = tanksFixture(1, 3);
    const tank = [...fixture.topology.tanks.values()][0];
    tank.moduleLayout = { columns: 8, rows: 2, numbering: "columnMajor" };
    const missing = tank.moduleIds.splice(1, 1)[0];
    fixture.topology.modules.delete(missing);
    useMinerStore.getState().hardware.replaceHardware(fixture.hardware);
    useMinerStore.getState().telemetry.mergeTelemetry(fixture.telemetry);
    render(<ContainerDiagnosticView />);
    expect(screen.getByRole("group", { name: "Module 1" })).toHaveStyle({ gridColumn: "1", gridRow: "2" });
    expect(screen.getByRole("group", { name: "Module 3" })).toHaveStyle({ gridColumn: "2", gridRow: "2" });
    expect(screen.queryByRole("group", { name: "Module 2" })).not.toBeInTheDocument();
  });

  test("opens tank and fan summaries using live readings", () => {
    render(<ContainerDiagnosticView />);
    fireEvent.click(screen.getByRole("button", { name: "Tank 1 info" }));
    const tank = within(screen.getByRole("dialog", { name: "Tank 1 status" }));
    expect(tank.queryByText(/not yet available/i)).not.toBeInTheDocument();
    expect(tank.getByText("48/48")).toBeInTheDocument();
    expect(tank.getByText("60.0°C")).toBeInTheDocument();
    fireEvent.click(tank.getByRole("button", { name: "Close dialog" }));
    fireEvent.click(screen.getByRole("button", { name: "Fan 1 info" }));
    const fan = within(screen.getByRole("dialog", { name: "Fan 1 status" }));
    expect(fan.queryByText(/not yet available/i)).not.toBeInTheDocument();
    expect(fan.getByText("85%")).toBeInTheDocument();
    expect(fan.getByText("Healthy")).toBeInTheDocument();
    act(() => {
      const telemetry = createEmptyTelemetryState();
      telemetry.fans.set(fanId, {
        componentId: fanId,
        health: "fault",
        speedPercent: { latest: { value: 15, units: "%" } },
      });
      useMinerStore.getState().telemetry.mergeTelemetry(telemetry);
    });
    expect(fan.getByText("15%")).toBeInTheDocument();
    expect(fan.getByText("Fault")).toBeInTheDocument();
  });

  test("shows pump metrics and one global tank light control", () => {
    render(<ContainerDiagnosticView />);
    fireEvent.mouseDown(screen.getByRole("button", { name: "Controls" }));
    expect(screen.getByText("50.0 Hz input, 161.3 LPM")).toBeInTheDocument();
    expect(screen.queryByText(/controls are not available/i)).not.toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Tank lights" })).toBeDisabled();
    expect(screen.queryByRole("checkbox", { name: "Tank A light" })).not.toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "AC 1" })).toBeDisabled();
  });

  test.each(["unknown", "poweringOn", "poweringOff"] as const)(
    "disables tank and bank power when state is %s",
    async (miningStatus) => {
      hosting.adapter = { startScopedMining: vi.fn(), stopScopedMining: vi.fn() } as unknown as MinerAdapter;
      const fixture = tanksFixture(6);
      fixture.telemetry.rollupsByEntity.forEach((rollup) => {
        rollup.miningStatus = miningStatus;
      });
      useMinerStore.getState().telemetry.mergeTelemetry(fixture.telemetry);
      render(<ContainerDiagnosticView />);
      const tank = screen.getByRole("checkbox", { name: "Tank 1 power" });
      expect(tank).toBeDisabled();
      expect(screen.getByRole("checkbox", { name: "Left tanks power" })).toBeDisabled();
      if (miningStatus === "poweringOn") expect(tank).toBeChecked();
      await userEvent.click(tank);
      expect(hosting.adapter.startScopedMining).not.toHaveBeenCalled();
      expect(hosting.adapter.stopScopedMining).not.toHaveBeenCalled();
      if (miningStatus === "unknown") expect(screen.queryByText("Not mining")).not.toBeInTheDocument();
    },
  );

  test("sends only the selected tank or bank's modules and serializes power commands", async () => {
    const command = deferred<void>();
    const stopScopedMining = vi.fn().mockReturnValue(command.promise);
    hosting.adapter = {
      startScopedMining: vi.fn(),
      stopScopedMining,
      getCurrentTelemetry: vi.fn().mockResolvedValue(createEmptyTelemetryState()),
    } as unknown as MinerAdapter;
    render(<ContainerDiagnosticView />);
    fireEvent.click(screen.getByRole("checkbox", { name: "Tank 1 power" }));
    expect(stopScopedMining).toHaveBeenCalledWith(
      { modules: Array.from({ length: 16 }, (_, index) => ({ tank: 1, module: index + 1 })) },
      expect.anything(),
    );
    expect(screen.getByRole("checkbox", { name: "Left tanks power" })).toBeDisabled();
    await act(async () => command.resolve());
    fireEvent.click(screen.getByRole("checkbox", { name: "Left tanks power" }));
    const scope = stopScopedMining.mock.calls[1][0] as { modules: { tank: number }[] };
    expect(scope.modules).toHaveLength(48);
    expect(new Set(scope.modules.map((module) => module.tank))).toEqual(new Set([1, 2, 3]));
    await act(async () => {});
  });

  test("writes supported controls and refreshes without optimistic state", async () => {
    const command = deferred<void>();
    const setContainerControl = vi.fn(() => command.promise);
    hosting.adapter = {
      setContainerControl,
      getCurrentTelemetry: vi.fn().mockResolvedValue(createEmptyTelemetryState()),
    } as unknown as MinerAdapter;
    render(<ContainerDiagnosticView />);
    fireEvent.mouseDown(screen.getByRole("button", { name: "Controls" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Coolant pump" }));
    expect(setContainerControl).toHaveBeenCalledWith("coolant_pump", false, expect.anything());
    expect(screen.getByRole("checkbox", { name: "Coolant pump" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Coolant pump" })).toBeDisabled();
    await act(async () => command.resolve());
    expect(hosting.adapter.getCurrentTelemetry).toHaveBeenCalledWith(["miner"]);
  });

  test("does not repeat a successful control command when refresh fails", async () => {
    const setContainerControl = vi.fn().mockResolvedValue(undefined);
    hosting.adapter = {
      setContainerControl,
      getCurrentTelemetry: vi.fn().mockRejectedValue(new Error("Refresh failed")),
    } as unknown as MinerAdapter;
    render(<ContainerDiagnosticView />);
    fireEvent.mouseDown(screen.getByRole("button", { name: "Controls" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Coolant pump" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Command sent.");
    expect(setContainerControl).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("checkbox", { name: "Coolant pump" })).toBeEnabled();
  });

  test("reports control failures without changing the displayed state", async () => {
    hosting.adapter = {
      setContainerControl: vi.fn().mockRejectedValue(new Error("Command rejected")),
      getCurrentTelemetry: vi.fn(),
    } as unknown as MinerAdapter;
    render(<ContainerDiagnosticView />);
    fireEvent.mouseDown(screen.getByRole("button", { name: "Controls" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Coolant pump" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Could not change the control.");
    expect(screen.getByRole("checkbox", { name: "Coolant pump" })).toBeChecked();
    expect(hosting.adapter.getCurrentTelemetry).not.toHaveBeenCalled();
  });

  test("does not refresh the previous device when a control completes after switching devices", async () => {
    const command = deferred<void>();
    const getCurrentTelemetry = vi.fn();
    hosting.adapter = {
      setContainerControl: vi.fn(() => command.promise),
      getCurrentTelemetry,
    } as unknown as MinerAdapter;
    render(<ContainerDiagnosticView />);
    fireEvent.mouseDown(screen.getByRole("button", { name: "Controls" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Coolant pump" }));
    await act(async () => {
      useMinerStore.getState().resetDeviceData();
      command.resolve();
    });
    expect(getCurrentTelemetry).not.toHaveBeenCalled();
  });

  test("discards control telemetry returned after switching devices", async () => {
    const response = deferred<ReturnType<typeof createEmptyTelemetryState>>();
    const getCurrentTelemetry = vi.fn(() => response.promise);
    hosting.adapter = {
      setContainerControl: vi.fn().mockResolvedValue(undefined),
      getCurrentTelemetry,
    } as unknown as MinerAdapter;
    render(<ContainerDiagnosticView />);
    fireEvent.mouseDown(screen.getByRole("button", { name: "Controls" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Coolant pump" }));
    await waitFor(() => expect(getCurrentTelemetry).toHaveBeenCalled());
    await act(async () => {
      useMinerStore.getState().resetDeviceData();
      response.resolve(telemetryWithTarget(19));
    });
    expect(useMinerStore.getState().telemetry.plant.size).toBe(0);
  });
});
