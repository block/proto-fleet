import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import Tab from "./Tab"; // Adjust the import path as necessary

describe("TabMenu", () => {
  const data = {
    id: "tab1",
    name: "Tab 1",
    axId: "tab1",
    label: "Tab 1",
    value: 1,
    units: "Units 1",
    path: "/tab1",
    isActive: true,
    onClick: () => {},
  };

  it("renders tab active status correctly", () => {
    render(<Tab {...data} isActive={true} />);
    expect(screen.getByText(data.name)).toBeInTheDocument();

    const tab = screen.getByText(data.name).closest("button");
    expect(tab).toHaveClass("bg-surface-base");
  });

  it("renders tab inactive status correctly", () => {
    render(<Tab {...data} isActive={false} />);
    expect(screen.getByText(data.name)).toBeInTheDocument();

    const tab = screen.getByText(data.name).closest("button");
    expect(tab).not.toHaveClass("bg-surface-base");
  });

  it("calls onClick handler on click", () => {
    const onClick = vi.fn();
    render(<Tab {...data} onClick={onClick} />);
    const tab = screen
      .getByText(data.name)
      .closest("button") as HTMLButtonElement;

    fireEvent.click(tab);
    expect(onClick).toHaveBeenCalled();
  });
});
