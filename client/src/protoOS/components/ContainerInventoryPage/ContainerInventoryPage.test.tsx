import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";

import ContainerInventoryPage from "./ContainerInventoryPage";

describe("ContainerInventoryPage", () => {
  test("renders all six tanks", () => {
    render(
      <ContainerInventoryPage
        title="Tanks"
        emptyMessage="No tanks found."
        items={Array.from({ length: 6 }, (_, index) => ({
          id: `tank:${index + 1}`,
          label: `Tank ${index + 1}`,
          details: ["16 modules", "48 hashboards"],
        }))}
      />,
    );

    expect(screen.getByRole("heading", { name: "Tanks" })).toBeInTheDocument();
    expect(screen.getByText("Tank 1")).toBeInTheDocument();
    expect(screen.getByText("Tank 6")).toBeInTheDocument();
    expect(screen.getAllByText("16 modules")).toHaveLength(6);
  });

  test("renders its empty state", () => {
    render(<ContainerInventoryPage title="Modules" emptyMessage="No modules found." items={[]} />);

    expect(screen.getByText("No modules found.")).toBeInTheDocument();
  });

  test("calls onSelectItem when a card is selected", () => {
    const onSelectItem = vi.fn();

    render(
      <ContainerInventoryPage
        title="Tanks"
        emptyMessage="No tanks found."
        items={[{ id: "tank:1", label: "Tank 1", details: ["16 modules"] }]}
        onSelectItem={onSelectItem}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "View Tank 1" }));

    expect(onSelectItem).toHaveBeenCalledWith("tank:1");
  });
});
