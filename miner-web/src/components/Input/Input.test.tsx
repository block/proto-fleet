import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test } from "vitest";
import "@testing-library/jest-dom";

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
});
