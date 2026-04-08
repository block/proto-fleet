import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";

import { DatePickerField } from ".";

describe("DatePickerField", () => {
  test("disables the clear button when the field is disabled", () => {
    const onClear = vi.fn();

    render(
      <DatePickerField
        id="expires-at"
        label="Expiration date"
        selectedDate={new Date(2026, 3, 8)}
        clearable
        onClear={onClear}
        disabled
      />,
    );

    const clearButton = screen.getByRole("button", { name: "Clear" });
    expect(clearButton).toBeDisabled();

    fireEvent.click(clearButton);
    expect(onClear).not.toHaveBeenCalled();
  });

  test("hides the clear button when no clear handler is provided", () => {
    render(<DatePickerField id="expires-at" label="Expiration date" selectedDate={new Date(2026, 3, 8)} clearable />);

    expect(screen.queryByRole("button", { name: "Clear" })).toBeNull();
  });
});
