import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import NullState from "./NullState";

describe("NullState", () => {
  it("uses a mobile-appropriate title size before restoring display type on tablet", () => {
    render(<NullState title="No racks yet" description="Add a rack to get started." />);

    expect(screen.getByText("No racks yet")).toHaveClass("text-heading-300", "tablet:text-display-200");
  });
});
