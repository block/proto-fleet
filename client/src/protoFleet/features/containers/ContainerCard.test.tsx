import { MemoryRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import ContainerCard from "./ContainerCard";
import { tankHealth } from "./containerHealth";
import { ContainerSchema, TankHealthSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { healthHeatBand } from "@/protoFleet/features/buildings/components/cardHealth";

describe("ContainerCard", () => {
  it("uses the same proportion bands as building racks", () => {
    const tank = create(TankHealthSchema, { position: 1, hashingCount: 15, attentionCount: 1 });
    expect(tankHealth(tank, false).band).toBe(healthHeatBand(1 / 16));
    const container = create(ContainerSchema, {
      deviceIdentifier: "ct1",
      name: "Container 1",
      tanks: [tank],
    });
    render(
      <MemoryRouter>
        <ContainerCard container={container} />
      </MemoryRouter>,
    );
    expect(screen.getByRole("img", { name: "Tank 1: 1 of 16 modules affected" })).toHaveAttribute(
      "data-heat-band",
      "2",
    );
    expect(screen.getByRole("link")).toHaveAttribute("href", "/containers/ct1");
  });
  it("never renders incomplete or stale observations as healthy", () => {
    expect(tankHealth(create(TankHealthSchema, { hashingCount: 15, unknownCount: 1 }), false).incomplete).toBe(true);
    expect(tankHealth(create(TankHealthSchema, { hashingCount: 16 }), true).incomplete).toBe(true);
    expect(tankHealth(create(TankHealthSchema), false).incomplete).toBe(true);
  });
  it("includes sleeping and isolated modules in the affected proportion", () => {
    const state = tankHealth(create(TankHealthSchema, { hashingCount: 2, sleepingCount: 1, isolatedCount: 1 }), false);
    expect(state.affected).toBe(2);
    expect(state.band).toBe(5);
  });
});

it("opens FleetNode-owned containers in Fleet without a proxy", () => {
  const container = create(ContainerSchema, {
    deviceIdentifier: "remote",
    name: "Remote CT1",
  });
  render(
    <MemoryRouter>
      <ContainerCard container={container} />
    </MemoryRouter>,
  );
  expect(screen.getByRole("link")).toHaveAttribute("href", "/containers/remote");
  expect(screen.getByRole("link")).not.toHaveAttribute("target");
});

it("opens stored details even without a controller web connection", () => {
  const container = create(ContainerSchema, { deviceIdentifier: "unavailable", name: "CT1" });
  render(
    <MemoryRouter>
      <ContainerCard container={container} />
    </MemoryRouter>,
  );
  expect(screen.getByRole("link")).toHaveAttribute("href", "/containers/unavailable");
  expect(screen.queryByText("Web view unavailable")).not.toBeInTheDocument();
});
