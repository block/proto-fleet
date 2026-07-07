import { type ComponentProps } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import TextareaListModal from "./TextareaListModal";

const defaultProps: ComponentProps<typeof TextareaListModal> = {
  open: true,
  categoryKey: "subnet",
  label: "Subnet",
  validate: (line: string) => (line === "BAD" ? "rejected by stub" : null),
  initialValue: [],
  onApply: vi.fn(),
  onClose: vi.fn(),
};

const renderModal = (overrides: Partial<ComponentProps<typeof TextareaListModal>> = {}) =>
  render(<TextareaListModal {...defaultProps} {...overrides} />);

describe("TextareaListModal", () => {
  it("renders the textarea and Apply button", () => {
    renderModal();
    expect(screen.getByRole("textbox")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /apply/i })).toBeInTheDocument();
  });

  it("hydrates the textarea from initialValue", () => {
    renderModal({ initialValue: ["192.168.1.0/24", "10.0.0.0/8"] });
    expect((screen.getByRole("textbox") as HTMLTextAreaElement).value).toBe("192.168.1.0/24\n10.0.0.0/8");
  });

  it("does not call onApply on keystrokes", () => {
    const onApply = vi.fn();
    renderModal({ onApply });
    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "192.168.1.0/24" },
    });
    expect(onApply).not.toHaveBeenCalled();
  });

  it("emits dedup'd, normalized values on Apply", () => {
    const onApply = vi.fn();
    const normalize = (line: string) => line.toUpperCase();
    renderModal({ onApply, normalize });
    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "abc\ndef\nabc" },
    });
    fireEvent.click(screen.getByRole("button", { name: /apply/i }));
    expect(onApply).toHaveBeenCalledWith(["ABC", "DEF"]);
  });

  it("validates on blur, not on keystroke", () => {
    renderModal();
    const textbox = screen.getByRole("textbox");
    fireEvent.change(textbox, { target: { value: "BAD" } });
    // No error while typing.
    expect(screen.queryByText(/rejected by stub/i)).not.toBeInTheDocument();
    fireEvent.blur(textbox);
    // Error surfaces on blur.
    expect(screen.getByText(/rejected by stub/i)).toBeInTheDocument();
  });

  it("clears the error on focus and re-validates on the next blur", () => {
    renderModal();
    const textbox = screen.getByRole("textbox");
    fireEvent.change(textbox, { target: { value: "BAD" } });
    fireEvent.blur(textbox);
    expect(screen.getByText(/rejected by stub/i)).toBeInTheDocument();

    // Focus clears the stale error.
    fireEvent.focus(textbox);
    expect(screen.queryByText(/rejected by stub/i)).not.toBeInTheDocument();

    // Correcting the value and blurring again leaves no error.
    fireEvent.change(textbox, { target: { value: "ok" } });
    fireEvent.blur(textbox);
    expect(screen.queryByText(/rejected by stub/i)).not.toBeInTheDocument();
  });

  it("disables Apply when any line is invalid (after blur)", () => {
    renderModal();
    const textbox = screen.getByRole("textbox");
    fireEvent.change(textbox, { target: { value: "ok\nBAD\nok2" } });
    fireEvent.blur(textbox);
    expect(screen.getByText(/Line 2/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /apply/i })).toBeDisabled();
  });

  it("emits empty array when textarea is cleared on Apply", () => {
    const onApply = vi.fn();
    renderModal({ initialValue: ["192.168.1.0/24"], onApply });
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "" } });
    fireEvent.click(screen.getByRole("button", { name: /apply/i }));
    expect(onApply).toHaveBeenCalledWith([]);
  });

  it("caps at maxLines with a 'Showing first N' notice", () => {
    const onApply = vi.fn();
    const lines = Array.from({ length: 5 }, (_, i) => `entry-${i}`);
    renderModal({ onApply, maxLines: 3 });
    const textbox = screen.getByRole("textbox");
    fireEvent.change(textbox, { target: { value: lines.join("\n") } });
    fireEvent.blur(textbox);
    expect(screen.getByText(/Showing first 3/i)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /apply/i }));
    expect(onApply).toHaveBeenCalledWith(["entry-0", "entry-1", "entry-2"]);
  });
});
