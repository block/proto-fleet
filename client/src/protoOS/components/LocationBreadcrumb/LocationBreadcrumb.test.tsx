import { MemoryRouter } from "react-router-dom";
import { render, screen, within } from "@testing-library/react";
import { expect, test } from "vitest";

import LocationBreadcrumb from "./LocationBreadcrumb";
import { tanksFixture } from "@/protoOS/features/tanks/Tanks.fixtures";
import { asModuleId } from "@/protoOS/store/contract";

test("shows the owning tank without duplicating the module selector", () => {
  const { topology } = tanksFixture(2, 2);
  const tank = [...topology.tanks.values()][1];
  tank.label = "  North tank  ";
  const module = topology.modules.get(tank.moduleIds[1])!;
  render(
    <MemoryRouter>
      <LocationBreadcrumb topology={topology} moduleId={module.id} />
    </MemoryRouter>,
  );
  const trail = screen.getByRole("navigation", { name: "Breadcrumb" });
  expect(trail).toHaveTextContent(/^North tank$/);
  expect(within(trail).queryByRole("link")).not.toBeInTheDocument();
  expect(within(trail).queryByRole("button")).not.toBeInTheDocument();
});

test.each([undefined, "", "   "])("blank tank label %s falls back to its slot", (label) => {
  const { topology } = tanksFixture(2, 1);
  const tank = [...topology.tanks.values()][1];
  tank.label = label;
  render(
    <MemoryRouter>
      <LocationBreadcrumb topology={topology} moduleId={tank.moduleIds[0]} />
    </MemoryRouter>,
  );
  expect(screen.getByRole("navigation")).toHaveTextContent(/^Tank 2$/);
});

test("slotless tanks match Tanks page display order", () => {
  const { topology } = tanksFixture(2, 1);
  const tank = [...topology.tanks.values()][0];
  tank.label = undefined;
  tank.slot = undefined;
  const module = topology.modules.get(tank.moduleIds[0])!;
  module.location = undefined;
  render(
    <MemoryRouter>
      <LocationBreadcrumb topology={topology} moduleId={module.id} />
    </MemoryRouter>,
  );
  // Slotless tanks sort after numbered tanks, even when inserted first.
  expect(screen.getByRole("navigation")).toHaveTextContent(/^Tank 2$/);
});

test("unresolved modules or owning tanks render no element or spacing", () => {
  const { topology } = tanksFixture(1, 1);
  const module = [...topology.modules.values()][0];
  const { container, rerender } = render(
    <MemoryRouter>
      <LocationBreadcrumb topology={topology} moduleId={asModuleId("missing")} />
    </MemoryRouter>,
  );
  expect(container).toBeEmptyDOMElement();
  topology.tanks.clear();
  rerender(
    <MemoryRouter>
      <LocationBreadcrumb topology={topology} moduleId={module.id} />
    </MemoryRouter>,
  );
  expect(container).toBeEmptyDOMElement();
});

test("shows board ancestors without duplicating the board selector and rejects mismatched modules", () => {
  const { topology } = tanksFixture(1, 2);
  const [module, other] = [...topology.modules.values()];
  const board = topology.hashboards.get(module.hashboardIds[2])!;
  // Neither serial syntax nor collection order is a location coordinate.
  board.identity.value = "arbitrary-board-serial";
  const { rerender } = render(
    <MemoryRouter>
      <LocationBreadcrumb topology={topology} moduleId={module.id} board={board} />
    </MemoryRouter>,
  );
  expect(screen.getByRole("navigation")).toHaveTextContent(/^Tank 1\/Module 1$/);
  module.location = undefined;
  rerender(
    <MemoryRouter>
      <LocationBreadcrumb topology={topology} moduleId={module.id} board={board} />
    </MemoryRouter>,
  );
  expect(screen.getByRole("navigation")).toHaveTextContent(/^Tank 1\/Module \?$/);
  rerender(
    <MemoryRouter>
      <LocationBreadcrumb topology={topology} moduleId={other.id} board={board} />
    </MemoryRouter>,
  );
  expect(screen.queryByRole("navigation")).not.toBeInTheDocument();
});
