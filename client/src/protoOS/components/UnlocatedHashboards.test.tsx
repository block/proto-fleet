import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { getScopeHealth } from "./StatusModal/scopeHealth";
import ContainerUnlocatedHashboards from "./UnlocatedHashboards";
import { toCanonicalContainer, toCanonicalErrors } from "@/protoOS/api/adapters/mdkV2/normalize";
import useMinerStore from "@/protoOS/store/useMinerStore";

beforeEach(() => useMinerStore.getState().resetDeviceData());
afterEach(() => vi.useRealTimers());

it("exposes an unlocated board and its fault without a tank or module", () => {
  const wire = {
    containerMap: {
      tanks: [
        {
          id: 0,
          modules: [
            {
              id: 0,
              slots: [
                {
                  hashboard: {
                    macAddress: "aa:bb:cc:dd:ee:01",
                    serial: "board-one",
                    health: {
                      timestamp: new Date().toISOString(),
                      hashrate: 2.5,
                      temperatures: [28],
                      errors: [{}],
                    },
                  },
                },
              ],
            },
          ],
        },
      ],
    },
  };
  const snapshot = toCanonicalContainer(wire, {}, {}, "ct1");
  const errors = toCanonicalErrors(wire, "ct1");
  const store = useMinerStore.getState();
  store.hardware.replaceHardware(snapshot.hardware);
  store.telemetry.mergeTelemetry(snapshot.telemetry);
  store.minerStatus.setErrors(errors);
  render(<ContainerUnlocatedHashboards />);
  expect(screen.getByText("Unlocated hashboards (1)")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "View hashboards" }));
  fireEvent.click(screen.getByRole("button", { name: "View hashboard aa:bb:cc:dd:ee:01" }));
  expect(screen.getByText("Unknown error")).toBeInTheDocument();
  expect(screen.getByText("Serial number: board-one")).toBeInTheDocument();
  expect(screen.queryByText(/Tank 0|Module 0/)).not.toBeInTheDocument();
  const health = getScopeHealth({ kind: "container" }, snapshot.hardware, snapshot.telemetry, errors);
  expect(health?.needsAttention).toBe(true);
  expect(health?.affectedModules).toBe(0);
  expect(health?.subtitle).not.toContain("across 0 modules");
});

it("keeps an isolated unlocated board fault visible without detailed errors", () => {
  const snapshot = toCanonicalContainer(
    { containerMap: { tanks: [{ id: 0, modules: [{ id: 0, slots: [{ hashboard: { serial: "isolated" } }] }] }] } },
    {},
    {},
    "ct1",
  );
  const board = [...snapshot.telemetry.hashboards.values()][0];
  board.health = "fault";
  board.healthObservedAt = Date.now();
  board.isolated = true;
  expect(getScopeHealth({ kind: "container" }, snapshot.hardware, snapshot.telemetry, [])?.needsAttention).toBe(true);
});

it.each([
  ["mining", "Hashing"],
  ["stopped", "Sleeping"],
  ["curtailed", "Sleeping"],
  ["error", "Needs attention"],
] as const)("uses the module status label for %s boards", (miningStatus, label) => {
  const snapshot = toCanonicalContainer(
    { containerMap: { tanks: [{ id: 0, modules: [{ id: 0, slots: [{ hashboard: { serial: "unlocated" } }] }] }] } },
    {},
    {},
    "ct1",
  );
  const board = [...snapshot.telemetry.hashboards.values()][0];
  board.health = "ok";
  board.healthObservedAt = Date.now();
  board.miningStatus = miningStatus;
  useMinerStore.getState().hardware.replaceHardware(snapshot.hardware);
  useMinerStore.getState().telemetry.mergeTelemetry(snapshot.telemetry);
  render(<ContainerUnlocatedHashboards />);
  fireEvent.click(screen.getByRole("button", { name: "View hashboards" }));
  expect(screen.getByText(label)).toBeInTheDocument();
  expect(screen.queryByText("Healthy")).not.toBeInTheDocument();
});

it.each(["old", "missing", "future"] as const)("does not renew %s health when inventory polls succeed", (kind) => {
  vi.useFakeTimers();
  const now = Date.now();
  const timestamp = kind === "missing" ? undefined : new Date(now + (kind === "old" ? -180_000 : 60_000)).toISOString();
  const wire = {
    containerMap: {
      tanks: [
        {
          id: 0,
          modules: [
            {
              id: 0,
              slots: [
                {
                  hashboard: {
                    serial: "expired",
                    health: { timestamp, hashrate: 2.5, temperatures: [28], errors: [{}] },
                  },
                },
                {
                  hashboard: {
                    serial: "current",
                    health: { timestamp: new Date(now).toISOString(), hashrate: 3.5, temperatures: [32], errors: [] },
                  },
                },
              ],
            },
          ],
        },
      ],
    },
  };
  const poll = () => {
    const snapshot = toCanonicalContainer(wire, {}, {}, "ct1");
    const store = useMinerStore.getState();
    store.hardware.replaceHardware(snapshot.hardware);
    store.telemetry.mergeTelemetry(snapshot.telemetry);
    store.minerStatus.setErrors(toCanonicalErrors(wire, "ct1"));
  };
  poll();
  render(<ContainerUnlocatedHashboards />);
  fireEvent.click(screen.getByRole("button", { name: "View hashboards" }));
  act(() => {
    vi.advanceTimersByTime(10_000);
    poll();
  });
  const expired = screen.getByRole("button", { name: "View hashboard expired" }).closest("tr")!;
  expect(within(expired).getByText("Unknown")).toBeInTheDocument();
  expect(within(expired).queryByText("28.0 °C")).not.toBeInTheDocument();
  const current = screen.getByRole("button", { name: "View hashboard current" }).closest("tr")!;
  expect(within(current).getByText("32.0 °C")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "View hashboard expired" }));
  expect(screen.getByText("Current readings are unavailable.")).toBeInTheDocument();
  expect(screen.getByText("Issue details unavailable.")).toBeInTheDocument();
  expect(screen.queryByText("Unknown error")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Back to hashboards" }));
  fireEvent.click(screen.getByRole("button", { name: "View hashboard current" }));
  expect(screen.getByText("Temperature: 32.0 °C")).toBeInTheDocument();
  act(() => vi.advanceTimersByTime(120_000));
  expect(screen.getByText("Temperature: —")).toBeInTheDocument();
});
