import { act, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import PsuStatusCard from "./PsuStatusCard";
import { asComponentId, asHashboardId, asModuleId } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

vi.mock("@/protoOS/components/StatusModal", () => ({ ProtoOSStatusModal: () => null }));
const { telemetryNow } = vi.hoisted(() => ({ telemetryNow: vi.fn(() => Date.now()) }));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({ useMinerHosting: () => ({ adapter: { telemetryNow } }) }));
const ids = [asComponentId("module:a:psu:1"), asComponentId("module:b:psu:1")];

beforeEach(() => {
  vi.useFakeTimers();
  telemetryNow.mockImplementation(() => Date.now());
  useMinerStore.getState().resetDeviceData();
  useMinerStore.setState((state) => {
    ids.forEach((id) =>
      state.hardware.psus.set(id, {
        ref: { id, kind: "psu", owner: { kind: "module", id: asModuleId(id.slice(0, -6)) } },
        slot: 1,
      }),
    );
    state.telemetry.psus.set(ids[0], {
      componentId: ids[0],
      health: "fault",
      observedAt: { electrical: Date.now(), temperatures: Date.now(), thermalState: Date.now() },
      inputPower: { latest: { value: 0, units: "W" } },
    });
    state.telemetry.psus.set(ids[1], {
      componentId: ids[1],
      health: "ok",
      observedAt: { electrical: Date.now(), temperatures: Date.now(), thermalState: Date.now() },
    });
  });
});
afterEach(() => vi.useRealTimers());

test("flags board-reported supply faults even with healthy thermal telemetry and qualifies stale reports", () => {
  const psu = useMinerStore.getState().hardware.psus.get(ids[1])!;
  useMinerStore.getState().minerStatus.setErrors([
    {
      id: "supply",
      code: "HASHBOARD_ERROR_CAUSE_BOARD_POWER_SUPPLY",
      message: "Power supply fault",
      severity: "critical",
      owner: psu.ref.owner,
      component: psu.ref,
      reportedBy: [
        {
          component: { kind: "hashboard", id: asHashboardId("board-2"), owner: psu.ref.owner },
          label: "Board 2",
          observedAt: Date.now() - 119_000,
        },
      ],
    },
  ]);
  render(<PsuStatusCard componentId={ids[1]} />);
  expect(screen.getByRole("img", { name: "PSU 1 needs attention" })).toBeInTheDocument();
  act(() => vi.advanceTimersByTime(1_000));
  expect(screen.getByText("Board fault reports are stale")).toBeInTheDocument();
  act(() => useMinerStore.getState().minerStatus.setErrors([]));
  expect(screen.queryByRole("img")).not.toBeInTheDocument();
  expect(screen.queryByText("Board fault reports are stale")).not.toBeInTheDocument();
});

test("shows missing readings as dashes, retains real zero, and isolates PSU warnings", () => {
  render(
    <>
      <PsuStatusCard componentId={ids[0]} />
      <PsuStatusCard componentId={ids[1]} />
    </>,
  );
  const [first, second] = screen.getAllByTestId("card").map((card) => within(card));
  expect(first.getAllByText("—")).toHaveLength(7);
  expect(first.getByText("0.0 kW")).toBeInTheDocument();
  expect(first.getByRole("img", { name: "PSU 1 needs attention" })).toBeInTheDocument();
  expect(second.getAllByText("—")).toHaveLength(8);
  expect(second.queryByRole("img")).not.toBeInTheDocument();
  expect(screen.getAllByRole("button", { name: "More info" })).toHaveLength(2);
  act(() =>
    useMinerStore.setState((state) => {
      state.telemetry.psus.get(ids[0])!.health = "ok";
    }),
  );
  expect(screen.queryByRole("img")).not.toBeInTheDocument();
});

test("groups currents with the correct input/output readings and expires readings without another poll", () => {
  useMinerStore.setState((state) => {
    Object.assign(state.telemetry.psus.get(ids[0])!, {
      health: "ok",
      inputCurrent: { latest: { value: 0, units: "A" } },
      outputCurrent: { latest: { value: 123.4, units: "A" } },
      averageTemperature: { latest: { value: 45, units: "°C" } },
      observedAt: { electrical: Date.now() - 119_000, temperatures: Date.now(), thermalState: undefined },
    });
  });
  render(<PsuStatusCard componentId={ids[0]} />);
  expect(within(screen.getByRole("region", { name: "AC input" })).getByText("0.0 A")).toBeInTheDocument();
  expect(within(screen.getByRole("region", { name: "DC output" })).getByText("123.4 A")).toBeInTheDocument();
  expect(screen.getByText("Thermal status unavailable")).toBeInTheDocument();
  act(() => vi.advanceTimersByTime(1_000));
  expect(screen.queryByText("123.4 A")).not.toBeInTheDocument();
  expect(screen.getAllByText("Readings stale")).toHaveLength(2);
  expect(screen.getByText(/45/)).toBeInTheDocument();
  act(() =>
    useMinerStore.setState((state) => {
      state.telemetry.psus.get(ids[0])!.observedAt = {
        electrical: Date.now(),
        temperatures: Date.now(),
        thermalState: Date.now(),
      };
    }),
  );
  expect(screen.getByText("123.4 A")).toBeInTheDocument();
  expect(screen.queryByText("Readings stale")).not.toBeInTheDocument();
});

test("restores retained PSU readings and thermal status when the controller clock recovers", () => {
  const observedAt = Date.now();
  telemetryNow.mockReturnValue(NaN);
  useMinerStore.setState((state) => {
    const psu = state.telemetry.psus.get(ids[0])!;
    psu.health = "unknown";
    psu.rawObservations = {
      electrical: [{ frameAt: observedAt, observedAt, values: { inputPower: { latest: { value: 0, units: "W" } } } }],
      temperatures: [],
      thermalState: [{ frameAt: observedAt, observedAt, health: "degraded" }],
    };
  });
  render(<PsuStatusCard componentId={ids[0]} />);
  expect(screen.getByText("Thermal status unavailable")).toBeInTheDocument();
  expect(screen.queryByText("0.0 kW")).not.toBeInTheDocument();
  expect(screen.queryByRole("img")).not.toBeInTheDocument();

  telemetryNow.mockReturnValue(observedAt + 1_000);
  act(() => vi.advanceTimersByTime(1_000));
  expect(screen.queryByText("Thermal status unavailable")).not.toBeInTheDocument();
  expect(screen.getByText("0.0 kW")).toBeInTheDocument();
  expect(screen.getByRole("img", { name: "PSU 1 needs attention" })).toBeInTheDocument();
});
