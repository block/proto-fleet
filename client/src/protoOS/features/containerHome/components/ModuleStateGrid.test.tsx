import { MemoryRouter } from "react-router-dom";
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { ModuleGrid } from "../types";
import ModuleStateGrid from "./ModuleStateGrid";

const module = (column: number, state: ModuleGrid["tanks"][number]["modules"][number]["state"]) => ({
  id: `module:${column}`,
  column,
  name: `Tank 1 module ${column}`,
  to: `/tanks/1/modules/${column}`,
  state,
  boards: ["healthy", "healthy", "healthy"] as ModuleGrid["tanks"][number]["modules"][number]["boards"],
});

describe("ModuleStateGrid", () => {
  it("marks a module that needs attention when all its hashboards are healthy", () => {
    const grid = {
      columns: 2,
      counts: { healthy: 1, attention: 1, offline: 0, isolated: 0, sleeping: 0, unknown: 0 },
      tanks: [{ id: "tank:1", label: "Tank 1", modules: [module(1, "healthy"), module(2, "attention")] }],
    } as ModuleGrid;
    render(
      <MemoryRouter>
        <ModuleStateGrid grid={grid} />
      </MemoryRouter>,
    );
    const healthy = screen.getByRole("link", { name: "Tank 1 module 1, Healthy" });
    const faulty = screen.getByRole("link", { name: "Tank 1 module 2, Needs attention" });
    expect(within(healthy).queryByTestId("module-state-marker")).toBeNull();
    expect(within(faulty).getByTestId("module-state-marker")).toBeInTheDocument();
  });
});
