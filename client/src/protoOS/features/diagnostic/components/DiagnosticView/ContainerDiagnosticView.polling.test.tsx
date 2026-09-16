import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import ContainerDiagnosticView from "./ContainerDiagnosticView";
import { tanksFixture } from "@/protoOS/features/tanks/Tanks.fixtures";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { createEmptyTelemetryState } from "@/protoOS/store/utils/createEmptyTelemetryState";

const mocks = vi.hoisted(() => ({ getCurrentTelemetry: vi.fn() }));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ adapter: mocks, isFleetHosted: true }),
}));

beforeEach(() => {
  vi.useFakeTimers();
  mocks.getCurrentTelemetry.mockReset().mockResolvedValue(createEmptyTelemetryState());
  const fixture = tanksFixture(6);
  useMinerStore.getState().resetDeviceData();
  useMinerStore.getState().hardware.replaceHardware(fixture.hardware);
  useMinerStore.getState().telemetry.mergeTelemetry(fixture.telemetry);
});

afterEach(() => vi.useRealTimers());

test.each(["All", "Dry cooler"])(
  "warns about stale readings after a failed %s poll and clears on recovery",
  async (tab) => {
    render(<ContainerDiagnosticView />);
    await act(async () => {});
    if (tab !== "All") {
      fireEvent.mouseDown(screen.getByRole("button", { name: tab }));
      await act(async () => {});
    }
    mocks.getCurrentTelemetry.mockRejectedValueOnce(new Error("Offline"));
    await act(async () => vi.advanceTimersByTimeAsync(15_000));
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Could not refresh container readings. Displayed values may be out of date.",
    );
    expect(screen.getByTestId("container-diagnostics")).toBeInTheDocument();
    await act(async () => vi.advanceTimersByTimeAsync(15_000));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  },
);

test("switching scopes cancels full telemetry, polls plant only, and restores full reads", async () => {
  const view = render(<ContainerDiagnosticView />);
  await act(async () => {});
  expect(mocks.getCurrentTelemetry).toHaveBeenCalledTimes(1);
  expect(mocks.getCurrentTelemetry.mock.calls[0][0]).toEqual(["hashboard", "miner"]);
  const fullSignal = mocks.getCurrentTelemetry.mock.calls[0][1].signal as AbortSignal;
  fireEvent.mouseDown(screen.getByRole("button", { name: "Dry cooler" }));
  await act(async () => {});
  expect(fullSignal.aborted).toBe(true);
  expect(mocks.getCurrentTelemetry).toHaveBeenCalledTimes(2);
  expect(mocks.getCurrentTelemetry.mock.calls[1][0]).toEqual(["miner"]);
  const plantSignal = mocks.getCurrentTelemetry.mock.calls[1][1].signal as AbortSignal;
  fireEvent.mouseDown(screen.getByRole("button", { name: "Controls" }));
  await act(async () => vi.advanceTimersByTimeAsync(15_000));
  expect(mocks.getCurrentTelemetry).toHaveBeenCalledTimes(3);
  expect(mocks.getCurrentTelemetry.mock.calls[2][0]).toEqual(["miner"]);
  expect(plantSignal.aborted).toBe(false);
  fireEvent.mouseDown(screen.getByRole("button", { name: "Tanks" }));
  await act(async () => {});
  expect(plantSignal.aborted).toBe(true);
  expect(mocks.getCurrentTelemetry).toHaveBeenCalledTimes(4);
  expect(mocks.getCurrentTelemetry.mock.calls[3][0]).toEqual(["hashboard", "miner"]);
  const lastSignal = mocks.getCurrentTelemetry.mock.calls[3][1].signal as AbortSignal;
  view.unmount();
  await act(async () => vi.advanceTimersByTimeAsync(30_000));
  expect(lastSignal.aborted).toBe(true);
  expect(mocks.getCurrentTelemetry).toHaveBeenCalledTimes(4);
});

test("changing tabs aborts an in-flight full snapshot", async () => {
  mocks.getCurrentTelemetry.mockImplementationOnce(() => new Promise(() => {}));
  const view = render(<ContainerDiagnosticView />);
  const signal = mocks.getCurrentTelemetry.mock.calls[0][1].signal as AbortSignal;
  fireEvent.mouseDown(screen.getByRole("button", { name: "Circuit breakers" }));
  await act(async () => {});
  expect(signal.aborted).toBe(true);
  expect(mocks.getCurrentTelemetry.mock.calls[1][0]).toEqual(["miner"]);
  view.unmount();
});
