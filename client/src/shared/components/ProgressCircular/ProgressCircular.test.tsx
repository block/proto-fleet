import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import ProgressCircular from ".";

describe("ProgressCircular", () => {
  it("always renders with the neutral primary color", () => {
    render(<ProgressCircular className="text-core-accent-fill" dataTestId="progress-spinner" indeterminate />);

    expect(screen.getByTestId("progress-spinner")).toHaveClass("!text-core-primary-fill");
    expect(screen.getByTestId("progress-spinner")).toHaveClass("text-core-accent-fill");
  });
});
