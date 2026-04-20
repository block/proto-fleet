import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, test } from "vitest";

import Textarea from ".";

describe("Textarea", () => {
  test("renders textarea component", () => {
    render(<Textarea id="notes" label="Notes" />);
    const textarea = screen.getByRole("textbox");
    expect(textarea).toBeInTheDocument();
  });

  test("renders label", () => {
    render(<Textarea id="notes" label="Notes" />);
    expect(screen.getByText("Notes")).toBeInTheDocument();
  });

  test("textarea accepts user input", () => {
    render(<Textarea id="notes" label="Notes" />);
    const textarea = screen.getByRole("textbox") as HTMLTextAreaElement;
    fireEvent.change(textarea, { target: { value: "Some text" } });
    expect(textarea.value).toBe("Some text");
  });
});

describe("Textarea ARIA attributes", () => {
  test("renders aria-required when required prop is set", () => {
    render(<Textarea id="notes" label="Notes" required />);
    const textarea = screen.getByRole("textbox");
    expect(textarea).toHaveAttribute("aria-required", "true");
  });

  test("does not render aria-required when required prop is not set", () => {
    render(<Textarea id="notes" label="Notes" />);
    const textarea = screen.getByRole("textbox");
    expect(textarea).not.toHaveAttribute("aria-required");
  });

  test("renders aria-invalid when there is an error", () => {
    render(<Textarea id="notes" label="Notes" error="Required field" />);
    const textarea = screen.getByRole("textbox");
    expect(textarea).toHaveAttribute("aria-invalid", "true");
  });

  test("renders aria-invalid when error is boolean true", () => {
    render(<Textarea id="notes" label="Notes" error={true} />);
    const textarea = screen.getByRole("textbox");
    expect(textarea).toHaveAttribute("aria-invalid", "true");
  });

  test("does not render aria-invalid when there is no error", () => {
    render(<Textarea id="notes" label="Notes" />);
    const textarea = screen.getByRole("textbox");
    expect(textarea).not.toHaveAttribute("aria-invalid");
  });

  test("renders aria-describedby pointing to error message ID when error is a string", () => {
    render(<Textarea id="notes" label="Notes" error="Required field" />);
    const textarea = screen.getByRole("textbox");
    expect(textarea).toHaveAttribute("aria-describedby", "notes-error");
  });

  test("does not render aria-describedby when error is boolean true", () => {
    render(<Textarea id="notes" label="Notes" error={true} />);
    const textarea = screen.getByRole("textbox");
    expect(textarea).not.toHaveAttribute("aria-describedby");
  });

  test("does not render aria-describedby when there is no error", () => {
    render(<Textarea id="notes" label="Notes" />);
    const textarea = screen.getByRole("textbox");
    expect(textarea).not.toHaveAttribute("aria-describedby");
  });

  test("does not render aria-describedby when error is an empty string", () => {
    render(<Textarea id="notes" label="Notes" error="" />);
    const textarea = screen.getByRole("textbox");
    expect(textarea).not.toHaveAttribute("aria-describedby");
  });

  test("error message div has matching id attribute", () => {
    render(<Textarea id="notes" label="Notes" error="Required field" testId="notes-textarea" />);
    const errorDiv = screen.getByTestId("notes-textarea-validation-error");
    expect(errorDiv).toHaveAttribute("id", "notes-error");
  });
});
