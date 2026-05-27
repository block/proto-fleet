import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import BuildingGridPane from "./BuildingGridPane";

const noop = () => undefined;

const baseProps = {
  cellLabels: {} as Record<string, string>,
  cellRackIds: {} as Record<string, bigint>,
  selectedCellKey: null as string | null,
  showPopover: false,
  onSelectFromList: noop,
  onSearchRacks: noop,
  onPopoverDismiss: noop,
  hasRacks: false,
  hoveredRackId: null as bigint | null,
  onHoverRack: noop,
  onOpenSettings: noop,
  assignedCount: 0,
  totalCells: 0,
};

describe("BuildingGridPane", () => {
  it("renders an empty-state with a Building settings CTA when dims are 0", () => {
    const onOpenSettings = vi.fn();
    render(<BuildingGridPane {...baseProps} aisles={0} racksPerAisle={0} onOpenSettings={onOpenSettings} />);
    expect(screen.getByTestId("manage-building-grid-empty")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("manage-building-grid-empty-open-settings"));
    expect(onOpenSettings).toHaveBeenCalled();
  });

  it("renders an aisles × racks_per_aisle grid", () => {
    render(
      <BuildingGridPane
        {...baseProps}
        aisles={2}
        racksPerAisle={3}
        assignedCount={0}
        totalCells={6}
        onCellClick={vi.fn()}
      />,
    );
    expect(screen.getByTestId("manage-building-grid")).toBeInTheDocument();
    // 2 × 3 = 6 cells
    expect(screen.getByTestId("manage-building-grid-cell-0-0")).toBeInTheDocument();
    expect(screen.getByTestId("manage-building-grid-cell-1-2")).toBeInTheDocument();
  });

  it("clicking a cell fires onCellClick with the cell coordinates + key", () => {
    const onCellClick = vi.fn();
    render(
      <BuildingGridPane
        {...baseProps}
        aisles={2}
        racksPerAisle={2}
        assignedCount={0}
        totalCells={4}
        onCellClick={onCellClick}
      />,
    );
    fireEvent.click(screen.getByTestId("manage-building-grid-cell-1-0"));
    expect(onCellClick).toHaveBeenCalledWith(1, 0, "1-0");
  });

  it("hovering an assigned cell fires onHoverRack(rackId), leaving fires null", () => {
    const onHoverRack = vi.fn();
    render(
      <BuildingGridPane
        {...baseProps}
        aisles={1}
        racksPerAisle={1}
        cellLabels={{ "0-0": "Rack-A" }}
        cellRackIds={{ "0-0": 42n }}
        assignedCount={1}
        totalCells={1}
        onHoverRack={onHoverRack}
        onCellClick={vi.fn()}
      />,
    );
    const cell = screen.getByTestId("manage-building-grid-cell-0-0");
    fireEvent.mouseEnter(cell);
    expect(onHoverRack).toHaveBeenLastCalledWith(42n);
    fireEvent.mouseLeave(cell);
    expect(onHoverRack).toHaveBeenLastCalledWith(null);
  });

  it("renders the popover overlay only on the selected cell when showPopover is true", () => {
    render(
      <BuildingGridPane
        {...baseProps}
        aisles={2}
        racksPerAisle={2}
        assignedCount={0}
        totalCells={4}
        selectedCellKey="0-1"
        showPopover
        hasRacks
        onCellClick={vi.fn()}
      />,
    );
    expect(screen.getByText("Select from list")).toBeInTheDocument();
    expect(screen.getByText("Search racks")).toBeInTheDocument();
  });

  it("Select from list is disabled when hasRacks is false", () => {
    render(
      <BuildingGridPane
        {...baseProps}
        aisles={2}
        racksPerAisle={2}
        assignedCount={0}
        totalCells={4}
        selectedCellKey="0-0"
        showPopover
        hasRacks={false}
        onCellClick={vi.fn()}
      />,
    );
    expect(screen.getByText("Select from list")).toBeDisabled();
    expect(screen.getByText("Search racks")).not.toBeDisabled();
  });

  it("cell shows assigned state when its rack matches hoveredRackId (symmetric hover)", () => {
    render(
      <BuildingGridPane
        {...baseProps}
        aisles={1}
        racksPerAisle={1}
        cellLabels={{ "0-0": "Rack-A" }}
        cellRackIds={{ "0-0": 42n }}
        hoveredRackId={42n}
        assignedCount={1}
        totalCells={1}
        onCellClick={vi.fn()}
      />,
    );
    const cell = screen.getByTestId("manage-building-grid-cell-0-0");
    expect(cell.getAttribute("data-cell-state")).toBe("assigned");
  });
});
