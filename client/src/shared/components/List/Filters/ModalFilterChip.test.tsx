import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import ModalFilterChip from "./ModalFilterChip";

describe("ModalFilterChip", () => {
  it("renders the condition on the left and the type label on the right", () => {
    render(
      <ModalFilterChip
        filterValue="hashrate"
        typeLabel="Hashrate"
        condition="≥ 50 TH/s AND ≤ 200 TH/s"
        onEdit={vi.fn()}
        onClear={vi.fn()}
      />,
    );
    const chip = screen.getByTestId("active-filter-hashrate");
    expect(chip).toHaveTextContent("≥ 50 TH/s AND ≤ 200 TH/s");
    expect(chip).toHaveTextContent("Hashrate");
  });

  it("calls onEdit when the editable region is clicked", () => {
    const onEdit = vi.fn();
    render(
      <ModalFilterChip
        filterValue="hashrate"
        typeLabel="Hashrate"
        condition="≥ 50 TH/s"
        onEdit={onEdit}
        onClear={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByTestId("active-filter-hashrate-edit"));
    expect(onEdit).toHaveBeenCalledTimes(1);
  });

  it("calls onClear when the clear button is clicked", () => {
    const onClear = vi.fn();
    render(
      <ModalFilterChip
        filterValue="subnet"
        typeLabel="Subnet"
        condition="192.168.1.0/24"
        onEdit={vi.fn()}
        onClear={onClear}
      />,
    );
    fireEvent.click(screen.getByTestId("active-filter-subnet-clear"));
    expect(onClear).toHaveBeenCalledTimes(1);
  });

  it("does not call onEdit when clear is clicked", () => {
    const onEdit = vi.fn();
    render(
      <ModalFilterChip
        filterValue="hashrate"
        typeLabel="Hashrate"
        condition="≥ 50 TH/s"
        onEdit={onEdit}
        onClear={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByTestId("active-filter-hashrate-clear"));
    expect(onEdit).not.toHaveBeenCalled();
  });
});
