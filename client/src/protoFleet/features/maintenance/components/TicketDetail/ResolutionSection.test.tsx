import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ResolutionSectionContent } from "./ResolutionSection";

describe("ResolutionSectionContent", () => {
  it("hides an inapplicable repair location", () => {
    render(<ResolutionSectionContent resolution="Deferred" repairLocation="unknown" partsUsed={[]} notes="" />);

    expect(screen.getByText("Deferred")).toBeInTheDocument();
    expect(screen.queryByText("Repair location")).not.toBeInTheDocument();
    expect(screen.queryByText("unknown")).not.toBeInTheDocument();
  });

  it("shows a recorded repair location", () => {
    render(<ResolutionSectionContent resolution="Repaired" repairLocation="On-site" partsUsed={[]} notes="" />);

    expect(screen.getByText("Repair location")).toBeInTheDocument();
    expect(screen.getByText("On-site")).toBeInTheDocument();
  });
});
