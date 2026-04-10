import React, { type ReactNode } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";

import TemperatureUnitsSwitcher from "./TemperatureUnitsSwitcher";

vi.mock("motion/react", () => ({
  motion: {
    div: ({
      children,
      initial: _initial,
      animate: _animate,
      exit: _exit,
      transition: _transition,
      ...rest
    }: Record<string, unknown>) => React.createElement("div", rest, children as ReactNode),
  },
  AnimatePresence: ({ children }: { children: ReactNode }) => children,
}));

describe("TemperatureUnitsSwitcher", () => {
  const defaultProps = {
    onClickDone: vi.fn(),
    temperatureUnit: "C" as const,
    setTemperatureUnit: vi.fn(),
  };

  test("renders with title 'Temperature'", () => {
    render(<TemperatureUnitsSwitcher {...defaultProps} />);
    expect(screen.getByText("Temperature")).toBeDefined();
  });

  test("renders temperature unit options (Celsius and Fahrenheit)", () => {
    render(<TemperatureUnitsSwitcher {...defaultProps} />);
    expect(screen.getByText("Celsius (ºC)")).toBeDefined();
    expect(screen.getByText("Fahrenheit (ºF)")).toBeDefined();
  });

  test("renders a 'Done' button that calls the dismiss handler", () => {
    const onClickDone = vi.fn();
    render(<TemperatureUnitsSwitcher {...defaultProps} onClickDone={onClickDone} />);
    const doneButton = screen.getByText("Done");
    expect(doneButton).toBeDefined();
    fireEvent.click(doneButton);
    expect(onClickDone).toHaveBeenCalled();
  });

  test("marks the current temperature unit as selected", () => {
    render(<TemperatureUnitsSwitcher {...defaultProps} temperatureUnit="F" />);
    const radioInputs = document.body.querySelectorAll('input[type="radio"]');
    expect(radioInputs).toHaveLength(2);
    expect((radioInputs[0] as HTMLInputElement).checked).toBe(false); // Celsius
    expect((radioInputs[1] as HTMLInputElement).checked).toBe(true); // Fahrenheit
  });
});
