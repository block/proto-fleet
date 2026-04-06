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
