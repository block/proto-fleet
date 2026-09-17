import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import FirmwarePickerButton from "./FirmwarePickerButton";

const options = [
  { value: "", label: "No firmware" },
  { value: "uploaded-file", label: "1.4.4" },
];

describe("FirmwarePickerButton", () => {
  beforeEach(() => {
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 120, 40));
  });

  afterEach(() => vi.restoreAllMocks());

  test("does not describe an unresolved nonempty file ID as no firmware", () => {
    render(
      <FirmwarePickerButton
        label="Firmware"
        options={options}
        value="missing-file"
        onChange={vi.fn()}
        testId="picker"
      />,
    );
    expect(screen.getByTestId("picker")).toHaveTextContent("Firmware details unavailable");
    fireEvent.click(screen.getByTestId("picker"));
    expect(screen.getByRole("option", { name: "No firmware" })).toHaveAttribute("aria-selected", "false");
  });

  test("uses the assigned version for an unavailable file and lets the user explicitly clear it", () => {
    const onChange = vi.fn();
    const { rerender } = render(
      <FirmwarePickerButton
        label="Firmware"
        options={options}
        value={null}
        unresolvedLabel="1.4.3"
        onChange={onChange}
        testId="picker"
      />,
    );
    expect(screen.getByTestId("picker")).toHaveTextContent("1.4.3");
    fireEvent.click(screen.getByTestId("picker"));
    expect(screen.getByRole("option", { name: "No firmware" })).toHaveAttribute("aria-selected", "false");
    fireEvent.click(screen.getByRole("option", { name: "No firmware" }));
    expect(onChange).toHaveBeenCalledExactlyOnceWith("");
    rerender(
      <FirmwarePickerButton
        label="Firmware"
        options={options}
        value=""
        unresolvedLabel="1.4.3"
        onChange={onChange}
        testId="picker"
      />,
    );
    expect(screen.getByTestId("picker")).toHaveTextContent("No firmware");
  });

  test("uses available option metadata and falls back to the assignment when that metadata disappears", () => {
    const props = {
      label: "Firmware",
      value: "uploaded-file",
      unresolvedLabel: "1.4.3",
      onChange: vi.fn(),
      testId: "picker",
    };
    const { rerender } = render(<FirmwarePickerButton {...props} options={options} />);
    expect(screen.getByTestId("picker")).toHaveTextContent("1.4.4");
    rerender(<FirmwarePickerButton {...props} options={[options[0]]} />);
    expect(screen.getByTestId("picker")).toHaveTextContent("1.4.3");
  });
});
