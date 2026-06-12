import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";

import Input from ".";

describe("Input", () => {
  beforeEach(() => {
    render(<Input id="name" label="Name" />);
  });

  test("renders input component", () => {
    const inputElement = screen.getByRole("textbox");
    expect(inputElement).toBeInTheDocument();
  });

  test("renders label", () => {
    const labelElement = screen.getByText("Name");
    expect(labelElement).toBeInTheDocument();
  });

  test("input component accepts user input", () => {
    const inputElement = screen.getByRole("textbox") as HTMLInputElement;
    const userInput = "Hello, World!";
    fireEvent.change(inputElement, { target: { value: userInput } });
    expect(inputElement.value).toBe(userInput);
  });

  test("applies new-password autocomplete to prevent autofill", () => {
    const inputElement = screen.getByRole("textbox") as HTMLInputElement;
    // Uses "new-password" instead of "off" because Chrome ignores "off" on password fields
    expect(inputElement.getAttribute("autocomplete")).toBe("new-password");
  });

  test("renders an accessible clear button for dismissible inputs", () => {
    const onChange = vi.fn();

    render(<Input id="email" label="Email" dismiss initValue="user@example.com" onChange={onChange} />);

    const clearButton = screen.getByRole("button", { name: "Clear Email" });
    fireEvent.click(clearButton);

    expect(onChange).toHaveBeenCalledWith("", "email");
  });

  test("renders an accessible password toggle button", () => {
    render(<Input id="password" label="Password" type="password" />);

    const passwordInput = screen.getByLabelText("Password");
    const showPasswordButton = screen.getByRole("button", { name: "Show password" });

    expect(passwordInput).toHaveAttribute("type", "password");

    fireEvent.click(showPasswordButton);

    expect(passwordInput).toHaveAttribute("type", "text");
    expect(screen.getByRole("button", { name: "Hide password" })).toBeInTheDocument();
  });

  test("does not reserve password toggle padding when the toggle is hidden", () => {
    render(
      <Input
        id="secret"
        label="Secret"
        type="password"
        hidePasswordToggle
        tooltip={{ body: "Use the saved password placeholder." }}
      />,
    );

    const passwordInput = screen.getByLabelText("Secret");

    expect(passwordInput).toHaveClass("pr-10");
    expect(passwordInput).not.toHaveClass("pr-20");
    expect(screen.queryByRole("button", { name: "Show password" })).not.toBeInTheDocument();
  });

  test("reserves space for tooltip, trailing icon, and suffix action together", () => {
    render(
      <Input
        id="secret"
        label="Secret"
        type="password"
        tooltip={{ body: "Use the saved password placeholder." }}
        suffixAction={
          <button type="button" aria-label="Suffix action">
            ?
          </button>
        }
      />,
    );

    const inputElement = screen.getByLabelText("Secret");
    const suffixAction = screen.getByRole("button", { name: "Suffix action" });
    const suffixActionWrapper = suffixAction.parentElement as HTMLElement;

    expect(inputElement).toHaveClass("pr-28");
    expect(inputElement).not.toHaveClass("pr-20");
    expect(suffixActionWrapper).toHaveClass("right-20");
  });

  test("does not apply focus highlight classes to readonly inputs", () => {
    render(<Input id="readonly-field" label="Readonly field" initValue="0" readOnly />);

    const inputElement = screen.getByLabelText("Readonly field");

    expect(inputElement).toHaveAttribute("readonly");
    expect(inputElement).toHaveClass("cursor-default");
    expect(inputElement).not.toHaveClass("focus:border-border-20");
    expect(inputElement).not.toHaveClass("focus:ring-4");
    expect(inputElement).not.toHaveClass("focus:ring-core-primary-5");
  });
});

describe("Input ARIA attributes", () => {
  test("renders aria-required when required prop is set", () => {
    render(<Input id="email" label="Email" required />);
    const input = screen.getByRole("textbox");
    expect(input).toHaveAttribute("aria-required", "true");
  });

  test("does not render aria-required when required prop is not set", () => {
    render(<Input id="email" label="Email" />);
    const input = screen.getByRole("textbox");
    expect(input).not.toHaveAttribute("aria-required");
  });

  test("renders aria-invalid when there is an error", () => {
    render(<Input id="email" label="Email" error="Invalid email" />);
    const input = screen.getByRole("textbox");
    expect(input).toHaveAttribute("aria-invalid", "true");
  });

  test("renders aria-invalid when error is boolean true", () => {
    render(<Input id="email" label="Email" error={true} />);
    const input = screen.getByRole("textbox");
    expect(input).toHaveAttribute("aria-invalid", "true");
  });

  test("does not render aria-invalid when there is no error", () => {
    render(<Input id="email" label="Email" />);
    const input = screen.getByRole("textbox");
    expect(input).not.toHaveAttribute("aria-invalid");
  });

  test("renders aria-describedby pointing to error message ID when error is a string", () => {
    render(<Input id="email" label="Email" error="Invalid email" />);
    const input = screen.getByRole("textbox");
    expect(input).toHaveAttribute("aria-describedby", "email-error");
  });

  test("does not render aria-describedby when error is boolean true", () => {
    render(<Input id="email" label="Email" error={true} />);
    const input = screen.getByRole("textbox");
    expect(input).not.toHaveAttribute("aria-describedby");
  });

  test("does not render aria-describedby when there is no error", () => {
    render(<Input id="email" label="Email" />);
    const input = screen.getByRole("textbox");
    expect(input).not.toHaveAttribute("aria-describedby");
  });

  test("does not render aria-describedby when error is an empty string", () => {
    render(<Input id="email" label="Email" error="" />);
    const input = screen.getByRole("textbox");
    expect(input).not.toHaveAttribute("aria-describedby");
  });

  test("error message div has matching id attribute", () => {
    render(<Input id="email" label="Email" error="Invalid email" testId="email-input" />);
    const errorDiv = screen.getByTestId("email-input-validation-error");
    expect(errorDiv).toHaveAttribute("id", "email-error");
  });
});
