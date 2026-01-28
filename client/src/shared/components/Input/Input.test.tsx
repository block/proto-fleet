import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test } from "vitest";

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
});
