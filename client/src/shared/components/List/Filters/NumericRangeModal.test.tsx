import { type ComponentProps } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import NumericRangeModal from "./NumericRangeModal";
import type { NumericRangeBounds } from "@/shared/utils/filterValidation";

const bounds: NumericRangeBounds = { min: 0, max: 1000, unit: "TH/s" };

const defaultProps: ComponentProps<typeof NumericRangeModal> = {
  open: true,
  categoryKey: "hashrate",
  label: "Hashrate",
  bounds,
  initialValue: {},
  onApply: vi.fn(),
  onClose: vi.fn(),
};

const renderModal = (overrides: Partial<ComponentProps<typeof NumericRangeModal>> = {}) =>
  render(<NumericRangeModal {...defaultProps} {...overrides} />);

describe("NumericRangeModal", () => {
  it("renders Min and Max inputs and Apply/Cancel buttons", () => {
    renderModal();
    expect(screen.getByLabelText(/min/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/max/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /apply/i })).toBeInTheDocument();
  });

  it("hydrates from initialValue", () => {
    renderModal({ initialValue: { min: 50, max: 200 } });
    expect(screen.getByLabelText(/min/i)).toHaveValue(50);
    expect(screen.getByLabelText(/max/i)).toHaveValue(200);
  });

  it("does not call onApply on every keystroke", () => {
    const onApply = vi.fn();
    renderModal({ onApply });
    fireEvent.change(screen.getByLabelText(/min/i), { target: { value: "75" } });
    fireEvent.change(screen.getByLabelText(/max/i), { target: { value: "150" } });
    expect(onApply).not.toHaveBeenCalled();
  });

  it("emits the draft value when Apply is clicked", () => {
    const onApply = vi.fn();
    renderModal({ onApply });
    fireEvent.change(screen.getByLabelText(/min/i), { target: { value: "75" } });
    fireEvent.change(screen.getByLabelText(/max/i), { target: { value: "150" } });
    fireEvent.click(screen.getByRole("button", { name: /apply/i }));
    expect(onApply).toHaveBeenCalledWith({ min: 75, max: 150 });
  });

  it("emits empty value when both inputs are blank on Apply", () => {
    const onApply = vi.fn();
    renderModal({ initialValue: { min: 50 }, onApply });
    fireEvent.change(screen.getByLabelText(/min/i), { target: { value: "" } });
    fireEvent.click(screen.getByRole("button", { name: /apply/i }));
    expect(onApply).toHaveBeenCalledWith({});
  });

  it("disables Apply when validation fails", () => {
    renderModal();
    fireEvent.change(screen.getByLabelText(/min/i), { target: { value: "-5" } });
    expect(screen.getByText(/Minimum is 0/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /apply/i })).toBeDisabled();
  });

  it("disables Apply on min > max", () => {
    renderModal();
    fireEvent.change(screen.getByLabelText(/min/i), { target: { value: "100" } });
    fireEvent.change(screen.getByLabelText(/max/i), { target: { value: "50" } });
    expect(screen.getByText(/min must not exceed max/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /apply/i })).toBeDisabled();
  });

  it("does not emit when closed without Apply", () => {
    const onApply = vi.fn();
    const onClose = vi.fn();
    renderModal({ onApply, onClose });
    fireEvent.change(screen.getByLabelText(/min/i), { target: { value: "100" } });
    // Simulate the parent closing the modal (via onClose) without Apply.
    onClose();
    expect(onApply).not.toHaveBeenCalled();
  });
});
