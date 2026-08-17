import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import ProgressCircular from ".";

describe("ProgressCircular", () => {
  it("renders with the neutral primary color by default", () => {
    render(<ProgressCircular className="text-core-accent-fill" dataTestId="progress-spinner" indeterminate />);

    expect(screen.getByTestId("progress-spinner")).toHaveClass("!text-core-primary-fill");
    expect(screen.getByTestId("progress-spinner")).toHaveClass("text-core-accent-fill");
  });

  it("renders with the inverse color when used on a filled control", () => {
    render(<ProgressCircular color="inverse" dataTestId="inverse-progress-spinner" indeterminate />);

    expect(screen.getByTestId("inverse-progress-spinner")).toHaveClass("!text-text-contrast");
  });
});
