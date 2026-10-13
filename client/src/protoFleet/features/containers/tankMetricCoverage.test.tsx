import { render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { buildContainerOverview } from "./buildContainerOverview";
import { buildTankOverview } from "./buildTankOverview";
import TankCard from "./components/TankCard";
import type { ContainerOverviewData } from "./hooks/useContainerOverview";
import { InventoryCell } from "./InventoryCell";
import { EquipmentCoverageSchema } from "@/protoFleet/api/generated/common/v1/equipment_coverage_pb";
import { ContainerSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { EntityKind, InventoryItemSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import Stats from "@/shared/components/Stats";
vi.mock("@/protoFleet/store", () => ({ useTemperatureUnit: () => "C" }));
function fixture(): ContainerOverviewData {
  return {
    container: create(ContainerSchema, { id: 7n, name: "CT1" }),
    tanks: [
      create(InventoryItemSchema, {
        ref: { kind: EntityKind.TANK, id: 81n },
        position: 1,
        ancestors: [{ kind: EntityKind.CONTAINER, id: 7n }],
        assetCount: 2n,
        hashrateThs: 1200,
        hashrateReportingCount: 2n,
        powerKw: 18,
        powerReportingCount: 1n,
        efficiencyJth: 30,
        minTemperatureC: 60,
        maxTemperatureC: 70,
        listStats: { temperatureReportingCount: 2, efficiencyReportingCount: 1 },
      }),
    ],
    modules: [1, 2].map((position) =>
      create(InventoryItemSchema, {
        ref: { kind: EntityKind.MODULE, id: BigInt(position) },
        position,
        state: "hashing",
        ancestors: [
          { kind: EntityKind.CONTAINER, id: 7n },
          { kind: EntityKind.TANK, id: 81n },
        ],
      }),
    ),
  };
}
it("labels partial tank KPIs even when every module state is known", () => {
  const view = buildTankOverview(fixture(), "81", "C")!;
  render(<Stats stats={view.kpis} />);
  const items = screen.getAllByTestId("stats-item");
  expect(within(items[0]).queryByText(/reporting/)).not.toBeInTheDocument();
  expect(items[1]).toHaveTextContent("18.0 kW");
  expect(items[1]).toHaveTextContent("1/2 modules reporting");
  expect(items[2]).toHaveTextContent("1/2 modules reporting");
  expect(within(items[3]).queryByText(/reporting/)).not.toBeInTheDocument();
});
it("labels partial power beside the value on container tank cards", () => {
  const card = buildContainerOverview(fixture(), "C").tanks[0];
  render(<TankCard {...card} />);
  expect(screen.getByTestId("tank-card-stats")).toHaveTextContent("2/2 modules hashing");
  expect(screen.getByText("18.0 kW").parentElement).toHaveTextContent("1/2 modules reporting");
});
it.each(["hashrate", "power", "temperature", "efficiency"])(
  "shows metric-specific inventory coverage for %s",
  (column) => {
    const item = fixture().tanks[0];
    render(<InventoryCell item={item} column={column} />);
    if (column === "power" || column === "efficiency")
      expect(screen.getByText("1/2 modules reporting")).toBeInTheDocument();
    else expect(screen.queryByText(/reporting/)).not.toBeInTheDocument();
  },
);
it("does not attach coverage to unavailable metrics", () => {
  const item = fixture().tanks[0];
  item.powerKw = undefined;
  item.powerReportingCount = 0n;
  render(<InventoryCell item={item} column="power" />);
  expect(screen.getByText("—")).toBeInTheDocument();
  expect(screen.queryByText(/reporting/)).not.toBeInTheDocument();
});

describe.each([
  [EntityKind.TANK, "hashrate", "hashrate", "boards"],
  [EntityKind.TANK, "power", "power", "meters"],
  [EntityKind.TANK, "temperature", "temperature", "modules"],
  [EntityKind.TANK, "efficiency", "hashrate", "boards"],
  [EntityKind.MODULE, "temperature", "temperature", "boards"],
  [EntityKind.CONTAINER, "temperature", "temperature", "modules"],
] as const)("coverage-backed subtitle for kind %s, %s", (kind, column, source, unit) => {
  it.each(["complete", "partial", "topology incomplete"] as const)("renders %s coverage", (state) => {
    const item = fixture().tanks[0];
    item.ref!.kind = kind;
    item.coverage = create(EquipmentCoverageSchema, {
      hashrate: { reporting: 9, expected: 9, topologyComplete: true },
      power: { reporting: 1, expected: 1, topologyComplete: true },
      temperature: { reporting: 9, expected: 9, topologyComplete: true },
    });
    const evidence = item.coverage[source]!;
    evidence.reporting = state === "complete" ? 2 : 1;
    evidence.expected = 2;
    evidence.topologyComplete = state !== "topology incomplete";
    render(<InventoryCell item={item} column={column} />);
    if (state === "complete") {
      expect(screen.queryByText(/reporting/)).not.toBeInTheDocument();
    } else {
      expect(
        screen.getByText(state === "partial" ? `1/2 ${unit} reporting` : `1 ${unit} reporting · topology incomplete`),
      ).toBeInTheDocument();
    }
  });
});
