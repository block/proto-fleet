import { MemoryRouter } from "react-router-dom";
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { ModuleGrid } from "../types";
import ModuleStateGrid from "./ModuleStateGrid";

const module = (column: number, state: ModuleGrid["tanks"][number]["modules"][number]["state"]) => ({
  id: `module:${column}`,
  label: String(column).padStart(2, "0"),
  position: { row: 2, column },
  name: `Tank 1 module ${column}`,
  to: `/tanks/1/modules/${column}`,
  state,
  boards: ["healthy", "healthy", "healthy"] as ModuleGrid["tanks"][number]["modules"][number]["boards"],
});

describe("ModuleStateGrid", () => {
  it("marks a module that needs attention when all its hashboards are healthy", () => {
    const grid = {
      counts: { healthy: 1, attention: 1, offline: 0, sleeping: 0, unknown: 0 },
      tanks: [
        { id: "tank:1", label: "Tank 1", columns: 8, rows: 2, modules: [module(1, "healthy"), module(2, "attention")] },
      ],
    } as ModuleGrid;
    render(
      <MemoryRouter>
        <ModuleStateGrid grid={grid} />
      </MemoryRouter>,
    );
    const healthy = screen.getByRole("button", { name: "Tank 1 module 1, Hashing" });
    const faulty = screen.getByRole("button", { name: "Tank 1 module 2, Needs attention" });
    expect(within(healthy).queryByTestId("module-state-marker")).toBeNull();
    expect(within(faulty).getByTestId("module-state-marker")).toBeInTheDocument();
  });
  it("keeps six tanks in a three-column layout with numbered modules", () => {
    const grid: ModuleGrid = {
      counts: { healthy: 6, attention: 0, offline: 0, sleeping: 0, unknown: 0 },
      tanks: [1, 2, 3, 4, 5, 6].map((slot) => ({
        id: `tank:${slot}`,
        label: `Tank ${slot}`,
        slot,
        columns: 8,
        rows: 2,
        modules: [{ ...module(1, "healthy"), name: `Tank ${slot} module 1` }],
      })),
    };
    render(
      <MemoryRouter>
        <ModuleStateGrid grid={grid} />
      </MemoryRouter>,
    );
    const diagram = screen.getByLabelText("Tank layout");
    expect(diagram.children).toHaveLength(2);
    const legend = screen.getByRole("list", { name: "Modules by state" });
    expect(within(legend).queryByText("Isolated")).toBeNull();
    expect(diagram.compareDocumentPosition(legend) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(screen.getAllByRole("heading").map((heading) => heading.textContent)).toEqual([
      "Tank 1",
      "Tank 2",
      "Tank 3",
      "Tank 4",
      "Tank 5",
      "Tank 6",
    ]);
    for (const slot of [1, 2, 3, 4, 5, 6]) {
      const tank = screen.getByRole("region", { name: `Tank ${slot}` });
      expect(tank.style.getPropertyValue("--tank-column")).toBe(String(3 - ((slot - 1) % 3)));
      expect(tank.parentElement).toBe(diagram.children[Math.floor((slot - 1) / 3)]);
      const link = within(tank).getByRole("button", { name: `Tank ${slot} module 1, Hashing` });
      expect(link).toHaveTextContent("01");
      expect(link).toHaveStyle({ gridRow: "2", gridColumn: "1" });
    }
  });
});

it("renders blank slots, offline dots, and unknown question marks without moving modules", () => {
  const grid: ModuleGrid = {
    counts: { healthy: 0, attention: 0, offline: 1, sleeping: 1, unknown: 1 },
    tanks: [
      {
        id: "tank:1",
        label: "Tank 1",
        columns: 8,
        rows: 2,
        modules: [
          { ...module(1, "offline"), boards: ["healthy", "empty", "offline"] },
          { ...module(2, "unknown"), boards: ["empty", "empty", "empty"] },
          { ...module(3, "unknown"), boards: ["healthy", "unknown", "healthy"] },
          { ...module(4, "sleeping"), boards: ["sleeping", "attention", "sleeping"] },
        ],
      },
    ],
  };
  render(
    <MemoryRouter>
      <ModuleStateGrid grid={grid} />
    </MemoryRouter>,
  );
  const emptyModule = screen.getByRole("button", { name: "Tank 1 module 2, No boards mapped" });
  expect(emptyModule).toHaveClass("border");
  expect(emptyModule).toHaveTextContent("02");
  expect(emptyModule.querySelectorAll('[data-board-state="empty"]')).toHaveLength(3);
  expect(within(emptyModule).queryByTestId("module-state-marker")).toBeNull();
  const offline = screen.getByRole("button", { name: /module 1, Offline/ });
  const blank = offline.querySelector('[data-board-state="empty"]')!;
  expect(blank).toBeEmptyDOMElement();
  expect(blank).not.toHaveClass("border");
  expect(offline.querySelector('[data-board-state="offline"] > span')).toHaveClass(
    "rounded-full",
    "bg-intent-warning-fill",
  );
  const unknown = screen.getByRole("button", { name: /module 3, Unknown/ });
  expect(unknown).toHaveStyle({ gridColumn: "3" });
  expect(unknown.querySelector('[data-board-state="unknown"]')).toHaveTextContent("?");
  expect(within(unknown).getByTestId("module-state-marker")).toHaveTextContent("?");
  const sleeping = screen.getByRole("button", { name: /module 4, Sleeping/ });
  expect(within(sleeping).getAllByTestId("module-state-marker")).toHaveLength(1);
  expect(within(sleeping).getByTestId("module-state-marker").querySelector(".bg-intent-info-fill")).toBeInTheDocument();
  expect(sleeping.querySelector('[data-board-state="attention"]')).toHaveClass("bg-intent-critical-80");
});

it.each(["critical", "warning"] as const)("shows %s PSU faults beside empty module numbers", (severity) => {
  const grid: ModuleGrid = {
    counts: { healthy: 0, attention: 1, offline: 0, sleeping: 0, unknown: 0 },
    tanks: [
      {
        id: "tank:1",
        label: "Tank 1",
        columns: 8,
        rows: 2,
        modules: [{ ...module(1, "attention"), boards: ["empty", "empty", "empty"], attentionSeverity: severity }],
      },
    ],
  };
  render(<ModuleStateGrid grid={grid} />);
  const tile = screen.getByRole("button", { name: "Tank 1 module 1, Needs attention, No boards mapped" });
  expect(tile).toHaveTextContent("01");
  expect(within(tile).getByTestId("module-state-marker").firstElementChild).toHaveClass(`bg-intent-${severity}-fill`);
  for (const slot of tile.querySelectorAll('[data-board-state="empty"]')) {
    expect(slot).toBeEmptyDOMElement();
    expect(slot).not.toHaveClass("border");
  }
});
