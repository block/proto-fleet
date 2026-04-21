import React, { type ReactNode } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";

import ThemeSwitcher from "./ThemeSwitcher";

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

describe("ThemeSwitcher", () => {
  const defaultProps = {
    onClickDone: vi.fn(),
    theme: "system" as const,
    setTheme: vi.fn(),
  };

  test("renders with title 'Theme'", () => {
    render(<ThemeSwitcher {...defaultProps} />);
    expect(screen.getByText("Theme")).toBeDefined();
  });

  test("renders theme options (System, Light, Dark)", () => {
    render(<ThemeSwitcher {...defaultProps} />);
    expect(screen.getByText("System")).toBeDefined();
    expect(screen.getByText("Light")).toBeDefined();
    expect(screen.getByText("Dark")).toBeDefined();
  });

  test("renders a 'Done' button that calls the dismiss handler", () => {
    const onClickDone = vi.fn();
    render(<ThemeSwitcher {...defaultProps} onClickDone={onClickDone} />);
    const doneButton = screen.getByText("Done");
    expect(doneButton).toBeDefined();
    fireEvent.click(doneButton);
    expect(onClickDone).toHaveBeenCalled();
  });

  test("marks the current theme as selected", () => {
    render(<ThemeSwitcher {...defaultProps} theme="dark" />);
    const radioInputs = document.body.querySelectorAll('input[type="radio"]');
    expect(radioInputs).toHaveLength(3);
    expect((radioInputs[0] as HTMLInputElement).checked).toBe(false); // system
    expect((radioInputs[1] as HTMLInputElement).checked).toBe(false); // light
    expect((radioInputs[2] as HTMLInputElement).checked).toBe(true); // dark
  });
});
