import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import ModuleHashboardCard from "./ModuleHashboardCard";
import { HashboardSlotSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

const settings = vi.hoisted(() => ({ unit: "C" }));
vi.mock("@/protoFleet/store", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/store")>()),
  useTemperatureUnit: () => settings.unit,
}));
beforeEach(() => {
  settings.unit = "C";
});
it("expires board measurements separately from state and keeps installed hardware identity", () => {
  const now = Date.now();
  const slot = create(HashboardSlotSchema, {
    position: 1,
    occupancy: "occupied",
    observedAt: timestampFromMs(now),
    board: {
      hashboardId: 11n,
      installationId: 21n,
      serial: "HB-ONE",
      state: "active",
      observedAt: timestampFromMs(now - 120_000),
      stateObservedAt: timestampFromMs(now),
      hashrateHs: 200e12,
      hashrailOk: true,
    },
  });
  const { rerender } = render(<ModuleHashboardCard slot={slot} now={now} />);
  expect(screen.queryByText("Hashing")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /issues$/ })).not.toBeInTheDocument();
  expect(screen.queryByText("200.0 TH/s")).not.toBeInTheDocument();
  expect(screen.getByText("Board readings unavailable or stale.")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /history|view/i })).not.toBeInTheDocument();
  slot.board!.observedAt = timestampFromMs(now);
  slot.board!.stateObservedAt = timestampFromMs(now - 120_000);
  rerender(<ModuleHashboardCard slot={slot} now={now} />);
  expect(screen.getByText("200.0 TH/s")).toBeInTheDocument();
  expect(screen.queryByText("Hashing")).not.toBeInTheDocument();
  expect(screen.queryByText(/Hashrail:|Reference clocks:/)).not.toBeInTheDocument();
  rerender(<ModuleHashboardCard slot={slot} now={now + 120_000} />);
  expect(screen.queryByText("HB-ONE")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /history/ })).not.toBeInTheDocument();
});

it("shows a non-interactive issue icon and opens board details through info", () => {
  const now = Date.now();
  const slot = create(HashboardSlotSchema, {
    position: 2,
    occupancy: "occupied",
    observedAt: timestampFromMs(now),
    board: {
      serial: "HB-TWO",
      state: "active",
      observedAt: timestampFromMs(now),
      stateObservedAt: timestampFromMs(now),
      health: { warnings: [{ cause: "HASHBOARD_ERROR_CAUSE_BOARD_OVER_TEMPERATURE" }] },
      issues: [
        {
          component: "hashboard",
          cause: "HASHBOARD_ERROR_CAUSE_BOARD_OVER_TEMPERATURE",
          severity: "warning",
          observedAt: timestampFromMs(now),
        },
      ],
    },
  });
  const { rerender } = render(<ModuleHashboardCard slot={slot} now={now} />);
  const icon = screen.getByRole("img", { name: "Board 2 needs attention" });
  expect(icon.textContent).toBe("");
  expect(icon.querySelector("svg")).toBeInTheDocument();
  expect(screen.queryByText(/Over temperature/)).not.toBeInTheDocument();
  expect(screen.queryByText("Hashboard 2 failure")).not.toBeInTheDocument();
  fireEvent.click(icon);
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Board 2 info" }));
  expect(screen.getByRole("dialog", { name: "Board 2 status" })).toHaveTextContent("Over temperature");
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  slot.board!.health = { errors: [], warnings: [] };
  slot.board!.issues = [];
  rerender(<ModuleHashboardCard slot={slot} now={now} />);
  expect(screen.queryByRole("button", { name: /issues$/ })).not.toBeInTheDocument();
});

it.each([
  [undefined, 0],
  [{ errors: [], warnings: [] }, 0],
  [undefined, 120_000],
])("keeps a current error visible without diagnostic causes (%j, %s)", (health, sampleAge) => {
  const now = Date.now();
  const slot = create(HashboardSlotSchema, {
    position: 1,
    occupancy: "occupied",
    observedAt: timestampFromMs(now),
    board: {
      state: "error",
      stateObservedAt: timestampFromMs(now),
      observedAt: timestampFromMs(now - sampleAge),
      health,
    },
  });
  const { rerender } = render(<ModuleHashboardCard slot={slot} now={now} />);
  expect(screen.getByRole("img", { name: "Board 1 needs attention" })).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Board 1 info" }));
  expect(screen.getByRole("dialog")).toHaveTextContent("Needs attention");
  expect(screen.getByRole("dialog")).toHaveTextContent("Detailed error information is unavailable.");
  expect(screen.getByRole("dialog")).not.toHaveTextContent("No active component errors reported.");
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  slot.board!.stateObservedAt = timestampFromMs(now - 120_000);
  rerender(<ModuleHashboardCard slot={slot} now={now} />);
  expect(screen.queryByRole("img", { name: "Board 1 needs attention" })).not.toBeInTheDocument();
  slot.board!.stateObservedAt = timestampFromMs(now);
  slot.board!.state = "active";
  slot.board!.miningState = "MINING_STATE_ERROR";
  rerender(<ModuleHashboardCard slot={slot} now={now} />);
  expect(screen.queryByRole("img", { name: "Board 1 needs attention" })).not.toBeInTheDocument();
});

it.each([false, true])("presents isolated board details as attention unless state is stale (%s)", (stale) => {
  const now = Date.now();
  const slot = create(HashboardSlotSchema, {
    position: 1,
    occupancy: "occupied",
    observedAt: timestampFromMs(now),
    board: {
      state: "inactive",
      isolated: true,
      stateObservedAt: timestampFromMs(now - (stale ? 120_000 : 0)),
      observedAt: timestampFromMs(now),
      health: { errors: [], warnings: [] },
    },
  });
  render(<ModuleHashboardCard slot={slot} now={now} />);
  fireEvent.click(screen.getByRole("button", { name: "Board 1 info" }));
  expect(screen.getByRole("dialog")).toHaveTextContent(stale ? "Status unknown" : "Sleeping");
  expect(screen.getByRole("dialog")).not.toHaveTextContent(/isolated|Fault reported/i);
});

it("uses the same five metrics as ProtoOS and keeps identity in the info modal", () => {
  const now = Date.now();
  const slot = create(HashboardSlotSchema, {
    position: 1,
    occupancy: "occupied",
    observedAt: timestampFromMs(now),
    board: {
      serial: "BOARD-SERIAL",
      macAddress: "aa:bb:cc:dd:ee:ff",
      state: "active",
      observedAt: timestampFromMs(now),
      stateObservedAt: timestampFromMs(now),
      hashrateHs: 200e12,
      powerW: 5800,
      efficiencyJh: 29e-12,
      temperatureC: 61,
      health: {
        asicTemperatureObservedAt: new Date(now).toISOString(),
        maximumAsicTemperatureC: 75,
        errors: [],
        warnings: [],
      },
    },
  });
  const { rerender } = render(<ModuleHashboardCard slot={slot} now={now} />);
  for (const [label, value] of [
    ["Efficiency", "29.0 J/TH"],
    ["Total hashrate", "200.0 TH/s"],
    ["Total power", "5.8 kW"],
    ["ASIC avg", "61.0 °C"],
    ["ASIC high", "75.0 °C"],
  ]) {
    expect(screen.getByText(label).parentElement).toHaveTextContent(value);
  }
  expect(screen.queryByText("BOARD-SERIAL")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Board 1 info" }));
  expect(screen.getByRole("dialog")).toHaveTextContent("BOARD-SERIAL");
  expect(screen.getByRole("dialog")).toHaveTextContent("aa:bb:cc:dd:ee:ff");
  expect(screen.getByRole("dialog")).not.toHaveTextContent(/Hashrail|Reference clocks/);
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  slot.board!.health = {};
  rerender(<ModuleHashboardCard slot={slot} now={now} />);
  expect(screen.getByText("ASIC high").parentElement).toHaveTextContent("—");
  slot.board!.health = { asicTemperatureObservedAt: new Date(now).toISOString(), maximumAsicTemperatureC: 75 };
  slot.board!.observedAt = timestampFromMs(now - 120_000);
  rerender(<ModuleHashboardCard slot={slot} now={now} />);
  expect(screen.getByText("ASIC avg").parentElement).toHaveTextContent("—");
  expect(screen.getByText("ASIC high").parentElement).toHaveTextContent("—");
});

it.each([
  [0, "32.0 °F"],
  [75, "167.0 °F"],
  ["75", "—"],
  [null, "—"],
  [Infinity, "—"],
])("formats only a valid ASIC maximum (%s) using Fleet's temperature preference", (maximum, expected) => {
  settings.unit = "F";
  const now = Date.now();
  const slot = create(HashboardSlotSchema, {
    position: 1,
    occupancy: "occupied",
    observedAt: timestampFromMs(now),
    board: {
      observedAt: timestampFromMs(now),
      stateObservedAt: timestampFromMs(now),
      temperatureC: 0,
      health: { asicTemperatureObservedAt: new Date(now).toISOString(), maximumAsicTemperatureC: maximum },
    },
  });
  render(<ModuleHashboardCard slot={slot} now={now} />);
  expect(screen.getByText("ASIC avg").parentElement).toHaveTextContent("32.0 °F");
  expect(screen.getByText("ASIC high").parentElement).toHaveTextContent(expected);
});

it.each([
  ["fresh", 0, "75.0 °C"],
  ["just before expiry", -119_999, "75.0 °C"],
  ["expired", -120_000, "—"],
  ["old", -300_000, "—"],
  ["allowed clock skew", 5_000, "75.0 °C"],
  ["excessive clock skew", 5_001, "—"],
  ["missing", undefined, "—"],
  ["null", null, "—"],
  ["invalid", "not-a-timestamp", "—"],
  ["non-RFC3339", "10/19/2026", "—"],
  ["numeric string", "0", "—"],
  ["object", { seconds: "0", nanos: 0 }, "—"],
])("validates the ASIC source timestamp independently of fresh board health (%s)", (_, source, expected) => {
  const now = Date.now();
  const slot = create(HashboardSlotSchema, {
    position: 1,
    occupancy: "occupied",
    observedAt: timestampFromMs(now),
    board: {
      observedAt: timestampFromMs(now),
      stateObservedAt: timestampFromMs(now),
      temperatureC: 61,
      health: {
        maximumAsicTemperatureC: 75,
        ...(source === undefined
          ? {}
          : {
              asicTemperatureObservedAt: typeof source === "number" ? new Date(now + source).toISOString() : source,
            }),
      },
    },
  });
  render(<ModuleHashboardCard slot={slot} now={now} />);
  expect(screen.getByText("ASIC avg").parentElement).toHaveTextContent("61.0 °C");
  expect(screen.getByText("ASIC high").parentElement).toHaveTextContent(expected);
});

it("expires the ASIC maximum as time passes while board readings remain current", () => {
  const now = Date.now();
  const slot = create(HashboardSlotSchema, {
    position: 1,
    occupancy: "occupied",
    observedAt: timestampFromMs(now),
    board: {
      observedAt: timestampFromMs(now),
      stateObservedAt: timestampFromMs(now),
      temperatureC: 61,
      health: { maximumAsicTemperatureC: 75, asicTemperatureObservedAt: new Date(now - 60_000).toISOString() },
    },
  });
  const { rerender } = render(<ModuleHashboardCard slot={slot} now={now} />);
  expect(screen.getByText("ASIC high").parentElement).toHaveTextContent("75.0 °C");
  rerender(<ModuleHashboardCard slot={slot} now={now + 60_000} />);
  expect(screen.getByText("ASIC avg").parentElement).toHaveTextContent("61.0 °C");
  expect(screen.getByText("ASIC high").parentElement).toHaveTextContent("—");
});

it.each(["MINING_STATE_ERROR", "MINING_STATE_MINING"])(
  "renders canonical causes without diagnosing raw state (%s)",
  (miningState) => {
    const now = Date.now();
    const slot = create(HashboardSlotSchema, {
      position: 1,
      occupancy: "occupied",
      observedAt: timestampFromMs(now),
      board: {
        state: "error",
        miningState,
        observedAt: timestampFromMs(now),
        stateObservedAt: timestampFromMs(now),
        health: { errors: [], warnings: [], psuThermal: { thermalStateValid: false } },
        issues: [
          {
            component: "hashboard",
            cause: "HASHBOARD_ERROR_CAUSE_ASIC_ENUMERATION",
            severity: "fault",
            observedAt: timestampFromMs(now),
          },
        ],
      },
    });
    const { rerender } = render(<ModuleHashboardCard slot={slot} now={now} />);
    expect(screen.getByRole("img", { name: "Board 1 needs attention" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Board 1 info" }));
    expect(screen.getByRole("dialog")).toHaveTextContent("Asic enumeration");
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    rerender(<ModuleHashboardCard slot={slot} now={now + 120_000} />);
    expect(screen.queryByRole("img", { name: "Board 1 needs attention" })).not.toBeInTheDocument();
  },
);

it.each(["ASIC_ENUMERATION", "ASIC_ECC"])(
  "retains fresh %s issues independently of unknown state and readings",
  (cause) => {
    vi.useFakeTimers();
    try {
      const now = Date.now();
      const slot = create(HashboardSlotSchema, {
        position: 1,
        occupancy: "occupied",
        observedAt: timestampFromMs(now),
        board: {
          state: "unknown",
          observedAt: timestampFromMs(now),
          issues: [{ component: "hashboard", cause, severity: "fault", observedAt: timestampFromMs(now) }],
        },
      });
      const { rerender } = render(<ModuleHashboardCard slot={slot} now={now} />);
      expect(screen.getByRole("img", { name: "Board 1 needs attention" })).toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "Board 1 info" }));
      expect(screen.getByRole("dialog")).toHaveTextContent(cause === "ASIC_ECC" ? "Asic ecc" : "Asic enumeration");
      fireEvent.click(screen.getByRole("button", { name: "Done" }));
      // Metric availability and stale operating evidence cannot hide a fresh cause.
      slot.board!.observedAt = undefined;
      slot.board!.stateObservedAt = timestampFromMs(now - 120_000);
      rerender(<ModuleHashboardCard slot={slot} now={now} />);
      expect(screen.getByRole("img", { name: "Board 1 needs attention" })).toBeInTheDocument();
      act(() => vi.advanceTimersByTime(120_000));
      expect(screen.queryByRole("img", { name: "Board 1 needs attention" })).not.toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  },
);
