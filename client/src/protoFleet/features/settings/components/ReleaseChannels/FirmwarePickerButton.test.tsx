import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import FirmwarePickerButton from "./FirmwarePickerButton";
import * as windowDimensions from "@/shared/hooks/useWindowDimensions";

const options = [
  { value: "", label: "No firmware" },
  { value: "uploaded-file", label: "1.4.4" },
];

describe("FirmwarePickerButton", () => {
  beforeEach(() => {
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 120, 40));
  });

  afterEach(() => vi.restoreAllMocks());

  test.each(["firmware-O'Brien", 'firmware-Rig "A"', "firmware-Rig\\[A]"])(
    "dismisses outside clicks and touches when the test ID contains special characters: %s",
    (testId) => {
      render(<FirmwarePickerButton label="Firmware" options={options} value="" onChange={vi.fn()} testId={testId} />);
      const trigger = screen.getByRole("button", { name: "Firmware" });

      fireEvent.click(trigger);
      expect(screen.getByRole("listbox")).toBeInTheDocument();
      fireEvent.mouseDown(document.body);
      expect(screen.queryByRole("listbox")).not.toBeInTheDocument();

      fireEvent.click(trigger);
      fireEvent.touchStart(document.body);
      expect(screen.queryByRole("listbox")).not.toBeInTheDocument();

      fireEvent.click(trigger);
      fireEvent.mouseDown(trigger);
      expect(screen.getByRole("listbox")).toBeInTheDocument();
      fireEvent.click(trigger);
      expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
    },
  );

  test("dismisses the first picker when another trigger has the same test ID", () => {
    render(
      <>
        <FirmwarePickerButton
          label="Firmware for Manufacturer A Rig"
          options={options}
          value=""
          onChange={vi.fn()}
          testId="firmware-Rig"
        />
        <FirmwarePickerButton
          label="Firmware for Manufacturer B Rig"
          options={options}
          value=""
          onChange={vi.fn()}
          testId="firmware-Rig"
        />
      </>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Firmware for Manufacturer A Rig" }));
    const otherTrigger = screen.getByRole("button", { name: "Firmware for Manufacturer B Rig" });
    fireEvent.mouseDown(otherTrigger);
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
    fireEvent.click(otherTrigger);
    expect(screen.queryByRole("listbox", { name: "Firmware for Manufacturer A Rig options" })).not.toBeInTheDocument();
    expect(screen.getByRole("listbox", { name: "Firmware for Manufacturer B Rig options" })).toBeInTheDocument();
  });

  test("still dismisses on Escape while its own trigger is focused", () => {
    render(<FirmwarePickerButton label="Firmware" options={options} value="" onChange={vi.fn()} testId="picker" />);
    const trigger = screen.getByRole("button", { name: "Firmware" });
    fireEvent.click(trigger);
    act(() => trigger.focus());
    expect(trigger).toHaveFocus();
    expect(screen.getByRole("listbox")).toBeInTheDocument();
    fireEvent.keyDown(trigger, { key: "Escape" });
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  });

  test("still dismisses a phone picker through its backdrop", () => {
    vi.spyOn(windowDimensions, "useWindowDimensions").mockReturnValue({
      width: 390,
      height: 800,
      isDesktop: false,
      isLaptop: false,
      isTablet: false,
      isPhone: true,
    });
    render(
      <FirmwarePickerButton label="Firmware" options={options} value="" onChange={vi.fn()} testId="firmware-O'Brien" />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Firmware" }));
    const backdrop = screen.getByTestId("popover-sheet");
    fireEvent.touchStart(backdrop);
    fireEvent.click(backdrop);
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  });

  test("keeps a picker closed after a write lock is released", () => {
    const props = {
      label: "Firmware",
      options,
      value: "",
      onChange: vi.fn(),
      testId: "picker",
    };
    const { rerender } = render(<FirmwarePickerButton {...props} />);
    const trigger = screen.getByRole("button", { name: "Firmware" });
    fireEvent.click(trigger);
    expect(screen.getByRole("listbox")).toBeInTheDocument();
    expect(trigger).toHaveAttribute("aria-expanded", "true");

    rerender(<FirmwarePickerButton {...props} disabled />);
    expect(trigger).toBeDisabled();
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
    fireEvent.click(trigger);
    expect(props.onChange).not.toHaveBeenCalled();

    rerender(<FirmwarePickerButton {...props} />);
    expect(trigger).toBeEnabled();
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
    fireEvent.click(trigger);
    fireEvent.click(screen.getByRole("option", { name: "1.4.4" }));
    expect(props.onChange).toHaveBeenCalledExactlyOnceWith("uploaded-file");
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  });

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
        assignment={{ value: null, label: "1.4.3" }}
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
        assignment={{ value: null, label: "1.4.3" }}
        onChange={onChange}
        testId="picker"
      />,
    );
    expect(screen.getByTestId("picker")).toHaveTextContent("No firmware");
  });

  test("uses the saved assignment label even when matching catalog metadata changes or disappears", () => {
    const props = {
      label: "Firmware",
      value: "uploaded-file",
      assignment: { value: "uploaded-file", label: "1.4.3" },
      onChange: vi.fn(),
      testId: "picker",
    };
    const { rerender } = render(<FirmwarePickerButton {...props} options={options} />);
    expect(screen.getByTestId("picker")).toHaveTextContent("1.4.3");
    fireEvent.click(screen.getByTestId("picker"));
    expect(screen.getByRole("option", { name: "1.4.3" })).toHaveAttribute("aria-selected", "true");
    expect(screen.queryByRole("option", { name: "1.4.4" })).not.toBeInTheDocument();
    rerender(<FirmwarePickerButton {...props} options={[options[0]]} />);
    expect(screen.getByTestId("picker")).toHaveTextContent("1.4.3");
  });
});
