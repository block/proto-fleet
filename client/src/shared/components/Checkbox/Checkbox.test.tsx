import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import Checkbox from "@/shared/components/Checkbox";

describe("Checkbox", () => {
  it("shows the checked indicator when checked and disabled", () => {
    render(<Checkbox checked disabled />);

    const checkbox = screen.getByRole("checkbox");

    expect(checkbox).toBeDisabled();
    expect(checkbox.className).toContain("checked:bg-border-20");
    expect(checkbox.className).not.toContain("checked:bg-core-accent-fill");
    expect(screen.getByTestId("checkmark-icon")).toBeInTheDocument();
  });
});
