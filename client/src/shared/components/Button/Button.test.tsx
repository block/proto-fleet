import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";

import Button, { sizes, variants } from ".";

const buttonText = "Click me";

describe("Button", () => {
  test("renders the button with the correct text", () => {
    const { getByText } = render(
      <Button text={buttonText} onClick={() => {}} size={sizes.base} variant={variants.secondary} />,
    );
    const buttonElement = getByText(buttonText);
    expect(buttonElement).toBeDefined();
  });

  test("calls the onClick function when clicked", () => {
    const onClickMock = vi.fn();
    const { getByText } = render(
      <Button text={buttonText} onClick={onClickMock} size={sizes.base} variant={variants.secondary} />,
    );
    const buttonElement = getByText(buttonText);
    fireEvent.click(buttonElement);
    expect(onClickMock).toHaveBeenCalled();
  });

  test("renders icon-only buttons with an accessible name and focus-visible styles", () => {
    render(
      <Button
        ariaLabel="Close dialog"
        onClick={() => {}}
        prefixIcon={<span aria-hidden="true">x</span>}
        size={sizes.base}
        variant={variants.secondary}
      />,
    );

    const buttonElement = screen.getByRole("button", { name: "Close dialog" });

    expect(buttonElement).toHaveClass("focus-visible:ring-2");
    expect(buttonElement).toHaveClass("focus-visible:ring-core-primary-fill");
    expect(buttonElement).toHaveClass("focus-visible:ring-offset-2");
  });
});
