import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { InventoryCell } from "./InventoryCell";
import { EntityKind, InventoryItemSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

const preferences = vi.hoisted(() => ({ unit: "C" as "C" | "F" }));
vi.mock("@/protoFleet/store", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/store")>()),
  useTemperatureUnit: () => preferences.unit,
}));

it.each([
  ["C", 60.1, "60.1 °C"],
  ["F", 60.1, "140.2 °F"],
  ["C", 0, "0.0 °C"],
  ["C", undefined, "—"],
] as const)("shows one module mean in %s for %s", (unit, mean, expected) => {
  preferences.unit = unit;
  const item = create(InventoryItemSchema, {
    ref: { kind: EntityKind.MODULE, id: 1n },
    minTemperatureC: mean,
    maxTemperatureC: mean,
  });
  render(<InventoryCell item={item} column="temperature" />);
  expect(screen.getByText(expected)).toBeInTheDocument();
});

it("retains module-mean ranges for tanks", () => {
  preferences.unit = "C";
  const item = create(InventoryItemSchema, {
    ref: { kind: EntityKind.TANK, id: 1n },
    minTemperatureC: 59.2,
    maxTemperatureC: 61.3,
  });
  render(<InventoryCell item={item} column="temperature" />);
  expect(screen.getByText("59.2 °C – 61.3 °C")).toBeInTheDocument();
});

it("keeps partial board coverage beside the module mean", () => {
  preferences.unit = "C";
  const item = create(InventoryItemSchema, {
    ref: { kind: EntityKind.MODULE, id: 1n },
    minTemperatureC: 60.1,
    maxTemperatureC: 60.1,
    coverage: { temperature: { reporting: 2, expected: 3, topologyComplete: true } },
  });
  render(<InventoryCell item={item} column="temperature" />);
  expect(screen.getByText("60.1 °C")).toBeInTheDocument();
  expect(screen.getByText("2/3 boards reporting")).toBeInTheDocument();
});
