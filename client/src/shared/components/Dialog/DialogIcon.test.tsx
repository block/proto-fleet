import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import DialogIcon from "./DialogIcon";

describe("DialogIcon", () => {
  it("renders children", () => {
    const { getByText } = render(
      <DialogIcon>
        <span>icon</span>
      </DialogIcon>,
    );

    expect(getByText("icon")).toBeInTheDocument();
  });

  it("applies critical intent class", () => {
    const { container } = render(
      <DialogIcon intent="critical">
        <span>icon</span>
      </DialogIcon>,
    );

    expect(container.firstChild).toHaveClass("text-intent-critical-fill");
  });

  it("applies success intent class", () => {
    const { container } = render(
      <DialogIcon intent="success">
        <span>icon</span>
      </DialogIcon>,
    );

    expect(container.firstChild).toHaveClass("text-intent-success-fill");
  });

  it("does not apply intent class when no intent is provided", () => {
    const { container } = render(
      <DialogIcon>
        <span>icon</span>
      </DialogIcon>,
    );

    const classList = (container.firstChild as HTMLElement).className;
    expect(classList).not.toMatch(/text-intent/);
  });

  it("always applies base wrapper classes", () => {
    const { container } = render(
      <DialogIcon>
        <span>icon</span>
      </DialogIcon>,
    );

    const el = container.firstChild as HTMLElement;
    expect(el).toHaveClass("flex", "size-10", "items-center", "justify-center", "rounded-lg", "bg-surface-5");
  });
});
