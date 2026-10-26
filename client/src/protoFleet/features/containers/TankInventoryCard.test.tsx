import { render, screen, within } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";
import TankInventoryCard from "./TankInventoryCard";
import { EquipmentCoverageSchema } from "@/protoFleet/api/generated/common/v1/equipment_coverage_pb";
import { EntityKind, InventoryItemSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

vi.mock("@/protoFleet/store", () => ({ useTemperatureUnit: () => "C" }));

const tank = () =>
  create(InventoryItemSchema, {
    ref: { kind: EntityKind.TANK, id: 1n },
    name: "CT1 T2",
    assetCount: 16n,
    hashrateThs: 7850,
    powerKw: 121.7,
    hashrateReportingCount: 15n,
    powerReportingCount: 15n,
    listStats: { hashingCount: 13, brokenCount: 1, offlineCount: 1, sleepingCount: 1 },
  });

it("shows shared module coverage once above the health bar", () => {
  render(<TankInventoryCard item={tank()} onClick={() => {}} />);
  const summary = screen.getByTestId("tank-inventory-coverage");
  expect(summary).toHaveTextContent("15/16 modules reporting");
  expect(screen.getAllByText("15/16 modules reporting")).toHaveLength(1);
  expect(screen.queryByText("16 modules")).not.toBeInTheDocument();
  expect(screen.getByText("121.7 kW").parentElement).not.toHaveTextContent("reporting");
});

it("keeps different metric counts separate in the summary", () => {
  const item = tank();
  item.powerReportingCount = 14n;
  render(<TankInventoryCard item={item} onClick={() => {}} />);
  const summary = screen.getByTestId("tank-inventory-coverage");
  expect(summary).toHaveTextContent("16 modules");
  expect(summary).toHaveTextContent("Hashrate: 15/16 modules reporting");
  expect(summary).toHaveTextContent("Power: 14/16 modules reporting");
});

it("preserves board, meter, and unknown topology coverage", () => {
  const item = tank();
  item.coverage = create(EquipmentCoverageSchema, {
    hashrate: { reporting: 47, expected: 48, topologyComplete: true },
    power: { reporting: 14, expected: 16, topologyComplete: false },
  });
  render(<TankInventoryCard item={item} onClick={() => {}} />);
  const summary = screen.getByTestId("tank-inventory-coverage");
  expect(summary).toHaveTextContent("16 modules");
  expect(summary).toHaveTextContent("Hashrate: 47/48 boards reporting");
  expect(summary).toHaveTextContent("Power: 14 meters reporting · topology incomplete");
});

it("shows only the module count when both metrics have complete coverage", () => {
  const item = tank();
  item.hashrateReportingCount = 16n;
  item.powerReportingCount = 16n;
  render(<TankInventoryCard item={item} onClick={() => {}} />);
  expect(screen.getByTestId("tank-inventory-coverage")).toHaveTextContent(/^16 modules$/);
});

it.each([
  [0n, "0 modules"],
  [1n, "1 module"],
] as const)("formats the inventory count %s as %s", (count, label) => {
  const item = tank();
  item.assetCount = count;
  item.hashrateReportingCount = count;
  item.powerReportingCount = count;
  render(<TankInventoryCard item={item} onClick={() => {}} />);
  expect(screen.getByTestId("tank-inventory-coverage")).toHaveTextContent(label);
  expect(screen.getByTestId("tank-inventory-coverage")).not.toHaveTextContent("hashing");
});

it("uses a singular module label when the only module is not reporting", () => {
  const item = tank();
  item.assetCount = 1n;
  item.hashrateReportingCount = 0n;
  item.powerReportingCount = 0n;
  render(<TankInventoryCard item={item} onClick={() => {}} />);
  expect(screen.getByTestId("tank-inventory-coverage")).toHaveTextContent("0/1 module reporting");
});

it("does not show coverage for missing readings, but keeps coverage for measured zero", () => {
  const item = tank();
  item.hashrateThs = undefined;
  item.powerKw = 0;
  render(<TankInventoryCard item={item} onClick={() => {}} />);
  const summary = screen.getByTestId("tank-inventory-coverage");
  expect(summary).not.toHaveTextContent("Hashrate:");
  expect(summary).toHaveTextContent("Power: 15/16 modules reporting");
  expect(screen.getByText("—")).toBeInTheDocument();
  expect(screen.getByText("0.0 kW")).toBeInTheDocument();
});

it("keeps the action button separate from tank navigation", async () => {
  const onClick = vi.fn();
  const onAction = vi.fn();
  const user = userEvent.setup();
  render(<TankInventoryCard item={tank()} onClick={onClick} actions={<button onClick={onAction}>Actions</button>} />);
  await user.click(screen.getByRole("button", { name: "Actions" }));
  expect(onAction).toHaveBeenCalledOnce();
  expect(onClick).not.toHaveBeenCalled();
  const card = screen.getByTestId("tank-inventory-card");
  await user.click(within(card).getByRole("button", { name: /CT1 T2/ }));
  expect(onClick).toHaveBeenCalledOnce();
});
