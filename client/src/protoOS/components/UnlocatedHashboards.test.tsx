import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, expect, it } from "vitest";
import { getScopeHealth } from "./StatusModal/scopeHealth";
import ContainerUnlocatedHashboards from "./UnlocatedHashboards";
import { toCanonicalContainer, toCanonicalErrors } from "@/protoOS/api/adapters/mdkV2/normalize";
import useMinerStore from "@/protoOS/store/useMinerStore";

beforeEach(() => useMinerStore.getState().resetDeviceData());

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
  board.miningStatus = miningStatus;
  useMinerStore.getState().hardware.replaceHardware(snapshot.hardware);
  useMinerStore.getState().telemetry.mergeTelemetry(snapshot.telemetry);
  render(<ContainerUnlocatedHashboards />);
  fireEvent.click(screen.getByRole("button", { name: "View hashboards" }));
  expect(screen.getByText(label)).toBeInTheDocument();
  expect(screen.queryByText("Healthy")).not.toBeInTheDocument();
});
