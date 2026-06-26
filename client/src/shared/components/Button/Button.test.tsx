import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";

import Button, { sizes, variants } from ".";

const buttonText = "Click me";

describe("Button", () => {
  test("renders as a link (anchor) when `to` is set — no nested button", () => {
    render(
      <MemoryRouter>
        <Button to="/fleet/sites" text="View sites" variant={variants.secondary} testId="cta" />
      </MemoryRouter>,
    );
    const cta = screen.getByTestId("cta");
    expect(cta.tagName).toBe("A");
    expect(cta).toHaveAttribute("href", "/fleet/sites");
    // The styled element must not wrap or contain a nested <button>.
    expect(cta.querySelector("button")).toBeNull();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  test("renders an inert span (not a link) when `to` is set but disabled", () => {
    render(
      <MemoryRouter>
        <Button to="/fleet/sites" text="View sites" disabled variant={variants.secondary} testId="cta" />
      </MemoryRouter>,
    );
    const cta = screen.getByTestId("cta");
    expect(cta.tagName).toBe("SPAN");
    expect(cta).not.toHaveAttribute("href");
    expect(cta).toHaveAttribute("aria-disabled", "true");
  });

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
